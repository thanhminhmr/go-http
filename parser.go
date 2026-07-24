/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-exception"
)

type RequestHandler[Request any] = func(ctx Context, request Request) *Response

// RequestParser parses an HTTP request and populates a struct using field tags
// to map request data to struct fields.
//
// Tags are applied in the order listed below, from lowest to highest priority.
// If multiple tags are present on the same field and more than one value is
// available in the request, the value from the higher-priority tag is used
// (e.g., `form` overrides `query`).
//
// Supported tags:
//
//   - `header`: If the tag value is not empty, the tag value must match the
//     normalized HTTP header name. If the tag value is empty, the field must be of
//     type [http.Header], and only one field with this tag is allowed per struct. When
//     the tag value is empty, the field is populated by assigning the request's
//     header map by reference, so mutations made to the map through the field are
//     also visible through [http.Request.Header].
//
//   - `cookie`: If the tag value is not empty, the tag value must match the cookie
//     name. If the tag value is empty, the field must be of type [KeyValues], and
//     only one field with this tag is allowed per struct.
//
//   - `query`: If the tag value is not empty, the tag value must match the query
//     parameter name. If the tag value is empty, the field must be of type
//     [KeyValues], and only one field with this tag is allowed per struct.
//
//   - `url`: If the tag value is not empty, the tag value must match a named
//     segment in the URL path. If the tag value is empty, the field must be of type
//     [KeyValue], and only one field with this tag is allowed per struct.
//
//   - `form`: If the tag value is not empty, the tag value must match the form
//     parameter name. If the tag value is empty, the field must be of type
//     [KeyValues], and only one field with this tag is allowed per struct.
//
//   - `json`: If the tag value is empty, the request body is unmarshalled into
//     this field using `encoding/json`, and only one field with this tag is allowed
//     per struct. In this case, any type validation is handled by the JSON
//     unmarshalling process. If the tag value is not empty, then the JSON body must
//     be an object, and the tag value must match the JSON object field name.
//
//   - `multipart`: The tag value must be empty. Only one field with this tag
//     is allowed per struct. The field must be of type [*multipart.Reader]. The
//     reader is backed by the raw request body, which the server closes after the
//     handler returns; the body must be consumed synchronously within the handler.
//
//   - `body`: Only one field with this tag is allowed per struct. The field must
//     be of type [io.ReadCloser]. If the tag value is not empty, the tag value must
//     be a semicolon-separated list of accepted Content-Types, and if `form`, `json`
//     or `multipart` tag exists, the list must not contain those types. If the tag
//     value is empty, the field will be mapped if no other body type are matched.
//     The [io.ReadCloser] is the raw request body, which the server closes after the
//     handler returns; the body must be consumed synchronously within the handler.
//
// If request parsing or validation fails, the handler is not invoked and an HTTP
// error response is sent with the appropriate status code and an empty body; the
// failure is logged at error level on the request logger. Parse failures use the
// status returned by the specific binder (e.g. 400 for malformed input, 415 for
// an unsupported or missing Content-Type); validation failures always return 400
// Bad Request.
func RequestParser[Request any](handler RequestHandler[Request]) http.HandlerFunc {
	tags := createTags(reflect.TypeFor[Request]())
	return func(writer http.ResponseWriter, request *http.Request) {
		var parsed Request
		parsedValue := reflect.ValueOf(&parsed).Elem()
		for _, field := range tags.defaultFields {
			if err := internalDecodeString(field.value, parsedValue.FieldByIndex(field.index)); err != nil {
				panic(fmt.Errorf("BUG: Cannot unmarshal default value: %w", err))
			}
		}
		requestHandler(writer, request, &tags, &parsed, func(ctx Context) *Response {
			return handler(ctx, parsed)
		})
	}
}

var requestValidator = validator.New(validator.WithRequiredStructEnabled())

func requestHandler(
	writer http.ResponseWriter, request *http.Request, tags *requestTags,
	parsed any, handler func(ctx Context) *Response,
) {
	logger := zerolog.Ctx(request.Context())
	// parse request
	if status, err := tags.parse(request, reflect.ValueOf(parsed).Elem()); err != nil {
		logger.Error().Err(err).Msg("Failed to parse request")
		writer.WriteHeader(status)
		return
	}
	// validate request
	if err := requestValidator.Struct(parsed); err != nil {
		logger.Error().Err(err).Msg("Failed to validate request")
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	logger.Debug().Any("parsed", parsed).Msg("Request parsed")
	// call handler and log error if any
	response := handler(Context{Ctx: request.Context(), header: writer.Header()})
	if response == nil {
		logger.Error().Msg("Handler failed")
		response = &Response{status: http.StatusInternalServerError}
	} else {
		logger.Debug().Any("response", response).Msg("Handler returned")
	}
	// write response
	if err := response.write(writer); err != nil {
		logger.Error().Err(err).Msg("Failed to write response")
	}
}

//region requestTags

type requestTags struct {
	flags               uint
	defaultFields       []defaultField
	headerFieldIndex    []int
	cookieFieldIndex    []int
	queryFieldIndex     []int
	urlFieldIndex       []int
	formFieldIndex      []int
	jsonFieldIndex      []int
	multipartFieldIndex []int
	bodyFieldIndex      []int
	bodyContentTypes    []string
}

type defaultField = struct {
	index []int
	value string
}

const (
	tagHeader uint = 1 << iota
	tagCookie
	tagQuery
	tagUrl
	tagForm
	tagJson
	tagMultipart
	tagBody
)

const (
	contentTypeIsForm      = "application/x-www-form-urlencoded"
	contentTypeIsJson      = "application/json"
	contentTypeIsMultipart = "multipart/form-data"
)

var globalTags = map[reflect.Type]requestTags{}
var globalTagsMutex sync.Mutex

func createTags(requestType reflect.Type) requestTags {
	if requestType.Kind() != reflect.Struct {
		panic("BUG: parsed request must be a struct")
	}
	// lock the global tags cache
	globalTagsMutex.Lock()
	defer globalTagsMutex.Unlock()
	// check if request tags already exists
	tags, exists := globalTags[requestType]
	if exists {
		return tags
	}
	// check the tags recursively
	defaultValue := reflect.New(requestType).Elem()
	tags.checkRecursively(requestType, defaultValue, nil)
	if len(tags.bodyContentTypes) > 0 {
		if tags.flags&tagForm != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsForm) {
			panic("BUG: `form` tag field is not allowed when `body` tag contains " + contentTypeIsForm)
		} else if tags.flags&tagJson != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsJson) {
			panic("BUG: `json` tag field is not allowed when `body` tag contains " + contentTypeIsJson)
		} else if tags.flags&tagMultipart != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsMultipart) {
			panic("BUG: `multipart` tag field is not allowed when `body` tag contains " + contentTypeIsMultipart)
		}
	}
	globalTags[requestType] = tags
	return tags
}

var typeForHeader = reflect.TypeFor[http.Header]()
var typeForKeyValues = reflect.TypeFor[KeyValues]()
var typeForKeyValue = reflect.TypeFor[KeyValue]()
var typeForMultipartReader = reflect.TypeFor[*multipart.Reader]()
var typeForReadCloser = reflect.TypeFor[io.ReadCloser]()

func (tags *requestTags) checkRecursively(requestType reflect.Type, requestValue reflect.Value, fieldIndex []int) {
	for index := range requestType.NumField() {
		field := requestType.Field(index)
		// skip if field is not exported
		if field.PkgPath != "" {
			continue
		}
		// process anonymous struct
		if field.Anonymous {
			if field.Type.Kind() != reflect.Struct {
				panic("BUG: anonymous field must be a struct")
			}
			tags.checkRecursively(field.Type, requestValue, append(fieldIndex, field.Index...))
			continue
		}
		// process default tag
		if value, exists := field.Tag.Lookup("default"); exists {
			// validate the default value can be decoded
			if err := internalDecodeString(value, requestValue.FieldByIndex(append(fieldIndex, field.Index...))); err != nil {
				panic(fmt.Errorf("BUG: Cannot unmarshal default value: %w", err))
			}
			// store field index and raw default string for per-request re-parsing
			tags.defaultFields = append(tags.defaultFields, defaultField{
				index: append(append([]int(nil), fieldIndex...), field.Index...),
				value: value,
			})
		}
		// process header tag
		if value, exists := field.Tag.Lookup("header"); exists {
			if value != "" {
				if tags.headerFieldIndex != nil {
					panic("BUG: multiple `header` tag fields are not allowed when empty `header` tag is present")
				}
			} else {
				if tags.flags&tagHeader != 0 {
					panic("BUG: multiple `header` tag fields are not allowed when empty `header` tag is present")
				}
				if field.Type != typeForHeader {
					panic("BUG: empty `header` tag field must be a `http.Header`")
				}
				tags.headerFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagHeader
		}
		// process cookie tag
		if value, exists := field.Tag.Lookup("cookie"); exists {
			if value != "" {
				if tags.cookieFieldIndex != nil {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
			} else {
				if tags.flags&tagCookie != 0 {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `cookie` tag field must be a `http.KeyValues`")
				}
				tags.cookieFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagCookie
		}
		// process query tag
		if value, exists := field.Tag.Lookup("query"); exists {
			if value != "" {
				if tags.queryFieldIndex != nil {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
			} else {
				if tags.flags&tagQuery != 0 {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `query` tag field must be a `http.KeyValues`")
				}
				tags.queryFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagQuery
		}
		// process url tag
		if value, exists := field.Tag.Lookup("url"); exists {
			if value != "" {
				if tags.urlFieldIndex != nil {
					panic("BUG: multiple `url` tag fields are not allowed when empty `url` tag is present")
				}
			} else {
				if tags.flags&tagUrl != 0 {
					panic("BUG: multiple `url` tag fields are not allowed when empty `url` tag is present")
				}
				if field.Type != typeForKeyValue {
					panic("BUG: empty `url` tag field must be a `http.KeyValue`")
				}
				tags.urlFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagUrl
		}
		// process form tag
		if value, exists := field.Tag.Lookup("form"); exists {
			if value != "" {
				if tags.formFieldIndex != nil {
					panic("BUG: multiple `form` tag fields are not allowed when empty `form` tag is present")
				}
			} else {
				if tags.flags&tagForm != 0 {
					panic("BUG: multiple `form` tag fields are not allowed when empty `form` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `form` tag field must be a `http.KeyValues`")
				}
				tags.formFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagForm
		}
		// process json tag
		if value, exists := field.Tag.Lookup("json"); exists {
			if value != "" {
				if tags.jsonFieldIndex != nil {
					panic("BUG: multiple `json` tag fields are not allowed when empty `json` tag is present")
				}
			} else {
				if tags.flags&tagJson != 0 {
					panic("BUG: multiple `json` tag fields are not allowed when empty `json` tag is present")
				}
				tags.jsonFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagJson
		}
		// process multipart tag
		if value, exists := field.Tag.Lookup("multipart"); exists {
			if value != "" {
				panic("BUG: `multipart` tag value must be empty")
			}
			if tags.flags&tagMultipart != 0 {
				panic("BUG: multiple `multipart` tag fields are not allowed")
			}
			if field.Type != typeForMultipartReader {
				panic("BUG: `multipart` tag field must be a `*multipart.Reader`")
			}
			tags.flags = tags.flags | tagMultipart
			tags.multipartFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
		}
		// process `body` tag
		if value, exists := field.Tag.Lookup("body"); exists {
			if tags.flags&tagBody != 0 {
				panic("BUG: multiple `body` tag fields are not allowed")
			}
			if field.Type != typeForReadCloser {
				panic("BUG: `body` tag field must be a `io.ReadCloser`")
			}
			tags.flags = tags.flags | tagBody
			tags.bodyFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			if value != "" {
				tags.bodyContentTypes = strings.Split(value, ";")
			}
		}
	}
}

//endregion requestTags

//region parseRequest

const (
	maxBodyLength       = 1 << 20 // 1 MiB
	maxReadBodyDuration = 5 * time.Second
)

func (tags *requestTags) parse(request *http.Request, parsed reflect.Value) (status int, parseErr error) {
	// parse and bind request header
	if tags.flags&tagHeader != 0 {
		if status, parseErr = tags.bindHeader(request, parsed); parseErr != nil {
			return
		}
	}
	// parse and bind cookies
	if tags.flags&tagCookie != 0 {
		if status, parseErr = tags.bindCookie(request, parsed); parseErr != nil {
			return
		}
	}
	// parse and bind url query values
	if tags.flags&tagQuery != 0 {
		if status, parseErr = tags.bindQuery(request, parsed); parseErr != nil {
			return
		}
	}
	// parse and bind url parameters
	if tags.flags&tagUrl != 0 {
		if status, parseErr = tags.bindUrl(request, parsed); parseErr != nil {
			return
		}
	}
	// parse and bind body
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		// check for empty request
		if request.ContentLength == 0 {
			return
		}
		// get content type
		contentTypeHeader := request.Header.Get("Content-Type")
		if contentTypeHeader == "" {
			return http.StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is missing")
		}
		// parse media type
		contentType, contentTypeParameters, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Content-Type is invalid").AddCause(err)
		}
		// parse and bind request body as form
		if tags.flags&tagForm != 0 && contentType == contentTypeIsForm {
			return bindFullTextBody(request, contentTypeParameters, parsed, tags.bindForm)
		}
		// parse and bind request body as JSON
		if tags.flags&tagJson != 0 && contentType == contentTypeIsJson {
			return bindFullTextBody(request, contentTypeParameters, parsed, tags.bindJson)
		}
		// parse and bind request body as multipart form
		if tags.flags&tagMultipart != 0 && contentType == contentTypeIsMultipart {
			return tags.bindMultipart(request, parsed, contentTypeParameters)
		}
		// bind request body raw
		if tags.flags&tagBody != 0 && (len(tags.bodyContentTypes) == 0 || slices.Contains(tags.bodyContentTypes, contentType)) {
			tags.bindBody(request, parsed)
			return
		}
		// nothing matched
		return http.StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is unsupported")
	}
	return
}

func bindFullTextBody(request *http.Request, contentTypeParameters map[string]string, parsed reflect.Value,
	binder func(reader io.Reader, parsed reflect.Value) (int, error)) (int, error) {
	if request.ContentLength < 0 {
		return http.StatusLengthRequired, exception.String("HttpServer: Content-Length is required but missing")
	} else if request.ContentLength > maxBodyLength {
		return http.StatusRequestEntityTooLarge, exception.String("HttpServer: Content-Length is too large")
	} else if reader, err := charsetReader(request.Body, contentTypeParameters); err != nil {
		return http.StatusUnsupportedMediaType,
			exception.String("HttpServer: cannot determine body encoding").AddCause(err)
	} else {
		type resultValue struct {
			status int
			err    error
		}
		done := make(chan resultValue, 1)
		// run the binder
		go func(binder func(reader io.Reader, parsed reflect.Value) (int, error), done chan<- resultValue) {
			defer exception.Recover(func(recovered exception.Exception) {
				done <- resultValue{http.StatusInternalServerError,
					exception.String("HttpServer: Bind body panicked").SetRecovered(recovered)}
			})
			status, err := binder(reader, parsed)
			done <- resultValue{status, err}
		}(binder, done)
		// set time limit for binder
		select {
		case result := <-done:
			return result.status, result.err
		case <-request.Context().Done():
			return http.StatusRequestTimeout,
				exception.String("HttpServer: Client diconnected").AddSuppressed(request.Body.Close())
		case <-time.After(maxReadBodyDuration):
			return http.StatusRequestTimeout,
				exception.String("HttpServer: Bind body timed out").AddSuppressed(request.Body.Close())
		}
	}
}

func (tags *requestTags) bindHeader(request *http.Request, parsed reflect.Value) (int, error) {
	// parse and bind request header
	if len(request.Header) > 0 {
		if tags.headerFieldIndex != nil {
			parsed.FieldByIndex(tags.headerFieldIndex).Set(reflect.ValueOf(request.Header))
		} else if err := bind("header", request.Header, parsed); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind request header failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindCookie(request *http.Request, parsed reflect.Value) (int, error) {
	// check if any cookies
	if cookies := request.Cookies(); len(cookies) > 0 {
		// convert cookies into key-values
		keyValues := make(KeyValues, len(cookies))
		for _, cookie := range cookies {
			keyValues[cookie.Name] = append(keyValues[cookie.Name], cookie.Value)
		}
		// parse and bind cookies
		if tags.cookieFieldIndex != nil {
			parsed.FieldByIndex(tags.cookieFieldIndex).Set(reflect.ValueOf(keyValues))
		} else if err := bind("cookie", keyValues, parsed); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind cookies failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindQuery(request *http.Request, parsed reflect.Value) (int, error) {
	// parse and bind url query values
	if values := request.URL.Query(); len(values) > 0 {
		if tags.queryFieldIndex != nil {
			parsed.FieldByIndex(tags.queryFieldIndex).Set(reflect.ValueOf(values))
		} else if err := bind("query", KeyValues(values), parsed); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind query values failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindUrl(request *http.Request, parsed reflect.Value) (int, error) {
	// get route context from chi router
	if routeContext := chi.RouteContext(request.Context()); routeContext != nil {
		// check if there is any url params
		if urlParams := &routeContext.URLParams; len(urlParams.Keys) > 0 {
			// convert url params into key-value
			keyValue := make(KeyValue, len(urlParams.Keys))
			for index, key := range urlParams.Keys {
				keyValue[key] = urlParams.Values[index]
			}
			// parse and bind url parameters
			if tags.urlFieldIndex != nil {
				parsed.FieldByIndex(tags.urlFieldIndex).Set(reflect.ValueOf(keyValue))
			} else if err := bind("url", keyValue, parsed); err != nil {
				return http.StatusBadRequest, exception.String("HttpServer: Bind url params failed").AddCause(err)
			}
		}
	}
	return 0, nil
}

func (tags *requestTags) bindForm(reader io.Reader, parsed reflect.Value) (int, error) {
	// read the whole body at once
	body, err := io.ReadAll(reader)
	if err != nil {
		return http.StatusInternalServerError, exception.String("HttpServer: Read request body failed").AddCause(err)
	}
	// parse form body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return http.StatusBadRequest, exception.String("HttpServer: Parse form body failed").AddCause(err)
	}
	// bind form body
	if tags.formFieldIndex != nil {
		parsed.FieldByIndex(tags.formFieldIndex).Set(reflect.ValueOf(values))
	} else if err := bind("form", values, parsed); err != nil {
		return http.StatusBadRequest, exception.String("HttpServer: Bind form params failed").AddCause(err)
	}
	return 0, nil
}

func (tags *requestTags) bindJson(reader io.Reader, parsed reflect.Value) (int, error) {
	// check if decode the whole body to the JSON field
	var target any
	var values map[string]any
	if tags.jsonFieldIndex != nil {
		target = parsed.FieldByIndex(tags.jsonFieldIndex).Addr().Interface()
	} else {
		target = &values
	}
	// shared json decoder path
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return http.StatusBadRequest, exception.String("HttpServer: Decode json body failed").AddCause(err)
	}
	// bind json body
	if tags.jsonFieldIndex == nil {
		if err := bind("json", values, parsed); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind json values failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindMultipart(
	request *http.Request, parsed reflect.Value, parameters map[string]string,
) (int, error) {
	// get multipart boundary
	boundary, ok := parameters["boundary"]
	if !ok {
		return http.StatusBadRequest, exception.String("HttpServer: Boundary is missing in Content-Type of a multipart/form-data")
	}
	parsed.FieldByIndex(tags.multipartFieldIndex).Set(reflect.ValueOf(multipart.NewReader(request.Body, boundary)))
	return 0, nil
}

func (tags *requestTags) bindBody(request *http.Request, parsed reflect.Value) {
	parsed.FieldByIndex(tags.bodyFieldIndex).Set(reflect.ValueOf(request.Body))
}

func bind(tag string, input any, output reflect.Value) error {
	if decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:           internalDecodeHookFunc,
		Squash:               true,
		Result:               output.Addr().Interface(),
		TagName:              tag,
		SquashTagOption:      "\xFF\xFF\xFF\xFF", // sentinel that cannot match any real tag name, disabling squash-by-tag
		IgnoreUntaggedFields: true,
	}); err != nil {
		return exception.String("HttpServer: Create decoder failed").AddCause(err)
	} else if err := decoder.Decode(input); err != nil {
		return exception.String("HttpServer: Decode failed").AddCause(err)
	}
	return nil
}

//endregion parseRequest

// region mapstructure

var typeForString = reflect.TypeFor[string]()
var typeForInt8 = reflect.TypeFor[int8]()
var typeForUint8 = reflect.TypeFor[uint8]()
var typeForInt16 = reflect.TypeFor[int16]()
var typeForUint16 = reflect.TypeFor[uint16]()
var typeForInt32 = reflect.TypeFor[int32]()
var typeForUint32 = reflect.TypeFor[uint32]()
var typeForInt64 = reflect.TypeFor[int64]()
var typeForUint64 = reflect.TypeFor[uint64]()
var typeForInt = reflect.TypeFor[int]()
var typeForUint = reflect.TypeFor[uint]()
var typeForFloat32 = reflect.TypeFor[float32]()
var typeForFloat64 = reflect.TypeFor[float64]()
var typeForBool = reflect.TypeFor[bool]()
var typeForComplex64 = reflect.TypeFor[complex64]()
var typeForComplex128 = reflect.TypeFor[complex128]()
var typeForJsonNumber = reflect.TypeFor[json.Number]()

func internalDecodeHookFunc(source, target reflect.Value) (any, error) {
	// same type, no need to change
	if source.Type() == target.Type() {
		target.Set(source)
		return nil, nil
	}
	{ // check if source and target is pure slice
		sourceIsPureSlice := source.Kind() == reflect.Slice && source.NumMethod() == 0 &&
			(!source.CanAddr() || source.Addr().NumMethod() == 0)
		targetIsPureSlice := target.Kind() == reflect.Slice && target.NumMethod() == 0 &&
			(!target.CanAddr() || target.Addr().NumMethod() == 0)
		// if both are pure slice, short circuit
		if sourceIsPureSlice && targetIsPureSlice {
			return source.Interface(), nil
		}
		// if source is not a pure slice and target is a pure slice
		if targetIsPureSlice {
			// wrap source into a single element slice
			wrapper := reflect.MakeSlice(reflect.SliceOf(source.Type()), 1, 1)
			wrapper.Index(0).Set(source)
			return wrapper.Interface(), nil
		}
		// if source is a pure slice with single element, and target is not a pure slice
		if sourceIsPureSlice && source.Len() == 1 {
			source = source.Index(0)
		}
	}
	{ // get pointer to target
		targetOrPointer := target
		if target.CanAddr() {
			targetOrPointer = target.Addr()
		}
		// check if target implements mapstructure.Unmarshaler
		if targetUnmarshaller, ok := reflect.TypeAssert[mapstructure.Unmarshaler](targetOrPointer); ok {
			if err := targetUnmarshaller.UnmarshalMapstructure(source.Interface()); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	// check if input is string or json.Number
	if target.CanAddr() {
		switch source.Type() {
		case typeForString, typeForJsonNumber:
			sourceString, targetType := source.String(), target.Type()
			switch targetType {
			case typeForInt, typeForInt8, typeForInt16, typeForInt32, typeForInt64:
				if parsed, err := strconv.ParseInt(sourceString, 0, targetType.Bits()); err != nil {
					return nil, err
				} else {
					target.SetInt(parsed)
				}
			case typeForUint, typeForUint8, typeForUint16, typeForUint32, typeForUint64:
				if parsed, err := strconv.ParseUint(sourceString, 0, targetType.Bits()); err != nil {
					return nil, err
				} else {
					target.SetUint(parsed)
				}
			case typeForFloat32, typeForFloat64:
				if parsed, err := strconv.ParseFloat(sourceString, targetType.Bits()); err != nil {
					return nil, err
				} else {
					target.SetFloat(parsed)
				}
			case typeForBool:
				if parsed, err := strconv.ParseBool(sourceString); err != nil {
					return nil, err
				} else {
					target.SetBool(parsed)
				}
			case typeForComplex64, typeForComplex128:
				if parsed, err := strconv.ParseComplex(sourceString, targetType.Bits()); err != nil {
					return nil, err
				} else {
					target.SetComplex(parsed)
				}
			default:
				// check if target implements encoding.TextUnmarshaler
				if targetUnmarshaller, ok := reflect.TypeAssert[encoding.TextUnmarshaler](target.Addr()); ok {
					if err := targetUnmarshaller.UnmarshalText(unsafeStringToBytes(sourceString)); err != nil {
						return nil, err
					}
					return nil, nil
				}
				return source.Interface(), nil
			}
			return nil, nil
		}
	}
	return source.Interface(), nil
}

func internalDecodeString(sourceString string, target reflect.Value) error {
	switch targetType := target.Type(); targetType {
	case typeForString:
		target.SetString(sourceString)
	case typeForInt, typeForInt8, typeForInt16, typeForInt32, typeForInt64:
		if parsed, err := strconv.ParseInt(sourceString, 0, targetType.Bits()); err != nil {
			return err
		} else {
			target.SetInt(parsed)
		}
	case typeForUint, typeForUint8, typeForUint16, typeForUint32, typeForUint64:
		if parsed, err := strconv.ParseUint(sourceString, 0, targetType.Bits()); err != nil {
			return err
		} else {
			target.SetUint(parsed)
		}
	case typeForFloat32, typeForFloat64:
		if parsed, err := strconv.ParseFloat(sourceString, targetType.Bits()); err != nil {
			return err
		} else {
			target.SetFloat(parsed)
		}
	case typeForBool:
		if parsed, err := strconv.ParseBool(sourceString); err != nil {
			return err
		} else {
			target.SetBool(parsed)
		}
	case typeForComplex64, typeForComplex128:
		if parsed, err := strconv.ParseComplex(sourceString, targetType.Bits()); err != nil {
			return err
		} else {
			target.SetComplex(parsed)
		}
	default:
		// check if target implements mapstructure.Unmarshaler
		if targetUnmarshaller, ok := reflect.TypeAssert[mapstructure.Unmarshaler](target.Addr()); ok {
			return targetUnmarshaller.UnmarshalMapstructure(sourceString)
		}
		// check if target implements encoding.TextUnmarshaler
		if targetUnmarshaller, ok := reflect.TypeAssert[encoding.TextUnmarshaler](target.Addr()); ok {
			return targetUnmarshaller.UnmarshalText(unsafeStringToBytes(sourceString))
		}
		return exception.String("HttpServer: Default value unmarshal with unknown type")
	}
	return nil
}

//endregion mapstructure

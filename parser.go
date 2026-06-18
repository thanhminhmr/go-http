/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

type RequestHandler[Request any] = func(ctx Context, request Request) (Response, error)

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
//     type [Header], and only one field with this tag is allowed per struct.
//
//   - `cookie`: If the tag value is not empty, the tag value must match the cookie
//     name, and the field must be of type *[Cookie]. If the tag value is empty, the
//     field must be of type []*[Cookie], and only one field with this tag is allowed
//     per struct.
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
//   - `json`: The tag value must be empty. Only one field with this tag
//     is allowed per struct. The request body is unmarshalled into this field using
//     `encoding/json`. Any type validation is handled by the JSON unmarshalling
//     process.
//
//   - `multipart`: The tag value must be empty. Only one field with this tag
//     is allowed per struct. The field must be of type [*multipart.Reader].
//
//   - `body`: Only one field with this tag is allowed per struct. The field must
//     be of type [io.ReadCloser]. If the tag value is not empty, the tag value must
//     be a semicolon-separated list of accepted Content-Types, and if `form`, `json`
//     or `multipart` tag exists, the list must not contain those types. If the tag
//     value is empty, the field will be mapped if no other body type are matched.
func RequestParser[Request any](handler RequestHandler[Request]) http.HandlerFunc {
	tags := createTags(reflect.TypeFor[Request]())
	return func(writer http.ResponseWriter, request *http.Request) {
		var parsed Request
		requestHandler(writer, request, &tags, &parsed, func() (Response, error) {
			return handler(Context{Context: request.Context(), header: writer.Header()}, parsed)
		})
	}
}

var requestValidator = validator.New(validator.WithRequiredStructEnabled())

func requestHandler(
	writer http.ResponseWriter, request *http.Request, tags *requestTags,
	parsed any, handler func() (Response, error),
) {
	logger := ctrl.Logger(request.Context())
	if status, err := tags.parse(request, reflect.ValueOf(parsed).Elem()); err != nil {
		logger.Error().Err(err).Msg("Failed to parse request")
		writer.WriteHeader(status)
		return
	}
	if err := requestValidator.Struct(parsed); err != nil {
		logger.Error().Err(err).Msg("Failed to validate request")
		writer.WriteHeader(StatusBadRequest)
		return
	}
	logger.Debug().Any("parsed", parsed).Msg("Request parsed")
	response, err := handler()
	if err != nil {
		logger.Debug().Err(err).Msg("Handler returned with error")
		if response.status == 0 {
			response = Response{status: StatusInternalServerError}
		}
	} else {
		logger.Debug().Any("response", response).Msg("Response returned")
	}
	if err := response.write(writer); err != nil {
		logger.Debug().Err(err).Msg("Failed to write response")
	}
}

//region requestTags

type requestTags struct {
	flags               uint
	headerFieldIndex    []int
	cookieFieldIndex    []int
	cookiesFieldMap     map[string][][]int
	queryFieldIndex     []int
	urlFieldIndex       []int
	formFieldIndex      []int
	jsonFieldIndex      []int
	multipartFieldIndex []int
	bodyFieldIndex      []int
	bodyContentTypes    []string
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

func createTags(requestType reflect.Type) requestTags {
	if requestType.Kind() != reflect.Struct {
		panic("BUG: parsed request must be a struct")
	}
	tags := requestTags{}
	tags.checkRecursively(requestType)
	if tags.flags&tagForm != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsForm) {
		panic("BUG: `form` tag field is not allowed when `body` tag contains " + contentTypeIsForm)
	}
	if tags.flags&tagJson != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsJson) {
		panic("BUG: `json` tag field is not allowed when `body` tag contains " + contentTypeIsJson)
	}
	if tags.flags&tagMultipart != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsMultipart) {
		panic("BUG: `multipart` tag field is not allowed when `body` tag contains " + contentTypeIsMultipart)
	}
	return tags
}

func (tags *requestTags) checkRecursively(requestType reflect.Type) {
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
			tags.checkRecursively(requestType)
			continue
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
				if field.Type != reflect.TypeFor[Header]() {
					panic("BUG: empty `header` tag field must be a `http.Header`")
				}
				tags.headerFieldIndex = field.Index
			}
			tags.flags = tags.flags | tagHeader
		}
		// process cookie tag
		if value, exists := field.Tag.Lookup("cookie"); exists {
			if value != "" {
				if tags.cookieFieldIndex != nil {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
				if field.Type != reflect.TypeFor[*Cookie]() {
					panic("BUG: `cookie` tag field must be a `*http.Cookie`")
				}
				if tags.cookiesFieldMap == nil {
					tags.cookiesFieldMap = map[string][][]int{}
				}
				tags.cookiesFieldMap[value] = append(tags.cookiesFieldMap[value], field.Index)
			} else {
				if tags.flags&tagCookie != 0 {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
				if field.Type != reflect.TypeFor[[]*Cookie]() {
					panic("BUG: empty `cookie` tag field must be a `[]*http.Cookie`")
				}
				tags.cookieFieldIndex = field.Index
			}
			tags.flags = tags.flags | tagCookie
		}
		// process query tag
		if value, exists := field.Tag.Lookup("query"); exists {
			if value != "" {
				if tags.cookieFieldIndex != nil {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
			} else {
				if tags.flags&tagQuery != 0 {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
				if field.Type != reflect.TypeFor[KeyValues]() {
					panic("BUG: empty `query` tag field must be a `http.KeyValues`")
				}
				tags.queryFieldIndex = field.Index
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
				if field.Type != reflect.TypeFor[KeyValue]() {
					panic("BUG: empty `url` tag field must be a `http.KeyValue`")
				}
				tags.urlFieldIndex = field.Index
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
				if field.Type != reflect.TypeFor[KeyValues]() {
					panic("BUG: empty `form` tag field must be a `http.KeyValues`")
				}
				tags.formFieldIndex = field.Index
			}
			tags.flags = tags.flags | tagForm
		}
		// process json tag
		if value, exists := field.Tag.Lookup("json"); exists {
			if value != "" {
				panic("BUG: `json` tag value must be empty")
			}
			if tags.flags&tagJson != 0 {
				panic("BUG: multiple `json` tag fields are not allowed")
			}
			tags.flags = tags.flags | tagJson
			tags.jsonFieldIndex = field.Index
		}
		// process multipart tag
		if value, exists := field.Tag.Lookup("multipart"); exists {
			if value != "" {
				panic("BUG: `multipart` tag value must be empty")
			}
			if tags.flags&tagMultipart != 0 {
				panic("BUG: multiple `multipart` tag fields are not allowed")
			}
			if field.Type != reflect.TypeFor[*multipart.Reader]() {
				panic("BUG: `multipart` tag field must be a `*multipart.Reader`")
			}
			tags.flags = tags.flags | tagMultipart
			tags.multipartFieldIndex = field.Index
		}
		// process `body` tag
		if value, exists := field.Tag.Lookup("body"); exists {
			if tags.flags&tagBody != 0 {
				panic("BUG: multiple `body` tag fields are not allowed")
			}
			if field.Type != reflect.TypeFor[io.ReadCloser]() {
				panic("BUG: `body` tag field must be a `io.ReadCloser`")
			}
			tags.flags = tags.flags | tagBody
			tags.bodyFieldIndex = field.Index
			if value != "" {
				tags.bodyContentTypes = strings.Split(value, ";")
			}
		}
	}
}

//endregion requestTags

//region parseRequest

func (tags *requestTags) parse(request *http.Request, parsed reflect.Value) (status Status, parseErr error) {
	// parse and bind request header
	if tags.flags&tagHeader != 0 {
		if status, parseErr = tags.bindHeader(request, parsed); parseErr != nil {
			return
		}
	}
	// parse and bind cookies
	if tags.flags&tagCookie != 0 {
		tags.bindCookie(request, parsed)
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
		contentTypeHeader := request.Header.Get("Content-Type")
		if contentTypeHeader == "" {
			return StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is missing")
		}
		// parse media type
		contentType, contentTypeParameters, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			return StatusBadRequest, exception.String("HttpServer: Content-Type is invalid").AddCause(err)
		}
		// parse and bind request body as form
		if tags.flags&tagForm != 0 && contentType == contentTypeIsForm {
			if reader, err := charsetReader(request.Body, contentTypeParameters); err != nil {
				return StatusUnsupportedMediaType,
					exception.String("HttpServer: cannot determine body encoding").AddCause(err)
			} else {
				return tags.bindForm(reader, parsed)
			}
		}
		// parse and bind request body as JSON
		if tags.flags&tagJson != 0 && contentType == contentTypeIsJson {
			if reader, err := charsetReader(request.Body, contentTypeParameters); err != nil {
				return StatusUnsupportedMediaType,
					exception.String("HttpServer: cannot determine body encoding").AddCause(err)
			} else {
				return tags.bindJson(reader, parsed)
			}
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
		return StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is unsupported")
	}
	return
}

func (tags *requestTags) bindHeader(request *http.Request, parsed reflect.Value) (Status, error) {
	// parse and bind request header
	if len(request.Header) > 0 {
		if tags.headerFieldIndex != nil {
			parsed.FieldByIndex(tags.headerFieldIndex).Set(reflect.ValueOf(request.Header))
		} else if err := bind("header", request.Header, parsed); err != nil {
			return StatusBadRequest, exception.String("HttpServer: Bind request header failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindCookie(request *http.Request, parsed reflect.Value) {
	// parse and bind cookies
	if cookies := request.Cookies(); len(cookies) > 0 {
		if tags.cookieFieldIndex != nil {
			parsed.FieldByIndex(tags.cookieFieldIndex).Set(reflect.ValueOf(cookies))
		} else {
			for _, cookie := range cookies {
				if fields, exists := tags.cookiesFieldMap[cookie.Name]; exists {
					for _, field := range fields {
						parsed.FieldByIndex(field).Set(reflect.ValueOf(cookie))
					}
				}
			}
		}
	}
}

func (tags *requestTags) bindQuery(request *http.Request, parsed reflect.Value) (Status, error) {
	// parse and bind url query values
	if values := request.URL.Query(); len(values) > 0 {
		if tags.queryFieldIndex != nil {
			parsed.FieldByIndex(tags.queryFieldIndex).Set(reflect.ValueOf(values))
		} else if err := bind("query", values, parsed); err != nil {
			return StatusBadRequest, exception.String("HttpServer: Bind query values failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindUrl(request *http.Request, parsed reflect.Value) (Status, error) {
	// parse and bind url parameters
	routeContext := chi.RouteContext(request.Context())
	if len(routeContext.URLParams.Keys) > 0 {
		urlParams := map[string]string{}
		for index, key := range routeContext.URLParams.Keys {
			urlParams[key] = routeContext.URLParams.Values[index]
		}
		if tags.urlFieldIndex != nil {
			parsed.FieldByIndex(tags.urlFieldIndex).Set(reflect.ValueOf(urlParams))
		} else if err := bind("url", urlParams, parsed); err != nil {
			return StatusBadRequest, exception.String("HttpServer: Bind url params failed").AddCause(err)
		}
	}
	return 0, nil
}

func (tags *requestTags) bindForm(reader io.Reader, parsed reflect.Value) (Status, error) {
	// read the whole body at once
	body, err := io.ReadAll(reader)
	if err != nil && err != io.EOF {
		return StatusInternalServerError, exception.String("HttpServer: Read request body failed").AddCause(err)
	}
	// parse form body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return StatusBadRequest, exception.String("HttpServer: Parse form body failed").AddCause(err)
	}
	// bind form body
	if tags.formFieldIndex != nil {
		parsed.FieldByIndex(tags.formFieldIndex).Set(reflect.ValueOf(values))
	} else if err := bind("form", values, parsed); err != nil {
		return StatusBadRequest, exception.String("HttpServer: Bind form params failed").AddCause(err)
	}
	return 0, nil
}

func (tags *requestTags) bindJson(reader io.Reader, parsed reflect.Value) (Status, error) {
	// decode the whole body to the JSON field
	jsonField := parsed.FieldByIndex(tags.jsonFieldIndex).Addr().Interface()
	if err := json.NewDecoder(reader).Decode(jsonField); err != nil {
		return StatusBadRequest, exception.String("HttpServer: Decode JSON body failed").AddCause(err)
	}
	return 0, nil
}

func (tags *requestTags) bindMultipart(
	request *http.Request, parsed reflect.Value, parameters map[string]string,
) (Status, error) {
	// get multipart boundary
	boundary, ok := parameters["boundary"]
	if !ok {
		return StatusBadRequest, exception.String("HttpServer: Boundary is missing in Content-Type of a multipart/form-data")
	}
	parsed.FieldByIndex(tags.multipartFieldIndex).Set(reflect.ValueOf(multipart.NewReader(request.Body, boundary)))
	return 0, nil
}

func (tags *requestTags) bindBody(request *http.Request, parsed reflect.Value) {
	parsed.FieldByIndex(tags.bodyFieldIndex).Set(reflect.ValueOf(request.Body))
}

func bind(tag string, input any, output reflect.Value) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:           internalDecodeHookFunc,
		WeaklyTypedInput:     true,
		Squash:               true,
		Result:               output.Addr().Interface(),
		TagName:              tag,
		SquashTagOption:      string([]byte{0xFF, 0xFF, 0xFF, 0xFF}), // No squash tag
		IgnoreUntaggedFields: true,
	})
	if err != nil {
		return exception.String("HttpServer: Create decoder failed").AddCause(err)
	}
	if err := decoder.Decode(input); err != nil {
		return exception.String("Decode failed").AddCause(err)
	}
	return nil
}

//endregion parseRequest

//region mapstructure

var internalDecodeHookFunc = mapstructure.ComposeDecodeHookFunc(
	mapstructure.TextUnmarshallerHookFunc(),
	mapstructure.StringToBasicTypeHookFunc(),
	mapstructure.StringToTimeHookFunc(time.RFC3339Nano),
	mapstructure.StringToURLHookFunc(),
	mapstructure.StringToIPHookFunc(),
	mapstructure.StringToIPNetHookFunc(),
	mapstructure.StringToNetIPAddrHookFunc(),
	mapstructure.StringToNetIPAddrPortHookFunc(),
	mapstructure.StringToNetIPPrefixHookFunc(),
	unboxIfElementSliceHasSingleElement,
)

func unboxIfElementSliceHasSingleElement(from reflect.Value, to reflect.Value) (any, error) {
	// convert single value slice to value
	if from.Kind() == reflect.Slice && from.Len() == 1 {
		toType := to.Type()
		for toType.Kind() == reflect.Ptr {
			toType = toType.Elem()
		}
		if toType.Kind() != reflect.Slice {
			return from.Index(0).Interface(), nil
		}
	}
	return from.Interface(), nil
}

//endregion mapstructure

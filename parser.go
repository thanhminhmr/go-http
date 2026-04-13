/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"context"
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
	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-exception"
	"golang.org/x/net/html/charset"
)

type ServerRequestHandler[ServerRequest any] func(ctx context.Context, request *ServerRequest) ServerResponse

// ServerRequestParser parses an HTTP request and populates a struct using field
// tags to map request data to struct fields.
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
//     type [http.Header], and only one field with this tag is allowed per struct.
//
//   - `cookie`: If the tag value is not empty, the tag value must match the cookie
//     name, and the field must be of type *[http.Cookie]. If the tag value is empty,
//     the field must be of type []*[http.Cookie], and only one field with this tag
//     is allowed per struct.
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
//     is allowed per struct. The field must be of type [multipart.Reader].
//
//   - `body`: Only one field with this tag is allowed per struct. The field must
//     be of type [io.ReadCloser]. If the tag value is not empty, the tag value must
//     be a semicolon-separated list of accepted Content-Types, and if `form`, `json`
//     or `multipart` tag exists, the list must not contain those types. If the tag
//     value is empty, the field will be mapped if no other body type are matched.
func ServerRequestParser[ServerRequest any](handler ServerRequestHandler[ServerRequest]) http.HandlerFunc {
	tags := createTags(reflect.TypeFor[ServerRequest]())
	return func(writer http.ResponseWriter, request *http.Request) {
		var parsed ServerRequest
		serverRequestHandler(writer, request, &parsed, tags, func() ServerResponse {
			return handler(request.Context(), &parsed)
		})
	}
}

func serverRequestHandler(
	writer http.ResponseWriter,
	request *http.Request,
	parsed any,
	tags serverRequestTags,
	handler func() ServerResponse,
) {
	logger := zerolog.Ctx(request.Context())
	if errorResponse := tags.parse(request, parsed); errorResponse != nil {
		logger.Error().Err(errorResponse).Msg("Failed to parse request")
		if err := errorResponse.Render(writer); err != nil {
			logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}
	logger.Trace().Any("request", parsed).Msg("Request parsed")
	if renderer := handler(); renderer != nil {
		logger.Trace().Any("response", funcOrAny(renderer)).Msg("Response returned")
		if err := renderer.Render(writer); err != nil {
			logger.Error().Err(err).Msg("Failed to render response")
		}
	} else {
		logger.Trace().Msg("Empty response returned")
		writer.WriteHeader(http.StatusNoContent)
	}
}

//region serverRequestTags

type serverRequestTags struct {
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

func createTags(requestType reflect.Type) serverRequestTags {
	if requestType.Kind() != reflect.Struct {
		panic("BUG: parsed request must be a struct")
	}
	tags := serverRequestTags{}
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

func (tags *serverRequestTags) checkRecursively(requestType reflect.Type) {
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
				if field.Type != reflect.TypeFor[http.Header]() {
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
				if field.Type != reflect.TypeFor[*http.Cookie]() {
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
				if field.Type != reflect.TypeFor[[]*http.Cookie]() {
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
			if field.Type != reflect.TypeFor[multipart.Reader]() {
				panic("BUG: `multipart` tag field must be a `multipart.Reader`")
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

//endregion serverRequestTags

//region parseServerRequest

var serverRequestValidator = validator.New(validator.WithRequiredStructEnabled())

func (tags *serverRequestTags) parse(request *http.Request, parsed any) (errorResponse *ServerErrorResponse) {
	// parse and bind request header
	if tags.flags&tagHeader != 0 {
		if err := tags.bindHeader(request, parsed); err != nil {
			return err
		}
	}
	// parse and bind cookies
	if tags.flags&tagCookie != 0 {
		tags.bindCookie(request, parsed)
	}
	// parse and bind url query values
	if tags.flags&tagQuery != 0 {
		if err := tags.bindQuery(request, parsed); err != nil {
			return err
		}
	}
	// parse and bind url parameters
	if tags.flags&tagUrl != 0 {
		if err := tags.bindUrl(request, parsed); err != nil {
			return err
		}
	}
	// validate body later
	defer func() {
		if errorResponse != nil {
			return
		}
		if err := serverRequestValidator.Struct(parsed); err != nil {
			errorResponse = &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Request body is not valid").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
	}()
	// parse and bind body
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		contentTypeHeader := request.Header.Get("Content-Type")
		if contentTypeHeader == "" {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Content-Type is missing"),
				Status: http.StatusUnsupportedMediaType,
			}
		}
		// parse media type
		contentType, contentTypeParameters, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Content-Type is invalid").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
		// parse and bind request body as form
		if tags.flags&tagForm != 0 && contentType == contentTypeIsForm {
			if reader, err := charset.NewReader(request.Body, contentTypeHeader); err != nil {
				return &ServerErrorResponse{
					Cause:  exception.String("HttpServer: cannot determine body encoding").AddCause(err),
					Status: http.StatusUnsupportedMediaType,
				}
			} else {
				return tags.bindForm(reader, parsed)
			}
		}
		// parse and bind request body as JSON
		if tags.flags&tagJson != 0 && contentType == contentTypeIsJson {
			if reader, err := charset.NewReader(request.Body, contentTypeHeader); err != nil {
				return &ServerErrorResponse{
					Cause:  exception.String("HttpServer: cannot determine body encoding").AddCause(err),
					Status: http.StatusUnsupportedMediaType,
				}
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
			return nil
		}
		// nothing matched
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Content-Type is unsupported"),
			Status: http.StatusUnsupportedMediaType,
		}
	}
	return nil
}

func (tags *serverRequestTags) bindHeader(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind request header
	if len(request.Header) > 0 {
		if tags.headerFieldIndex != nil {
			reflect.ValueOf(parsed).Elem().FieldByIndex(tags.headerFieldIndex).Set(reflect.ValueOf(request.Header))
		} else if err := bind("header", request.Header, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind request header failed").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
	}
	return nil
}

func (tags *serverRequestTags) bindCookie(request *http.Request, parsed any) {
	// parse and bind cookies
	if cookies := request.Cookies(); len(cookies) > 0 {
		structValue := reflect.ValueOf(parsed).Elem()
		if tags.cookieFieldIndex != nil {
			structValue.FieldByIndex(tags.cookieFieldIndex).Set(reflect.ValueOf(cookies))
		} else {
			for _, cookie := range cookies {
				if fields, exists := tags.cookiesFieldMap[cookie.Name]; exists {
					for _, field := range fields {
						structValue.FieldByIndex(field).Set(reflect.ValueOf(cookie))
					}
				}
			}
		}
	}
}

func (tags *serverRequestTags) bindQuery(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind url query values
	if values := request.URL.Query(); len(values) > 0 {
		if tags.queryFieldIndex != nil {
			reflect.ValueOf(parsed).Elem().FieldByIndex(tags.queryFieldIndex).Set(reflect.ValueOf(values))
		} else if err := bind("query", values, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind query values failed").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
	}
	return nil
}

func (tags *serverRequestTags) bindUrl(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind url parameters
	routeContext := chi.RouteContext(request.Context())
	if len(routeContext.URLParams.Keys) > 0 {
		urlParams := map[string]string{}
		for index, key := range routeContext.URLParams.Keys {
			urlParams[key] = routeContext.URLParams.Values[index]
		}
		if tags.urlFieldIndex != nil {
			reflect.ValueOf(parsed).Elem().FieldByIndex(tags.urlFieldIndex).Set(reflect.ValueOf(urlParams))
		} else if err := bind("url", urlParams, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind url params failed").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
	}
	return nil
}

func (tags *serverRequestTags) bindForm(reader io.Reader, parsed any) *ServerErrorResponse {
	// read the whole body at once
	body, err := io.ReadAll(reader)
	if err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Read request body failed").AddCause(err),
			Status: http.StatusInternalServerError,
		}
	}
	// parse form body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Parse form body failed").AddCause(err),
			Status: http.StatusBadRequest,
		}
	}
	// bind form body
	if tags.formFieldIndex != nil {
		reflect.ValueOf(parsed).Elem().FieldByIndex(tags.formFieldIndex).Set(reflect.ValueOf(values))
	} else if err := bind("form", values, parsed); err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Bind form params failed").AddCause(err),
			Status: http.StatusBadRequest,
		}
	}
	return nil
}

func (tags *serverRequestTags) bindJson(reader io.Reader, parsed any) *ServerErrorResponse {
	// decode the whole body to the JSON field
	fieldAsInterface := reflect.ValueOf(parsed).Elem().FieldByIndex(tags.jsonFieldIndex).Addr().Interface()
	if err := json.NewDecoder(reader).Decode(fieldAsInterface); err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Decode JSON body failed").AddCause(err),
			Status: http.StatusBadRequest,
		}
	}
	return nil
}

func (tags *serverRequestTags) bindMultipart(request *http.Request, parsed any, parameters map[string]string) *ServerErrorResponse {
	// get multipart boundary
	boundary, ok := parameters["boundary"]
	if !ok {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Boundary is missing in Content-Type of a multipart/form-data"),
			Status: http.StatusBadRequest,
		}
	}
	reflect.ValueOf(parsed).Elem().FieldByIndex(tags.multipartFieldIndex).
		Set(reflect.ValueOf(multipart.NewReader(request.Body, boundary)))
	return nil
}

func (tags *serverRequestTags) bindBody(request *http.Request, parsed any) {
	reflect.ValueOf(parsed).Elem().FieldByIndex(tags.bodyFieldIndex).
		Set(reflect.ValueOf(request.Body))
}

func bind(tag string, input any, output any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:           internalDecodeHookFunc,
		WeaklyTypedInput:     true,
		Squash:               true,
		Result:               output,
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

//endregion parseServerRequest

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

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
//   - `header`: The tag value must match the normalized HTTP header name.
//
//   - `cookie`: The tag value must match the cookie name.
//
//   - `query`: The tag value must match the query parameter name.
//
//   - `url`: The tag value must match a named segment in the URL path.
//
//   - `form`: The tag value must match the form parameter name.
//
//   - `json`: The tag value must be empty. Only one field with this tag
//     is allowed per struct. The request body is unmarshalled into this field using
//     `encoding/json`. Any type validation is handled by the JSON unmarshalling
//     process.
//
//   - `multipart`: The tag value must be empty. Only one field with this tag
//     is allowed per struct. The field must be of type [multipart.Reader].
//
//   - `body`: The tag value must be a semicolon-separated list of accepted
//     Content-Types. Only one field with this tag is allowed per struct.
//     The field must be of type [io.ReadCloser].
func ServerRequestParser[ServerRequest any](handler ServerRequestHandler[ServerRequest]) http.HandlerFunc {
	tags := checkServerRequestConfiguration[ServerRequest]()
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
	tags serverRequestConfiguration,
	handler func() ServerResponse,
) {
	logger := zerolog.Ctx(request.Context())
	if errorResponse := parseServerRequest(request, parsed, tags); errorResponse != nil {
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

//region serverRequestConfiguration

type serverRequestConfiguration struct {
	flags               uint
	jsonFieldIndex      int
	multipartFieldIndex int
	bodyFieldIndex      int
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

func checkServerRequestConfiguration[ServerRequest any]() serverRequestConfiguration {
	requestType := reflect.TypeFor[ServerRequest]()
	if requestType.Kind() != reflect.Struct {
		panic("BUG: ServerRequest must be a struct")
	}
	tags := serverRequestConfiguration{}
	for index := range requestType.NumField() {
		field := requestType.Field(index)
		if tags.flags&tagHeader == 0 {
			if _, exists := field.Tag.Lookup("header"); exists {
				tags.flags = tags.flags | tagHeader
			}
		}
		if tags.flags&tagCookie == 0 {
			if _, exists := field.Tag.Lookup("cookie"); exists {
				tags.flags = tags.flags | tagCookie
			}
		}
		if tags.flags&tagQuery == 0 {
			if _, exists := field.Tag.Lookup("query"); exists {
				tags.flags = tags.flags | tagQuery
			}
		}
		if tags.flags&tagUrl == 0 {
			if _, exists := field.Tag.Lookup("url"); exists {
				tags.flags = tags.flags | tagUrl
			}
		}
		if tags.flags&tagForm == 0 {
			if _, exists := field.Tag.Lookup("form"); exists {
				tags.flags = tags.flags | tagForm
			}
		}
		if value, exists := field.Tag.Lookup("json"); exists {
			if value != "" {
				panic("BUG: json tag value must be empty")
			}
			if tags.flags&tagJson != 0 {
				panic("BUG: multiple json-tagged fields are not allowed")
			}
			tags.flags = tags.flags | tagJson
			tags.jsonFieldIndex = index
		}
		if value, exists := field.Tag.Lookup("multipart"); exists {
			if value != "" {
				panic("BUG: multipart tag value must be empty")
			}
			if tags.flags&tagMultipart != 0 {
				panic("BUG: multiple multipart-tagged fields are not allowed")
			}
			if field.Type != reflect.TypeFor[multipart.Reader]() {
				panic("BUG: multipart-tagged field must be a multipart.Reader")
			}
			tags.flags = tags.flags | tagMultipart
			tags.multipartFieldIndex = index
		}
		if value, exists := field.Tag.Lookup("body"); exists {
			if tags.flags&tagBody != 0 {
				panic("BUG: multiple body-tagged fields are not allowed")
			}
			if field.Type != reflect.TypeFor[io.ReadCloser]() {
				panic("BUG: body-tagged field must be a io.ReadCloser")
			}
			tags.flags = tags.flags | tagBody
			tags.bodyFieldIndex = index
			tags.bodyContentTypes = strings.Split(value, ";")
		}
	}
	return tags
}

//endregion serverRequestConfiguration

//region parseServerRequest

var serverRequestValidator = validator.New(validator.WithRequiredStructEnabled())

func parseServerRequest(request *http.Request, parsed any, tags serverRequestConfiguration) (errorResponse *ServerErrorResponse) {
	// parse and bind request header
	if tags.flags&tagHeader != 0 {
		if err := bindHeader(request, parsed); err != nil {
			return err
		}
	}
	// parse and bind cookies
	if tags.flags&tagCookie != 0 {
		if err := bindCookie(request, parsed); err != nil {
			return err
		}
	}
	// parse and bind url query values
	if tags.flags&tagQuery != 0 {
		if err := bindQuery(request, parsed); err != nil {
			return err
		}
	}
	// parse and bind url parameters
	if tags.flags&tagUrl != 0 {
		if err := bindUrl(request, parsed); err != nil {
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
		contentType := request.Header.Get("Content-Type")
		if contentType == "" {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Content-Type is missing"),
				Status: http.StatusUnsupportedMediaType,
			}
		}
		// parse media type
		contentType, contentTypeParameters, err := mime.ParseMediaType(contentType)
		if err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Content-Type is invalid").AddCause(err),
				Status: http.StatusBadRequest,
			}
		}
		// parse and bind request body as form
		if tags.flags&tagForm != 0 && contentType == "application/x-www-form-urlencoded" {
			return bindForm(request, parsed)
		}
		// parse and bind request body as JSON
		if tags.flags&tagJson != 0 && contentType == "application/json" {
			return bindJson(request, parsed, tags.jsonFieldIndex)
		}
		// parse and bind request body as multipart form
		if tags.flags&tagMultipart != 0 && contentType == "multipart/form-data" {
			return bindMultipart(request, parsed, tags.multipartFieldIndex, contentTypeParameters)
		}
		// bind request body raw
		if tags.flags&tagBody != 0 && slices.Contains(tags.bodyContentTypes, contentType) {
			bindBody(request, parsed, tags.bodyFieldIndex)
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

func bindHeader(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind request header
	if len(request.Header) > 0 {
		if err := bind("header", request.Header, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind request header failed").AddCause(err),
				Status: http.StatusInternalServerError,
			}
		}
	}
	return nil
}

func bindCookie(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind cookies
	if cookies := request.Cookies(); len(cookies) > 0 {
		cookieMap := map[string][]string{}
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = append(cookieMap[cookie.Name], cookie.Value)
		}
		if err := bind("cookie", cookieMap, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind request cookies failed").AddCause(err),
				Status: http.StatusInternalServerError,
			}
		}
	}
	return nil
}

func bindQuery(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind url query values
	if values := request.URL.Query(); len(values) > 0 {
		if err := bind("query", values, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind query values failed").AddCause(err),
				Status: http.StatusInternalServerError,
			}
		}
	}
	return nil
}

func bindUrl(request *http.Request, parsed any) *ServerErrorResponse {
	// parse and bind url parameters
	routeContext := chi.RouteContext(request.Context())
	if len(routeContext.URLParams.Keys) > 0 {
		urlParams := map[string]string{}
		for index, key := range routeContext.URLParams.Keys {
			urlParams[key] = routeContext.URLParams.Values[index]
		}
		if err := bind("url", urlParams, parsed); err != nil {
			return &ServerErrorResponse{
				Cause:  exception.String("HttpServer: Bind url params failed").AddCause(err),
				Status: http.StatusInternalServerError,
			}
		}
	}
	return nil
}

func bindForm(request *http.Request, parsed any) *ServerErrorResponse {
	// read the whole body at once
	body, err := io.ReadAll(request.Body)
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
	if err := bind("form", values, parsed); err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Bind form params failed").AddCause(err),
			Status: http.StatusInternalServerError,
		}
	}
	return nil
}

func bindJson(request *http.Request, parsed any, fieldIndex int) *ServerErrorResponse {
	// decode the whole body to the JSON field
	fieldAsInterface := reflect.ValueOf(parsed).Elem().Field(fieldIndex).Addr().Interface()
	if err := json.NewDecoder(request.Body).Decode(fieldAsInterface); err != nil {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Decode JSON body failed").AddCause(err),
			Status: http.StatusInternalServerError,
		}
	}
	return nil
}

func bindMultipart(request *http.Request, parsed any, fieldIndex int, parameters map[string]string) *ServerErrorResponse {
	// get multipart boundary
	boundary, ok := parameters["boundary"]
	if !ok {
		return &ServerErrorResponse{
			Cause:  exception.String("HttpServer: Boundary is missing in Content-Type of a multipart/form-data"),
			Status: http.StatusBadRequest,
		}
	}
	reflect.ValueOf(parsed).Elem().Field(fieldIndex).Set(reflect.ValueOf(multipart.NewReader(request.Body, boundary)))
	return nil
}

func bindBody(request *http.Request, parsed any, fieldIndex int) {
	reflect.ValueOf(parsed).Elem().Field(fieldIndex).Set(reflect.ValueOf(request.Body))
}

func bind(tag string, input any, output any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:           internalDecodeHookFunc,
		WeaklyTypedInput:     true,
		Result:               output,
		TagName:              tag,
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

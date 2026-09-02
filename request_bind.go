/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"mime"
	"net/http"
	"reflect"
	"slices"

	"github.com/thanhminhmr/go-common/common"
	"github.com/thanhminhmr/go-exception"
)

// parse binds request data into parsed. Non-body sources are applied in this
// order: header, cookie, query, URL path values. It then selects at most one body
// binder based on method and Content-Type. A zero status with nil error means
// success or that no applicable value was present; failures return the HTTP
// status that RequestParser should use for its empty error response.
func (tags *requestTags) parse(request *http.Request, parsed reflect.Value) (status int, parseErr error) {
	if tags.flags&tagHeader != 0 {
		if status, parseErr = tags.bindHeader(request, parsed); parseErr != nil {
			return
		}
	}
	if tags.flags&tagCookie != 0 {
		if status, parseErr = tags.bindCookie(request, parsed); parseErr != nil {
			return
		}
	}
	if tags.flags&tagQuery != 0 {
		if status, parseErr = tags.bindQuery(request, parsed); parseErr != nil {
			return
		}
	}
	if tags.flags&tagUrl != 0 {
		if status, parseErr = tags.bindUrl(request, parsed); parseErr != nil {
			return
		}
	}
	if tags.flags&(tagForm|tagJson|tagMultipart|tagBody) == 0 {
		return
	}
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if request.ContentLength == 0 {
			return
		}
		contentTypeHeader := request.Header.Get("Content-Type")
		if contentTypeHeader == "" {
			return http.StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is missing")
		}
		contentType, contentTypeParameters, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Content-Type is invalid").AddCause(err)
		}
		if tags.flags&tagForm != 0 && contentType == contentTypeIsForm {
			return bindFullTextBody(request, contentTypeParameters, parsed, tags.bindForm)
		}
		if tags.flags&tagJson != 0 && contentType == contentTypeIsJson {
			return bindFullTextBody(request, contentTypeParameters, parsed, tags.bindJson)
		}
		if tags.flags&tagMultipart != 0 && contentType == contentTypeIsMultipart {
			return tags.bindMultipart(request, parsed, contentTypeParameters)
		}
		if tags.flags&tagBody != 0 && (len(tags.bodyContentTypes) == 0 || slices.Contains(tags.bodyContentTypes, contentType)) {
			tags.bindBody(request, parsed)
			return
		}
		return http.StatusUnsupportedMediaType, exception.String("HttpServer: Content-Type is unsupported")
	}
	return
}

// bindHeader binds either the complete http.Header for an empty header tag or
// named header values through common.BindStructWithTag. Conversion failures are
// reported as 400 Bad Request.
func (tags *requestTags) bindHeader(request *http.Request, parsed reflect.Value) (int, error) {
	if len(request.Header) > 0 {
		if tags.headerFieldIndex != nil {
			parsed.FieldByIndex(tags.headerFieldIndex).Set(reflect.ValueOf(request.Header))
		} else if err := common.BindStructWithTag("header", request.Header, parsed.Addr().Interface()); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind request header failed").AddCause(err)
		}
	}
	return 0, nil
}

// bindCookie converts request cookies into KeyValues, preserving repeated cookie
// names, then binds either the complete map or named cookie values. Conversion
// failures are reported as 400 Bad Request.
func (tags *requestTags) bindCookie(request *http.Request, parsed reflect.Value) (int, error) {
	if cookies := request.Cookies(); len(cookies) > 0 {
		keyValues := make(KeyValues, len(cookies))
		for _, cookie := range cookies {
			keyValues[cookie.Name] = append(keyValues[cookie.Name], cookie.Value)
		}
		if tags.cookieFieldIndex != nil {
			parsed.FieldByIndex(tags.cookieFieldIndex).Set(reflect.ValueOf(keyValues))
		} else if err := common.BindStructWithTag("cookie", keyValues, parsed.Addr().Interface()); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind cookies failed").AddCause(err)
		}
	}
	return 0, nil
}

// bindQuery binds request.URL.Query either as the complete KeyValues map or as
// named query values. Conversion failures are reported as 400 Bad Request.
func (tags *requestTags) bindQuery(request *http.Request, parsed reflect.Value) (int, error) {
	if values := request.URL.Query(); len(values) > 0 {
		if tags.queryFieldIndex != nil {
			parsed.FieldByIndex(tags.queryFieldIndex).Set(reflect.ValueOf(values))
		} else if err := common.BindStructWithTag("query", values, parsed.Addr().Interface()); err != nil {
			return http.StatusBadRequest, exception.String("HttpServer: Bind query values failed").AddCause(err)
		}
	}
	return 0, nil
}

// bindUrl binds ServeMux wildcard values from [getPathValues]. Requests without
// a matched pattern or without named wildcards leave URL-tagged fields unchanged.
// Conversion failures are reported as 400 Bad Request.
func (tags *requestTags) bindUrl(request *http.Request, parsed reflect.Value) (int, error) {
	if request.Pattern == "" {
		return 0, nil
	}
	keyValue := getPathValues(request)
	if len(keyValue) == 0 {
		return 0, nil
	}
	if tags.urlFieldIndex != nil {
		parsed.FieldByIndex(tags.urlFieldIndex).Set(reflect.ValueOf(keyValue))
	} else if err := common.BindStructWithTag("url", keyValue, parsed.Addr().Interface()); err != nil {
		return http.StatusBadRequest, exception.String("HttpServer: Bind url params failed").AddCause(err)
	}
	return 0, nil
}

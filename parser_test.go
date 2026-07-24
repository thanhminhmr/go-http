/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// multipartReader is a type alias used by multiple test files.
type multipartReader = multipart.Reader

type capturedRequest[T any] struct {
	ctx     Context
	request T
}

// captureHandler is a RequestHandler that returns http.StatusOK. When used with
// doRequest, the request is captured automatically by doRequest's wrapper.
func captureHandler[T any](ctx Context, _ T) *Response {
	return ctx.Response(http.StatusOK)
}

type RequestSetter func(*http.Request)

func doRequest[T any](t *testing.T, handler RequestHandler[T], method, target string, setters ...RequestSetter) (*capturedRequest[T], *httptest.ResponseRecorder) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for _, setter := range setters {
		setter(req)
	}
	captured := &capturedRequest[T]{}
	wrappedHandler := RequestParser(func(ctx Context, req T) *Response {
		captured.ctx = ctx
		captured.request = req
		return handler(ctx, req)
	})
	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)
	return captured, rec
}

func doChiRequest[T any](t *testing.T, method, pattern, target string, handler RequestHandler[T], setters ...RequestSetter) (*capturedRequest[T], *httptest.ResponseRecorder) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for _, setter := range setters {
		setter(req)
	}
	captured := &capturedRequest[T]{}
	router := chi.NewRouter()
	router.MethodFunc(method, pattern, RequestParser(func(ctx Context, req T) *Response {
		captured.ctx = ctx
		captured.request = req
		return handler(ctx, req)
	}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return captured, rec
}

func withHeader(key, value string) RequestSetter {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

func withCookie(name, value string) RequestSetter {
	return func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
}

func withQuery(query string) RequestSetter {
	return func(req *http.Request) {
		req.URL.RawQuery = query
	}
}

func withJSONBody(body any) RequestSetter {
	return func(req *http.Request) {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			panic(err)
		}
		req.Body = io.NopCloser(buf)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.ContentLength = int64(buf.Len())
	}
}

func withFormBody(values url.Values) RequestSetter {
	return func(req *http.Request) {
		body := values.Encode()
		req.Body = io.NopCloser(strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = int64(len(body))
	}
}

func withRawBody(contentType string, data []byte) RequestSetter {
	return func(req *http.Request) {
		req.Body = io.NopCloser(bytes.NewReader(data))
		req.Header.Set("Content-Type", contentType)
		req.ContentLength = int64(len(data))
	}
}

func withMultipartBody(t *testing.T, buildForm func(*multipart.Writer)) RequestSetter {
	t.Helper()
	return func(req *http.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		buildForm(writer)
		if err := writer.Close(); err != nil {
			t.Fatalf("failed to close multipart writer: %v", err)
		}
		req.Body = io.NopCloser(body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.ContentLength = int64(body.Len())
	}
}

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
	"testing"
)

// Benchmarks split into two groups so the name reflects what is measured:
//   - BenchmarkParser_*: exercises RequestParser via [asTestHTTPHandler],
//     measuring parser + tag binding + response write, but NOT the real
//     [Router.Handle] / ServeMux dispatch.
//   - BenchmarkRouter_*: exercises the full stack via [newTestRouter] +
//     ServeMux, including middleware dispatch and the tracker path.

// benchPreflight runs handler once against req and asserts the recorder reports
// wantStatus. It exists so a benchmark cannot silently measure an error path:
// every benchmark must call it (or its router counterpart) before
// [testing.B.ResetTimer].
func benchPreflight(b *testing.B, handler http.HandlerFunc, req *http.Request, wantStatus int) {
	b.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		b.Fatalf("preflight status mismatch: want %d, got %d (body=%q)",
			wantStatus, rec.Code, rec.Body.String())
	}
}

// benchPreflightRouter is the [Router] counterpart of [benchPreflight]: it
// dispatches through r.serveMux once and asserts the resulting status.
func benchPreflightRouter(b *testing.B, r Router, req *http.Request, wantStatus int) {
	b.Helper()
	rec := httptest.NewRecorder()
	r.serveMux.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		b.Fatalf("preflight status mismatch: want %d, got %d (body=%q)",
			wantStatus, rec.Code, rec.Body.String())
	}
}

// ============ parser-only benchmarks ============

func BenchmarkParser_Query(b *testing.B) {
	type Req struct {
		Name string `query:"name"`
		Age  int    `query:"age"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	req, _ := http.NewRequest(http.MethodGet, "/?name=alice&age=30", nil)
	benchPreflight(b, handler, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkParser_Header(b *testing.B) {
	type Req struct {
		Name string `header:"X-Name"`
		Age  int    `header:"X-Age"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Name", "alice")
	req.Header.Set("X-Age", "30")
	benchPreflight(b, handler, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkParser_Form(b *testing.B) {
	type Req struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	formBody := url.Values{"name": {"alice"}, "email": {"alice@example.com"}}.Encode()
	makeReq := func() *http.Request {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(formBody)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = int64(len(formBody))
		return req
	}
	benchPreflight(b, handler, makeReq(), http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
	}
}

func BenchmarkParser_JSON(b *testing.B) {
	type Req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	bodyData, _ := json.Marshal(map[string]string{"name": "alice", "email": "alice@example.com"})
	makeReq := func() *http.Request {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(bodyData))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(bodyData))
		return req
	}
	benchPreflight(b, handler, makeReq(), http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
	}
}

func BenchmarkParser_Multipart(b *testing.B) {
	type Req struct {
		Reader *multipartReader `multipart:""`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	makeReq := func() *http.Request {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		field, _ := writer.CreateFormField("name")
		_, _ = field.Write([]byte("alice"))
		_ = writer.Close()
		req, _ := http.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.ContentLength = int64(body.Len())
		return req
	}
	benchPreflight(b, handler, makeReq(), http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
	}
}

func BenchmarkParser_RawBody(b *testing.B) {
	type Req struct {
		Body io.ReadCloser `body:""`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	data := []byte("raw body data for benchmarking")
	makeReq := func() *http.Request {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(data))
		return req
	}
	benchPreflight(b, handler, makeReq(), http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
	}
}

func BenchmarkParser_ComplexRequest(b *testing.B) {
	type Address struct {
		Street string `json:"street" validate:"required"`
		City   string `json:"city" validate:"required"`
	}
	type Req struct {
		Name    string  `header:"X-Name" validate:"required"`
		Token   string  `cookie:"session" validate:"required"`
		Page    int     `query:"page" validate:"min=1"`
		Address Address `json:"address" validate:"required"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	// Body shape must match the struct: top-level "address" object whose own
	// fields match [Address]. A flat body silently binds nothing.
	bodyData, _ := json.Marshal(map[string]any{
		"address": map[string]string{"street": "123 Main St", "city": "Springfield"},
	})
	makeReq := func() *http.Request {
		req, _ := http.NewRequest(http.MethodPost, "/?page=1", bytes.NewReader(bodyData))
		req.Header.Set("X-Name", "alice")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
		req.ContentLength = int64(len(bodyData))
		return req
	}
	benchPreflight(b, handler, makeReq(), http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
	}
}

func BenchmarkParser_Validation(b *testing.B) {
	type Req struct {
		Name  string `query:"name" validate:"required,min=2"`
		Email string `query:"email" validate:"required,email"`
		Age   int    `query:"age" validate:"required,min=18,max=120"`
	}
	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))
	req, _ := http.NewRequest(http.MethodGet, "/?name=alice&email=alice@example.com&age=30", nil)
	benchPreflight(b, handler, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkParser_ResponsePlainText(b *testing.B) {
	type Req struct{}
	handler := asTestHTTPHandler(RequestParser(func(ctx *Context, _ Req) {
		ctx.NewResponse(http.StatusOK).PlainTextBody("hello world")
	}))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	benchPreflight(b, handler, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkParser_ResponseJSON(b *testing.B) {
	type Req struct{}
	type Data struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
	handler := asTestHTTPHandler(RequestParser(func(ctx *Context, _ Req) {
		ctx.NewResponse(http.StatusOK).JsonBody(Data{Name: "alice", Email: "alice@example.com", Age: 30})
	}))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	benchPreflight(b, handler, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// ============ Router end-to-end benchmark ============

func BenchmarkRouter_FullStack_WithMiddleware(b *testing.B) {
	type Req struct {
		Name string `query:"name"`
	}
	router := newTestRouter()
	mw := func(ctx *Context, next func()) { next() }
	router.Group(mw).Handle(http.MethodGet+" /", RequestParser(captureHandler[Req]))
	req, _ := http.NewRequest(http.MethodGet, "/?name=alice", nil)
	benchPreflightRouter(b, router, req, http.StatusOK)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		router.serveMux.ServeHTTP(rec, req)
	}
}

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

// Benchmarks for request parsing and response writing across all tag types.

func BenchmarkQuery_Simple(b *testing.B) {
	type Req struct {
		Name string `query:"name"`
		Age  int    `query:"age"`
	}
	handler := RequestParser(captureHandler[Req])
	req, _ := http.NewRequest(http.MethodGet, "/?name=alice&age=30", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkHeader_Simple(b *testing.B) {
	type Req struct {
		Name string `header:"X-Name"`
		Age  int    `header:"X-Age"`
	}
	handler := RequestParser(captureHandler[Req])
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Name", "alice")
		req.Header.Set("X-Age", "30")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkJSON_Body(b *testing.B) {
	type Req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	handler := RequestParser(captureHandler[Req])
	bodyData, _ := json.Marshal(map[string]string{"name": "alice", "email": "alice@example.com"})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(bodyData))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(bodyData))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkForm_Body(b *testing.B) {
	type Req struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	}
	handler := RequestParser(captureHandler[Req])
	formBody := url.Values{"name": {"alice"}, "email": {"alice@example.com"}}.Encode()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(formBody)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = int64(len(formBody))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkValidation(b *testing.B) {
	type Req struct {
		Name  string `query:"name" validate:"required,min=2"`
		Email string `query:"email" validate:"required,email"`
		Age   int    `query:"age" validate:"required,min=18,max=120"`
	}
	handler := RequestParser(captureHandler[Req])
	req, _ := http.NewRequest(http.MethodGet, "/?name=alice&email=alice@example.com&age=30", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkResponse_PlainText(b *testing.B) {
	type Req struct{}
	handler := RequestParser(func(ctx Context, _ Req) *Response {
		return ctx.Response(http.StatusOK).PlainTextBody("hello world")
	})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkResponse_JSON(b *testing.B) {
	type Req struct{}
	type Data struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
	handler := RequestParser(func(ctx Context, _ Req) *Response {
		return ctx.Response(http.StatusOK).JsonBody(Data{Name: "alice", Email: "alice@example.com", Age: 30})
	})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkMultipart_Body(b *testing.B) {
	type Req struct {
		Reader *multipartReader `multipart:""`
	}
	handler := RequestParser(captureHandler[Req])
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		field, _ := writer.CreateFormField("name")
		_, _ = field.Write([]byte("alice"))
		_ = writer.Close()
		req, _ := http.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.ContentLength = int64(body.Len())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkComplex_Request(b *testing.B) {
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
	handler := RequestParser(captureHandler[Req])
	bodyData, _ := json.Marshal(map[string]string{"street": "123 Main St", "city": "Springfield"})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, "/?page=1", bytes.NewReader(bodyData))
		req.Header.Set("X-Name", "alice")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
		req.ContentLength = int64(len(bodyData))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkRawBody(b *testing.B) {
	type Req struct {
		Body io.ReadCloser `body:""`
	}
	handler := RequestParser(captureHandler[Req])
	data := []byte("raw body data for benchmarking")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(data))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

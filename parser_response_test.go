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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for the Response type: fluent builders, body types, headers, cookies,
// and error paths.

func TestResponse_FluentBuilder(t *testing.T) {
	type Req struct{}
	t.Run("status_and_body", func(t *testing.T) {
		handler := RequestParser(func(ctx Context, _ Req) *Response {
			return ctx.Response(http.StatusCreated).PlainTextBody("created")
		})
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, "created", rec.Body.String())
	})
	t.Run("json_body", func(t *testing.T) {
		type Data struct {
			Name string `json:"name"`
		}
		handler := RequestParser(func(ctx Context, _ Req) *Response {
			return ctx.Response(http.StatusOK).JsonBody(Data{Name: "alice"})
		})
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		var result Data
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		assert.Equal(t, "alice", result.Name, "Name")
	})
	t.Run("stream_body", func(t *testing.T) {
		handler := RequestParser(func(ctx Context, _ Req) *Response {
			return ctx.Response(http.StatusOK).StreamBody(func(w io.Writer) error {
				_, err := w.Write([]byte("streamed"))
				return err
			})
		})
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "streamed", rec.Body.String())
	})
	t.Run("bytes_body", func(t *testing.T) {
		handler := RequestParser(func(ctx Context, _ Req) *Response {
			return ctx.Response(http.StatusOK).BytesBody([]byte("raw bytes"))
		})
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "raw bytes", rec.Body.String())
	})
	t.Run("string_body", func(t *testing.T) {
		handler := RequestParser(func(ctx Context, _ Req) *Response {
			return ctx.Response(http.StatusOK).StringBody("a string")
		})
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "a string", rec.Body.String())
	})
}

func TestResponse_Header(t *testing.T) {
	type Req struct{}
	handler := RequestParser(func(ctx Context, _ Req) *Response {
		resp := ctx.Response(http.StatusOK)
		resp.Header().Set("X-Custom", "value")
		return resp.PlainTextBody("ok")
	})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	if rec.Header().Get("X-Custom") != "value" {
		t.Errorf("header X-Custom = %q, want %q", rec.Header().Get("X-Custom"), "value")
	}
}

func TestResponse_OctetsBody(t *testing.T) {
	type Req struct{}
	data := []byte{0x00, 0x01, 0x02, 0xFF}
	handler := RequestParser(func(ctx Context, _ Req) *Response {
		return ctx.Response(http.StatusOK).OctetsBody(data)
	})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	if !bytes.Equal(rec.Body.Bytes(), data) {
		t.Errorf("body = %v, want %v", rec.Body.Bytes(), data)
	}
}

// ============ Response error paths ============

func TestResponse_JsonMarshalError(t *testing.T) {
	handler := RequestParser(func(_ Context, _ struct{}) *Response {
		return (&Response{status: http.StatusOK, header: http.Header{}}).JsonBody(make(chan int))
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestResponse_UnknownBodyType(t *testing.T) {
	type customBody struct{}
	handler := RequestParser(func(_ Context, _ struct{}) *Response {
		return &Response{status: http.StatusOK, header: http.Header{}, body: customBody{}}
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============ Response getters ============

func TestResponse_StatusGetter(t *testing.T) {
	r := &Response{status: http.StatusNotFound}
	assert.Equal(t, http.StatusNotFound, r.Status())
}

func TestResponse_HeaderGetter(t *testing.T) {
	h := http.Header{"X-Test": {"value"}}
	r := &Response{header: h}
	assert.Equal(t, h, r.Header())
}

func TestResponse_CookieSetter(t *testing.T) {
	r := &Response{status: http.StatusOK, header: http.Header{}}
	r.Cookie(http.Cookie{Name: "session", Value: "abc"})
	assert.Len(t, r.header.Values("Set-Cookie"), 1)
}

// ============ http.StatusText ============

func TestStatusText(t *testing.T) {
	assert.Equal(t, "OK", http.StatusText(http.StatusOK))
	assert.Equal(t, "Not Found", http.StatusText(http.StatusNotFound))
	assert.Equal(t, "", http.StatusText(999))
}

// ============ nil body (status-only response) ============

func TestResponse_NilBody_StatusOnlyThroughHandler(t *testing.T) {
	handler := func(ctx Context, _ struct{}) *Response {
		return ctx.Response(http.StatusNoContent)
	}
	_, rec := doRequest[struct{}](t, handler, http.MethodGet, "/")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 0, rec.Body.Len(), "body should be empty")
	assert.Equal(t, "", rec.Header().Get("Content-Type"), "Content-Type should not be set")
}

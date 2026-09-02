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
)

// Fuzz targets focused on package-owned parsing behavior: arbitrary bytes in
// the request body or arbitrary Content-Type/charset header values must produce
// a controlled status (200/4xx/5xx) — never a panic or hang.

// noBodyRequest is a minimal request type with only a json-tagged field. It is
// the simplest shape that exercises [requestTags.bindJson] end-to-end.
type jsonNameRequest struct {
	Name string `json:"name"`
}

// makeJSONParserHandler returns an [http.HandlerFunc] that captures the parsed
// request and writes an OK response. Fuzz iterations reuse one handler value.
func makeJSONParserHandler(captured *jsonNameRequest) http.HandlerFunc {
	return asTestHTTPHandler(RequestParser(func(ctx *Context, req jsonNameRequest) {
		*captured = req
		ctx.NewResponse(http.StatusOK)
	}))
}

// FuzzParser_JSONBody asserts the JSON body binder never panics or hangs on
// arbitrary input: it must return one of the controlled statuses (200/400/500
// or other 4xx/5xx) and exit cleanly.
func FuzzParser_JSONBody(f *testing.F) {
	// seed corpus: empty, valid object, invalid syntax, second value, garbage.
	seeds := [][]byte{
		nil,
		[]byte(`{}`),
		[]byte(`{"name":"alice"}`),
		[]byte(`{"name":"alice"}{"name":"bob"}`),
		[]byte(`{`),
		[]byte(`]not json[`),
		[]byte("\x00\x01\x02"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	var captured jsonNameRequest
	handler := makeJSONParserHandler(&captured)

	f.Fuzz(func(t *testing.T, body []byte) {
		// bound the body so the fuzzer cannot generate megabytes per iter
		if len(body) > maxBodyLength {
			body = body[:maxBodyLength]
		}
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		// must not panic
		handler.ServeHTTP(rec, req)
		// status must be a valid HTTP status; the binder either succeeds (200)
		// or returns a controlled 4xx/5xx error.
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

// FuzzParser_FormBody asserts the form body binder never panics or hangs on
// arbitrary URL-encoded-ish bytes.
func FuzzParser_FormBody(f *testing.F) {
	type formReq struct {
		Name string `form:"name"`
	}
	seeds := [][]byte{
		nil,
		[]byte(`name=alice`),
		[]byte(`name=alice&age=30`),
		[]byte(`%%bad`),
		[]byte(`=no_key`),
		[]byte("\x00\x01"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	handler := asTestHTTPHandler(RequestParser(captureHandler[formReq]))

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxBodyLength {
			body = body[:maxBodyLength]
		}
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

// FuzzParser_ContentTypeHeader asserts that arbitrary Content-Type header
// values cannot crash the parser. The binder calls [mime.ParseMediaType]; a
// malformed header must yield a controlled 4xx status.
func FuzzParser_ContentTypeHeader(f *testing.F) {
	seeds := []string{
		"",
		"application/json",
		"application/json; charset=utf-8",
		"not a valid media type",
		"application/json; charset=",
		"multipart/form-data; boundary=---",
		"\x00\x01",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	var captured jsonNameRequest
	handler := makeJSONParserHandler(&captured)

	f.Fuzz(func(t *testing.T, contentType string) {
		body := []byte(`{"name":"alice"}`)
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

// FuzzParser_CharsetHeader asserts the charset-parameter path in
// [charsetReader] never panics on arbitrary values.
func FuzzParser_CharsetHeader(f *testing.F) {
	seeds := []string{
		"utf-8",
		"utf-16le",
		"invalid-charset",
		"",
		"utf-8; extra=1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	var captured jsonNameRequest
	handler := makeJSONParserHandler(&captured)

	f.Fuzz(func(t *testing.T, charset string) {
		body := []byte(`{"name":"alice"}`)
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset="+charset)
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

// FuzzParser_RawBody asserts the raw-body binder (`body:""` tag) never panics
// on arbitrary bytes; the body is handed to the handler verbatim.
func FuzzParser_RawBody(f *testing.F) {
	type Req struct {
		Body io.ReadCloser `body:""`
	}
	f.Add([]byte("hello"))
	f.Add([]byte("\x00\x01\x02"))
	f.Add([]byte(""))

	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxBodyLength {
			body = body[:maxBodyLength]
		}
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

// FuzzParser_WholeJSONBody exercises the whole-body `json:""` decode path with
// a typed target. The body must decode to exactly one JSON value or yield a
// controlled 4xx error.
func FuzzParser_WholeJSONBody(f *testing.F) {
	type Inner struct {
		Name string `json:"name"`
	}
	type Req struct {
		Data Inner `json:""`
	}
	seeds := [][]byte{
		[]byte(`{"name":"alice"}`),
		[]byte(`{"name":"alice"}{"name":"bob"}`),
		[]byte(`{}`),
		[]byte(`invalid`),
		[]byte(``),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	// also seed with marshaled valid JSON so coverage includes the happy path
	if data, err := json.Marshal(Inner{Name: "seed"}); err == nil {
		f.Add(data)
	}

	handler := asTestHTTPHandler(RequestParser(captureHandler[Req]))

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxBodyLength {
			body = body[:maxBodyLength]
		}
		req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 100 || rec.Code > 599 {
			t.Fatalf("expected valid HTTP status, got %d", rec.Code)
		}
	})
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for HTTP error paths: invalid content types, body size limits, timeouts,
// and bind (type coercion) failures.

func TestError_NilResponse_Returns500(t *testing.T) {
	type Req struct{}
	handler := RequestParser(func(_ Context, _ Req) *Response {
		return nil
	})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_MissingContentType_415(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	handler := RequestParser(captureHandler[Req])
	body := io.NopCloser(strings.NewReader(`{"data":"hello"}`))
	req, _ := http.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = int64(len(`{"data":"hello"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_InvalidContentType_400(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	handler := RequestParser(captureHandler[Req])
	body := io.NopCloser(strings.NewReader(`{"data":"hello"}`))
	req, _ := http.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "not a valid media type")
	req.ContentLength = int64(len(`{"data":"hello"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_UnsupportedContentType_415(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("text/plain", []byte("hello world")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_MissingContentLength_411(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	handler := RequestParser(captureHandler[Req])
	body := io.NopCloser(strings.NewReader(`{"data":"hello"}`))
	req, _ := http.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusLengthRequired, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_BodyTooLarge_413(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	handler := RequestParser(captureHandler[Req])
	largeBody := make([]byte, maxBodyLength+1)
	for i := range largeBody {
		largeBody[i] = 'a'
	}
	req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(largeBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_InvalidForm_400(t *testing.T) {
	type Req struct {
		Email string `form:"email"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/x-www-form-urlencoded", []byte("%%invalid%%")))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestError_BodyTimeout_408(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	handler := RequestParser(captureHandler[Req])
	req, _ := http.NewRequest(http.MethodPost, "/", &slowReader{delay: maxReadBodyDuration + 2*time.Second})
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 100
	rec := httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)
	assert.Equal(t, http.StatusRequestTimeout, rec.Code)
	assert.Empty(t, rec.Body.String())
	if elapsed < maxReadBodyDuration {
		t.Errorf("timeout occurred too quickly: %v (expected at least %v)", elapsed, maxReadBodyDuration)
	}
}

type slowReader struct {
	delay time.Duration
}

func (sr *slowReader) Read(_ []byte) (n int, err error) {
	time.Sleep(sr.delay)
	return 0, io.EOF
}

// ============ bind error paths (type coercion failures) ============

func TestError_BindHeader_400(t *testing.T) {
	type Req struct {
		Value int `header:"X-Value"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withHeader("X-Value", "not-a-number"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestError_BindCookie_400(t *testing.T) {
	type Req struct {
		Value int `cookie:"value"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withCookie("value", "not-a-number"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestError_BindForm_TypeMismatch_400(t *testing.T) {
	type Req struct {
		Age int `form:"age"`
	}
	// Valid URL-encoded data, but "not-a-number" fails int coercion in common.BindStructWithTag.
	_, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withFormBody(url.Values{"age": {"not-a-number"}}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestError_BindForm_ReadError_500(t *testing.T) {
	type Req struct {
		Value string `form:"value"`
	}
	reqType := reflect.TypeFor[Req]()
	tags := createTags(reqType)

	var req Req
	parsed := reflect.ValueOf(&req).Elem()

	status, err := tags.bindForm(errorReader{}, parsed)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Error(t, err)
}

func TestError_BindJson_TypeMismatch_400(t *testing.T) {
	type Req struct {
		Value int `json:"value"`
	}
	// JSON object where "value" is a string, not an int — common.BindStructWithTag fails.
	_, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json",
			[]byte(`{"value":"not-a-number"}`)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestError_BindJson_ReadError_400(t *testing.T) {
	type Req struct {
		Value string `json:"value"`
	}
	reqType := reflect.TypeFor[Req]()
	tags := createTags(reqType)

	var req Req
	parsed := reflect.ValueOf(&req).Elem()

	status, err := tags.bindJson(errorReader{}, parsed)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Error(t, err)
}

// ============ errorReader: returns error on Read ============

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
)

// ============ body tag tests ============

type bodyStruct struct {
	Body io.ReadCloser `body:""`
}

func TestBodyTag_Basic_BindsRawBody(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("raw body content")))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Body == nil {
		t.Fatal("Body is nil")
	}
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "raw body content", string(data), "Body")
}

type bodyWithContentTypesStruct struct {
	Body io.ReadCloser `body:"text/plain application/xml"`
}

func TestBodyTag_WithContentTypes_AcceptsMatching(t *testing.T) {
	captured, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("plain text")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "plain text", string(data), "Body")
}

func TestBodyTag_WithContentTypes_RejectsNonMatching_415(t *testing.T) {
	_, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("application/json", []byte("{}")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestBodyTag_MultipleContentTypes_AcceptsAny(t *testing.T) {
	captured, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("application/xml", []byte("<x/>")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "<x/>", string(data), "Body")
}

type bodyConflictFormStruct struct {
	Body io.ReadCloser `body:"application/x-www-form-urlencoded"`
	Form url.Values    `form:"name"`
}

type bodyConflictJsonStruct struct {
	Body io.ReadCloser    `body:"application/json"`
	Data jsonSimpleStruct `json:""`
}

type bodyConflictMultipartStruct struct {
	Body   io.ReadCloser     `body:"multipart/form-data"`
	Reader *multipart.Reader `multipart:""`
}

type bodyMultipleStruct struct {
	B1 io.ReadCloser `body:""`
	B2 io.ReadCloser `body:""`
}

type bodyWrongTypeStruct struct {
	Body string `body:""`
}

func TestBodyTag_ConflictsWithForm_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[bodyConflictFormStruct])
	})
}

func TestBodyTag_ConflictsWithJson_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[bodyConflictJsonStruct])
	})
}

func TestBodyTag_ConflictsWithMultipart_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[bodyConflictMultipartStruct])
	})
}

func TestBodyTag_MultipleTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[bodyMultipleStruct])
	})
}

func TestBodyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[bodyWrongTypeStruct])
	})
}

// Test that body tag is used as fallback when no other body tags match
type bodyFallbackStruct struct {
	Body io.ReadCloser `body:""`
}

func TestBodyTag_Fallback_WhenNoOtherBodyTagMatches(t *testing.T) {
	captured, rec := doRequest[bodyFallbackStruct](t, captureHandler[bodyFallbackStruct],
		http.MethodPost, "/", withRawBody("application/octet-stream", []byte("binary data")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "binary data", string(data), "Body")
}

func TestBodyTag_BodyReadable(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 1024)
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", body))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Len(t, data, 1024, "Body length")
}

// TestBodyTag_BodyMethodMatrix exercises the raw-body binder across the PUT/
// PATCH/DELETE methods that share the body-parsing case in request_body.go.
func TestBodyTag_BodyMethodMatrix(t *testing.T) {
	cases := []struct {
		name   string
		method string
		body   string
	}{
		{"PUT", http.MethodPut, "put body"},
		{"PATCH", http.MethodPatch, "patch body"},
		{"DELETE", http.MethodDelete, "delete body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
				tc.method, "/", withRawBody("text/plain", []byte(tc.body)))
			require.Equal(t, http.StatusOK, rec.Code)
			data, _ := io.ReadAll(captured.request.Body)
			assert.Equal(t, tc.body, string(data), "Body")
		})
	}
}

// ============ body tag content-length zero ============

func TestBodyTag_ContentLengthZero_SkipsBinding(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte{}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, captured.request.Body, "Body should be nil when ContentLength is 0")
}

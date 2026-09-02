/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============ multipart tag tests ============

type multipartStruct struct {
	Reader *multipart.Reader `multipart:""`
}

func TestMultipartTag_BasicReader(t *testing.T) {
	captured, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withMultipartBody(t, func(w *multipart.Writer) {
			_ = w.WriteField("field1", "value1")
			_ = w.WriteField("field2", "value2")
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Reader == nil {
		t.Fatal("Reader is nil")
	}
	part, err := captured.request.Reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart failed: %v", err)
	}
	assert.Equal(t, "field1", part.FormName(), "first part form name")
	value, _ := io.ReadAll(part)
	assert.Equal(t, "value1", string(value), "first part value")
}

func TestMultipartTag_MissingBoundary_400(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	req.ContentLength = int64(len("garbage"))
	rec := httptest.NewRecorder()
	asTestHTTPHandler(RequestParser(captureHandler[multipartStruct])).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMultipartTag_WrongContentType_415(t *testing.T) {
	_, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("data")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

type multipartNonEmptyStruct struct {
	Reader *multipart.Reader `multipart:"value"`
}

func TestMultipartTag_NonEmptyValue_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[multipartNonEmptyStruct])
	})
}

type multipartWrongTypeStruct struct {
	Reader string `multipart:""`
}

func TestMultipartTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[multipartWrongTypeStruct])
	})
}

type multipartMultipleStruct struct {
	R1 *multipart.Reader `multipart:""`
	R2 *multipart.Reader `multipart:""`
}

func TestMultipartTag_MultipleTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[multipartMultipleStruct])
	})
}

func TestMultipartTag_FileUpload(t *testing.T) {
	captured, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withMultipartBody(t, func(w *multipart.Writer) {
			writer, err := w.CreateFormFile("upload", "test.txt")
			if err != nil {
				t.Fatalf("CreateFormFile failed: %v", err)
			}
			_, _ = writer.Write([]byte("file content"))
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Reader == nil {
		t.Fatal("Reader is nil")
	}
	part, err := captured.request.Reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart failed: %v", err)
	}
	assert.Equal(t, "test.txt", part.FileName(), "file name")
	content, _ := io.ReadAll(part)
	assert.Equal(t, "file content", string(content), "content")
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============ Response.write (HTTP wire marshaling) ============

func TestResponse_Write_NilBody(t *testing.T) {
	rec := httptest.NewRecorder()
	r := &Response{status: http.StatusNoContent, header: rec.Header()}
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestResponse_Write_BytesBody(t *testing.T) {
	rec := httptest.NewRecorder()
	data := []byte("raw bytes")
	r := (&Response{status: http.StatusOK, header: rec.Header()}).BytesBody(data)
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestResponse_Write_StringBody(t *testing.T) {
	rec := httptest.NewRecorder()
	r := (&Response{status: http.StatusOK, header: rec.Header()}).StringBody("a string")
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "a string", rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestResponse_Write_StreamBody(t *testing.T) {
	rec := httptest.NewRecorder()
	r := (&Response{status: http.StatusOK, header: rec.Header()}).StreamBody(func(w io.Writer) error {
		_, err := w.Write([]byte("streamed"))
		return err
	})
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "streamed", rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestResponse_Write_StreamBody_Error(t *testing.T) {
	rec := httptest.NewRecorder()
	streamErr := errors.New("stream write failed")
	r := (&Response{status: http.StatusOK, header: rec.Header()}).StreamBody(func(w io.Writer) error {
		return streamErr
	})
	err := r.write(rec)
	assert.ErrorIs(t, err, streamErr)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestResponse_Write_PlainTextBody(t *testing.T) {
	rec := httptest.NewRecorder()
	r := (&Response{status: http.StatusOK, header: rec.Header()}).PlainTextBody("hello plain")
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello plain", rec.Body.String())
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
}

func TestResponse_Write_OctetsBody(t *testing.T) {
	rec := httptest.NewRecorder()
	data := []byte{0x00, 0x01, 0x02, 0xFF}
	r := (&Response{status: http.StatusOK, header: rec.Header()}).OctetsBody(data)
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
}

func TestResponse_Write_JsonBody(t *testing.T) {
	rec := httptest.NewRecorder()
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := payload{Name: "alice", Age: 30}
	r := (&Response{status: http.StatusCreated, header: rec.Header()}).JsonBody(p)
	err := r.write(rec)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	var result payload
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, p, result)
}

func TestResponse_Write_JsonBody_MarshalError(t *testing.T) {
	rec := httptest.NewRecorder()
	r := (&Response{status: http.StatusOK, header: rec.Header()}).JsonBody(make(chan int))
	err := r.write(rec)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestResponse_Write_UnknownBodyType(t *testing.T) {
	rec := httptest.NewRecorder()
	r := &Response{status: http.StatusOK, header: rec.Header(), body: 12345}
	err := r.write(rec)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

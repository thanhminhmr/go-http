/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============ requestLogger middleware ============

func testLoggerHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("hello"))
}

func TestRequestLogger_BasicRequest(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(testLoggerHandler))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}

func TestRequestLogger_PanicRecovery(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRequestLogger_StatusPreserved(t *testing.T) {
	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "bad request", rec.Body.String())
}

// ============ funcObject / funcObjects ============

func TestFuncObject_KnownHandler(t *testing.T) {
	frame := funcObject(http.HandlerFunc(testLoggerHandler))
	assert.NotEqual(t, "<unknown>", frame.Function)
	assert.NotEmpty(t, frame.File)
	assert.Greater(t, frame.Line, 0)
}

func TestFuncObject_UnknownValue(t *testing.T) {
	frame := funcObject(42)
	assert.Equal(t, "<unknown>", frame.Function)
}

func TestFuncObject_NilValue(t *testing.T) {
	frame := funcObject(nil)
	assert.Equal(t, "<unknown>", frame.Function)
}

func TestFuncObjects_Empty(t *testing.T) {
	frames := funcObjects([]http.Handler{})
	assert.Empty(t, frames)
}

func TestFuncObjects_NonEmpty(t *testing.T) {
	h := http.HandlerFunc(testLoggerHandler)
	frames := funcObjects([]http.Handler{h, h})
	assert.Len(t, frames, 2)
	assert.NotEqual(t, "<unknown>", frames[0].Function)
	assert.NotEqual(t, "<unknown>", frames[1].Function)
}

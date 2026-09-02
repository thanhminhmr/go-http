/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============ funcObject / funcObjects ============

func TestFuncObject_KnownHandler(t *testing.T) {
	frame := funcObject(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
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
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})
	frames := funcObjects([]http.Handler{h, h})
	assert.Len(t, frames, 2)
	assert.NotEqual(t, "<unknown>", frames[0].Function)
	assert.NotEqual(t, "<unknown>", frames[1].Function)
}

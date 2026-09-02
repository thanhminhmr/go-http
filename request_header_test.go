/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

// ============ header tag tests ============

type headerSingleStruct struct {
	UserID string `header:"X-User-Id"`
}

func TestHeaderTag_SingleField(t *testing.T) {
	captured, rec := doRequest[headerSingleStruct](t, captureHandler[headerSingleStruct],
		http.MethodGet, "/", withHeader("X-User-Id", "user123"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "user123", captured.request.UserID, "UserID")
}

func TestHeaderTag_MissingHeader_ZeroValue(t *testing.T) {
	captured, rec := doRequest[headerSingleStruct](t, captureHandler[headerSingleStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.UserID, "UserID")
}

type headerTypeCoercionStruct struct {
	Count    int  `header:"X-Count"`
	Enabled  bool `header:"X-Enabled"`
	Verified bool `header:"X-Verified"`
}

func TestHeaderTag_TypeCoercion(t *testing.T) {
	captured, rec := doRequest[headerTypeCoercionStruct](t, captureHandler[headerTypeCoercionStruct],
		http.MethodGet, "/",
		withHeader("X-Count", "42"),
		withHeader("X-Enabled", "true"),
		withHeader("X-Verified", "1"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 42, captured.request.Count, "Count")
	assert.Equal(t, true, captured.request.Enabled, "Enabled")
	assert.Equal(t, true, captured.request.Verified, "Verified")
}

type headerAllStruct struct {
	Headers http.Header `header:""`
}

func TestHeaderTag_EmptyTag_BindsAllHeaders(t *testing.T) {
	captured, rec := doRequest[headerAllStruct](t, captureHandler[headerAllStruct],
		http.MethodGet, "/",
		withHeader("X-Custom-1", "value1"),
		withHeader("X-Custom-2", "value2"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Headers == nil {
		t.Fatal("Headers field is nil")
	}
	assert.Equal(t, "value1", captured.request.Headers.Get("X-Custom-1"), "Headers[X-Custom-1]")
	assert.Equal(t, "value2", captured.request.Headers.Get("X-Custom-2"), "Headers[X-Custom-2]")
}

type headerAllWrongTypeStruct struct {
	Headers string `header:""`
}

func TestHeaderTag_EmptyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[headerAllWrongTypeStruct])
	})
}

type headerMultipleEmptyStruct struct {
	H1 http.Header `header:""`
	H2 http.Header `header:""`
}

func TestHeaderTag_EmptyTag_MultipleTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[headerMultipleEmptyStruct])
	})
}

type headerNonEmptyAfterEmptyStruct struct {
	All    http.Header `header:""`
	Single string      `header:"X-Custom"`
}

func TestHeaderTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[headerNonEmptyAfterEmptyStruct])
	})
}

type headerMultiValueStruct struct {
	Values []string `header:"X-Multi"`
}

func TestHeaderTag_MultipleValues_BindsAllValues(t *testing.T) {
	captured, rec := doRequest[headerMultiValueStruct](t, captureHandler[headerMultiValueStruct],
		http.MethodGet, "/",
		func(r *http.Request) {
			r.Header.Add("X-Multi", "val1")
			r.Header.Add("X-Multi", "val2")
			r.Header.Add("X-Multi", "val3")
		},
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"val1", "val2", "val3"}, captured.request.Values, "Values")
}

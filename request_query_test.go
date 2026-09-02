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
	"github.com/stretchr/testify/require"
)

// ============ query tag tests ============

type querySingleStruct struct {
	Page string `query:"page"`
}

func TestQueryTag_SingleField(t *testing.T) {
	captured, rec := doRequest[querySingleStruct](t, captureHandler[querySingleStruct],
		http.MethodGet, "/", withQuery("page=5"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "5", captured.request.Page, "Page")
}

func TestQueryTag_MissingParam_ZeroValue(t *testing.T) {
	captured, rec := doRequest[querySingleStruct](t, captureHandler[querySingleStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Page, "Page")
}

type queryTypeCoercionStruct struct {
	Count   int     `query:"count"`
	Enabled bool    `query:"enabled"`
	Limit   uint    `query:"limit"`
	Ratio   float64 `query:"ratio"`
}

func TestQueryTag_TypeCoercion(t *testing.T) {
	captured, rec := doRequest[queryTypeCoercionStruct](t, captureHandler[queryTypeCoercionStruct],
		http.MethodGet, "/", withQuery("count=42&enabled=true&limit=100&ratio=3.14"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 42, captured.request.Count, "Count")
	assert.Equal(t, true, captured.request.Enabled, "Enabled")
	assert.Equal(t, uint(100), captured.request.Limit, "Limit")
	assert.Equal(t, 3.14, captured.request.Ratio, "Ratio")
}

type queryMultipleValuesStruct struct {
	IDs []string `query:"id"`
}

func TestQueryTag_MultipleValues_SliceField(t *testing.T) {
	captured, rec := doRequest[queryMultipleValuesStruct](t, captureHandler[queryMultipleValuesStruct],
		http.MethodGet, "/", withQuery("id=1&id=2&id=3"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"1", "2", "3"}, captured.request.IDs, "IDs")
}

type queryUnboxStruct struct {
	ID int `query:"id"`
}

func TestQueryTag_SingleElement_UnboxHook(t *testing.T) {
	captured, rec := doRequest[queryUnboxStruct](t, captureHandler[queryUnboxStruct],
		http.MethodGet, "/", withQuery("id=42"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 42, captured.request.ID, "ID (unboxed from single-element slice)")
}

type queryAllStruct struct {
	Params KeyValues `query:""`
}

func TestQueryTag_EmptyTag_BindsAllParams(t *testing.T) {
	captured, rec := doRequest[queryAllStruct](t, captureHandler[queryAllStruct],
		http.MethodGet, "/", withQuery("a=1&b=2&c=3"))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Params == nil {
		t.Fatal("Params field is nil")
	}
	assert.Equal(t, []string{"1"}, captured.request.Params["a"], "Params[a]")
	assert.Equal(t, []string{"2"}, captured.request.Params["b"], "Params[b]")
}

type queryAllWrongTypeStruct struct {
	Params string `query:""`
}

func TestQueryTag_EmptyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[queryAllWrongTypeStruct])
	})
}

type queryMultipleEmptyStruct struct {
	Q1 KeyValues `query:""`
	Q2 KeyValues `query:""`
}

func TestQueryTag_MultipleEmptyTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[queryMultipleEmptyStruct])
	})
}

type queryNonEmptyAfterEmptyStruct struct {
	All    KeyValues `query:""`
	Single string    `query:"name"`
}

func TestQueryTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[queryNonEmptyAfterEmptyStruct])
	})
}

type queryIntSliceStruct struct {
	IDs []int `query:"id"`
}

func TestQueryTag_IntSlice_MultipleValues(t *testing.T) {
	captured, rec := doRequest[queryIntSliceStruct](t, captureHandler[queryIntSliceStruct],
		http.MethodGet, "/", withQuery("id=1&id=2&id=3"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int{1, 2, 3}, captured.request.IDs, "IDs")
}

// --- single-value slices (unbox hook path) ---

func TestQueryTag_SingleValue_StringSlice(t *testing.T) {
	type Req struct {
		Tags []string `query:"tag"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("tag=go"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"go"}, captured.request.Tags, "Tags")
}

func TestQueryTag_SingleValue_IntSlice(t *testing.T) {
	type Req struct {
		IDs []int `query:"id"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("id=42"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int{42}, captured.request.IDs, "IDs")
}

func TestQueryTag_SpecialCharacters_URLDecoded(t *testing.T) {
	captured, rec := doRequest[querySingleStruct](t, captureHandler[querySingleStruct],
		http.MethodGet, "/", withQuery("page=hello+world"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello world", captured.request.Page, "Page")
}

// url.URL type doesn't implement TextUnmarshaler, so it doesn't work with any tag.
// Use string field instead, or a custom type implementing encoding.TextUnmarshaler.
// See request_conversion_test.go for TextUnmarshaler type tests.
type queryURLStringStruct struct {
	Target string `query:"target"`
}

func TestQueryTag_URLAsString(t *testing.T) {
	captured, rec := doRequest[queryURLStringStruct](t, captureHandler[queryURLStringStruct],
		http.MethodGet, "/", withQuery("target=https://example.com"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com", captured.request.Target, "Target")
}

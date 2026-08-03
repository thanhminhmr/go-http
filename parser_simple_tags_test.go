/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the simple request tags: default, header, cookie, query, and url.
// Body-reading tags (form, json, multipart, body) are in parser_body_tags_test.go.

// ============ default tag tests ============

type defaultBasicStruct struct {
	Name string `default:"alice"`
	Age  int    `default:"42"`
}

func TestDefaultTag_Basic(t *testing.T) {
	captured, rec := doRequest[defaultBasicStruct](t, captureHandler[defaultBasicStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Name, "Name")
	assert.Equal(t, 42, captured.request.Age, "Age")
}

type defaultMultipleStruct struct {
	A string `default:"foo"`
	B int    `default:"100"`
	C bool   `default:"true"`
}

func TestDefaultTag_MultipleFields(t *testing.T) {
	captured, rec := doRequest[defaultMultipleStruct](t, captureHandler[defaultMultipleStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "foo", captured.request.A, "A")
	assert.Equal(t, 100, captured.request.B, "B")
	assert.Equal(t, true, captured.request.C, "C")
}

type defaultOverriddenStruct struct {
	Name string `default:"alice" query:"name"`
	Age  int    `default:"42" query:"age"`
}

func TestDefaultTag_OverriddenByRequest(t *testing.T) {
	captured, rec := doRequest[defaultOverriddenStruct](t, captureHandler[defaultOverriddenStruct],
		http.MethodGet, "/?name=bob&age=99")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "bob", captured.request.Name, "Name")
	assert.Equal(t, 99, captured.request.Age, "Age")
}

func TestDefaultTag_RequestOverridesPartially(t *testing.T) {
	captured, rec := doRequest[defaultOverriddenStruct](t, captureHandler[defaultOverriddenStruct],
		http.MethodGet, "/?name=bob")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "bob", captured.request.Name, "Name")
	assert.Equal(t, 42, captured.request.Age, "Age (default)")
}

type defaultInvalidStruct struct {
	Age int `default:"not-a-number"`
}

func TestDefaultTag_InvalidValue_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidStruct])
	})
}

// ============ default tag: string-specific tests ============

type defaultStringMultiWordStruct struct {
	Value string `default:"hello world"`
}

func TestDefaultTag_StringWithSpaces(t *testing.T) {
	captured, rec := doRequest[defaultStringMultiWordStruct](t, captureHandler[defaultStringMultiWordStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello world", captured.request.Value)
}

type defaultStringWithSpecialCharsStruct struct {
	Value string `default:"a=b&c d:e[f]"`
}

func TestDefaultTag_StringWithSpecialChars(t *testing.T) {
	captured, rec := doRequest[defaultStringWithSpecialCharsStruct](t, captureHandler[defaultStringWithSpecialCharsStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "a=b&c d:e[f]", captured.request.Value)
}

type defaultEmptyStringStruct struct {
	Value string `default:""`
}

func TestDefaultTag_EmptyString(t *testing.T) {
	captured, rec := doRequest[defaultEmptyStringStruct](t, captureHandler[defaultEmptyStringStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Value)
}

// ============ default tag: numeric edge cases ============

type defaultNumericEdgeStruct struct {
	NegInt   int     `default:"-42"`
	NegFloat float64 `default:"-3.14"`
	BigInt   int64   `default:"9223372036854775807"`
	PosUint  uint    `default:"42"`
}

func TestDefaultTag_NumericEdgeCases(t *testing.T) {
	captured, rec := doRequest[defaultNumericEdgeStruct](t, captureHandler[defaultNumericEdgeStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, -42, captured.request.NegInt)
	assert.Equal(t, -3.14, captured.request.NegFloat)
	assert.Equal(t, int64(9223372036854775807), captured.request.BigInt)
	assert.Equal(t, uint(42), captured.request.PosUint)
}

type defaultBoolLikeValues struct {
	A bool `default:"true"`
	B bool `default:"false"`
	C bool `default:"1"`
	D bool `default:"0"`
}

func TestDefaultTag_BoolLikeValues(t *testing.T) {
	captured, rec := doRequest[defaultBoolLikeValues](t, captureHandler[defaultBoolLikeValues], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, true, captured.request.A)
	assert.Equal(t, false, captured.request.B)
	assert.Equal(t, true, captured.request.C)
	assert.Equal(t, false, captured.request.D)
}

type defaultInvalidIntFormat struct {
	Age int `default:"123abc"`
}

func TestDefaultTag_InvalidIntFormat_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidIntFormat])
	})
}

type defaultInvalidBoolFormat struct {
	Flag bool `default:"yes"`
}

func TestDefaultTag_InvalidBoolFormat_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidBoolFormat])
	})
}

type defaultInvalidUintStruct struct {
	V uint `default:"not-a-number"`
}

func TestDefaultTag_InvalidUint_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidUintStruct])
	})
}

type defaultInvalidFloatStruct struct {
	V float64 `default:"not-a-number"`
}

func TestDefaultTag_InvalidFloat_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidFloatStruct])
	})
}

type defaultInvalidComplexStruct struct {
	C complex64 `default:"not-a-complex"`
}

func TestDefaultTag_InvalidComplex_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultInvalidComplexStruct])
	})
}

// ============ default tag: TextUnmarshaler ============

type defaultTextUnmarshalerStruct struct {
	Value textUnmarshalerType `default:"hello"`
}

type textUnmarshalerType string

func (t *textUnmarshalerType) UnmarshalText(text []byte) error {
	*t = textUnmarshalerType("unmarshaled:" + string(text))
	return nil
}

func TestDefaultTag_TextUnmarshaler(t *testing.T) {
	captured, rec := doRequest[defaultTextUnmarshalerStruct](t, captureHandler[defaultTextUnmarshalerStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, textUnmarshalerType("unmarshaled:hello"), captured.request.Value)
}

type defaultTextUnmarshalerStructEmptyDefault struct {
	Value textUnmarshalerType `default:""`
}

func TestDefaultTag_TextUnmarshaler_EmptyString(t *testing.T) {
	captured, rec := doRequest[defaultTextUnmarshalerStructEmptyDefault](t, captureHandler[defaultTextUnmarshalerStructEmptyDefault], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, textUnmarshalerType("unmarshaled:"), captured.request.Value)
}

// ============ default tag: MapstructureUnmarshaler ============

type mapstructureUnmarshalerType struct {
	Value string
}

func (t *mapstructureUnmarshalerType) UnmarshalMapstructure(value any) error {
	if s, ok := value.(string); ok {
		t.Value = "mapstruct:" + s
		return nil
	}
	return fmt.Errorf("expected string, got %T", value)
}

type defaultMapstructureUnmarshalerStruct struct {
	Value mapstructureUnmarshalerType `default:"world"`
}

func TestDefaultTag_MapstructureUnmarshaler(t *testing.T) {
	captured, rec := doRequest[defaultMapstructureUnmarshalerStruct](t, captureHandler[defaultMapstructureUnmarshalerStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "mapstruct:world", captured.request.Value.Value)
}

// ============ default tag: unknown type panics ============

type defaultUnknownTypeStruct struct {
	Value struct{ Name string } `default:"anything"`
}

func TestDefaultTag_UnknownType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[defaultUnknownTypeStruct])
	})
}

// ============ default tag: complex numbers ============

type defaultComplexStruct struct {
	C64  complex64  `default:"1+2i"`
	C128 complex128 `default:"3+4i"`
}

func TestDefaultTag_ComplexNumbers(t *testing.T) {
	captured, rec := doRequest[defaultComplexStruct](t, captureHandler[defaultComplexStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, complex64(1+2i), captured.request.C64, "C64")
	assert.Equal(t, 3+4i, captured.request.C128, "C128")
}

// ============ default tag: no aliasing between requests ============

type sliceDefaultType []string

func (s *sliceDefaultType) UnmarshalText(text []byte) error {
	*s = strings.Split(string(text), ",")
	return nil
}

type defaultSliceStruct struct {
	Items sliceDefaultType `default:"a,b,c"`
}

func TestDefaultTag_NoAliasing_BetweenRequests(t *testing.T) {
	var secondRequestItems sliceDefaultType
	callCount := 0
	handler := RequestParser(func(ctx Context, req defaultSliceStruct) *Response {
		callCount++
		if callCount == 1 {
			req.Items[0] = "MUTATED"
		} else {
			secondRequestItems = req.Items
		}
		return ctx.Response(http.StatusOK)
	})

	req1, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	assert.Equal(t, 2, callCount)
	assert.Equal(t, sliceDefaultType{"a", "b", "c"}, secondRequestItems,
		"second request should see fresh default, not mutated value (no aliasing)")
}

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

// ============ cookie tag tests ============

type cookieSingleStruct struct {
	Session string `cookie:"session"`
}

func TestCookieTag_SingleField(t *testing.T) {
	captured, rec := doRequest[cookieSingleStruct](t, captureHandler[cookieSingleStruct],
		http.MethodGet, "/", withCookie("session", "abc123"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "abc123", captured.request.Session, "Session")
}

func TestCookieTag_MissingCookie_ZeroValue(t *testing.T) {
	captured, rec := doRequest[cookieSingleStruct](t, captureHandler[cookieSingleStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Session, "Session")
}

type cookieAllStruct struct {
	Cookies KeyValues `cookie:""`
}

func TestCookieTag_EmptyTag_BindsAllCookies(t *testing.T) {
	captured, rec := doRequest[cookieAllStruct](t, captureHandler[cookieAllStruct],
		http.MethodGet, "/",
		withCookie("session", "abc"),
		withCookie("csrf", "xyz"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Cookies == nil {
		t.Fatal("Cookies field is nil")
	}
	assert.Equal(t, []string{"abc"}, captured.request.Cookies["session"], "Cookies[session]")
	assert.Equal(t, []string{"xyz"}, captured.request.Cookies["csrf"], "Cookies[csrf]")
}

type cookieAllWrongTypeStruct struct {
	Cookies string `cookie:""`
}

func TestCookieTag_EmptyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[cookieAllWrongTypeStruct])
	})
}

type cookieMultipleEmptyStruct struct {
	C1 KeyValues `cookie:""`
	C2 KeyValues `cookie:""`
}

func TestCookieTag_MultipleEmptyTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[cookieMultipleEmptyStruct])
	})
}

type cookieNonEmptyAfterEmptyStruct struct {
	All    KeyValues `cookie:""`
	Single string    `cookie:"session"`
}

func TestCookieTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[cookieNonEmptyAfterEmptyStruct])
	})
}

func TestCookieTag_MultipleCookiesSameName_BindsAllValues(t *testing.T) {
	type Req struct {
		Values []string `cookie:"pref"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/",
		withCookie("pref", "dark"),
		withCookie("pref", "compact"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"dark", "compact"}, captured.request.Values, "Values")
}

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

// ============ url tag tests ============

type urlSingleStruct struct {
	ID string `url:"id"`
}

func TestUrlTag_SingleField_ChiRouteParam(t *testing.T) {
	captured, rec := doChiRequest[urlSingleStruct](t,
		http.MethodGet, "/users/{id}", "/users/42",
		captureHandler[urlSingleStruct])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "42", captured.request.ID, "ID")
}

type urlMultipleStruct struct {
	UserID string `url:"userId"`
	PostID string `url:"postId"`
}

func TestUrlTag_MultipleRouteParams(t *testing.T) {
	captured, rec := doChiRequest[urlMultipleStruct](t,
		http.MethodGet, "/users/{userId}/posts/{postId}", "/users/u123/posts/p456",
		captureHandler[urlMultipleStruct])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "u123", captured.request.UserID, "UserID")
	assert.Equal(t, "p456", captured.request.PostID, "PostID")
}

type urlTypeCoercionStruct struct {
	ID int `url:"id"`
}

func TestUrlTag_TypeCoercion(t *testing.T) {
	captured, rec := doChiRequest[urlTypeCoercionStruct](t,
		http.MethodGet, "/items/{id}", "/items/99",
		captureHandler[urlTypeCoercionStruct])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 99, captured.request.ID, "ID")
}

type urlAllStruct struct {
	Params KeyValue `url:""`
}

func TestUrlTag_EmptyTag_BindsAllParams(t *testing.T) {
	captured, rec := doChiRequest[urlAllStruct](t,
		http.MethodGet, "/users/{userId}/posts/{postId}", "/users/u123/posts/p456",
		captureHandler[urlAllStruct])
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Params == nil {
		t.Fatal("Params field is nil")
	}
	assert.Equal(t, "u123", captured.request.Params["userId"], "Params[userId]")
	assert.Equal(t, "p456", captured.request.Params["postId"], "Params[postId]")
}

type urlAllWrongTypeStruct struct {
	Params string `url:""`
}

func TestUrlTag_EmptyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[urlAllWrongTypeStruct])
	})
}

type urlMultipleEmptyStruct struct {
	U1 KeyValue `url:""`
	U2 KeyValue `url:""`
}

func TestUrlTag_MultipleEmptyTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[urlMultipleEmptyStruct])
	})
}

type urlNonEmptyAfterEmptyStruct struct {
	All    KeyValue `url:""`
	Single string   `url:"id"`
}

func TestUrlTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[urlNonEmptyAfterEmptyStruct])
	})
}

func TestUrlTag_NoRouteContext_ZeroValue(t *testing.T) {
	captured, rec := doRequest[urlSingleStruct](t, captureHandler[urlSingleStruct], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.ID, "ID (no route context)")
}

// url.URL type doesn't implement TextUnmarshaler, so it doesn't work with any tag.
// Use string field instead, or a custom type implementing encoding.TextUnmarshaler.
// See parser_hooks_test.go for TextUnmarshaler type tests.
type queryURLStringStruct struct {
	Target string `query:"target"`
}

func TestQueryTag_URLAsString(t *testing.T) {
	captured, rec := doRequest[queryURLStringStruct](t, captureHandler[queryURLStringStruct],
		http.MethodGet, "/", withQuery("target=https://example.com"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com", captured.request.Target, "Target")
}

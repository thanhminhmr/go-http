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
	handler := asTestHTTPHandler(RequestParser(func(ctx *Context, req defaultSliceStruct) {
		callCount++
		if callCount == 1 {
			req.Items[0] = "MUTATED"
		} else {
			secondRequestItems = req.Items
		}
		ctx.NewResponse(http.StatusOK)
	}))

	req1, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	req2, _ := http.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	assert.Equal(t, 2, callCount)
	assert.Equal(t, sliceDefaultType{"a", "b", "c"}, secondRequestItems,
		"second request should see fresh default, not mutated value (no aliasing)")
}

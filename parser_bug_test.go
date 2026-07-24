/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// This file contains regression tests for previously fixed bugs:
//   - Bug 1: json.Number + mapstructure panic (FIXED)
//   - Bug 3: net.IP slice-kind TextUnmarshaler (FIXED)
//   - Single-value slice regressions
//
// All bugs are FIXED. These tests ensure they stay fixed.

// ============ Bug 1 (FIXED): json.Number + mapstructure panic ============
//
// When using a non-empty json:"fieldname" tag with a numeric field (int, float,
// etc.), the JSON body is decoded into map[string]any using decoder.UseNumber()
// (parser.go), which produces json.Number values for JSON numbers. These values
// are then passed to mapstructure's bind(), which calls internalDecodeHookFunc.
//
// Previously, the hook did not handle json.Number, causing a panic:
//
//	panic: interface conversion: interface {} is json.Number, not string
//
// The fix: internalDecodeHookFunc now handles json.Number alongside string
// (parser.go, typeForJsonNumber case). The binder goroutine also has a
// defer recover() so that even if a panic occurs, it is caught and returned
// as a 500 error instead of crashing the server.
//
// The tests below call bind() directly to verify the hook handles json.Number
// correctly for int, float, int64, and string fields.

func TestBug_JSONNumber_IntField(t *testing.T) {
	type Req struct {
		Value int `json:"value"`
	}
	req := &Req{}
	parsed := reflect.ValueOf(req).Elem()
	values := map[string]any{"value": json.Number("12345")}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("bind should not panic: %v", r)
		}
	}()
	err := bind("json", values, parsed)
	if err != nil {
		t.Fatalf("bind should succeed: %v", err)
	}
	if req.Value != 12345 {
		t.Errorf("Value = %d, want 12345", req.Value)
	}
}

func TestBug_JSONNumber_FloatField(t *testing.T) {
	type Req struct {
		Value float64 `json:"value"`
	}
	req := &Req{}
	parsed := reflect.ValueOf(req).Elem()
	values := map[string]any{"value": json.Number("1.23")}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("bind should not panic: %v", r)
		}
	}()
	err := bind("json", values, parsed)
	if err != nil {
		t.Fatalf("bind should succeed: %v", err)
	}
	if req.Value != 1.23 {
		t.Errorf("Value = %v, want 1.23", req.Value)
	}
}

func TestBug_JSONNumber_Int64Field(t *testing.T) {
	type Req struct {
		Value int64 `json:"value"`
	}
	req := &Req{}
	parsed := reflect.ValueOf(req).Elem()
	values := map[string]any{"value": json.Number("9999999999")}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("bind should not panic: %v", r)
		}
	}()
	err := bind("json", values, parsed)
	if err != nil {
		t.Fatalf("bind should succeed: %v", err)
	}
	if req.Value != 9999999999 {
		t.Errorf("Value = %d, want 9999999999", req.Value)
	}
}

// String fields work fine with non-empty json tags because mapstructure can
// assign string → string without calling hooks.
func TestBug_JSONNumber_StringField(t *testing.T) {
	type Req struct {
		Value string `json:"value"`
	}
	req := &Req{}
	parsed := reflect.ValueOf(req).Elem()
	values := map[string]any{"value": "hello"}
	err := bind("json", values, parsed)
	if err != nil {
		t.Fatalf("bind should succeed: %v", err)
	}
	if req.Value != "hello" {
		t.Errorf("Value = %q, want %q", req.Value, "hello")
	}
}

// ============ Probe: unbox + jsonNumberToString interaction ============
//
// When the JSON body is {"x":[123]}, UseNumber() produces
// map[string]any{"x": []any{json.Number("123")}}. The hook chain runs ONCE
// on the slice value:
//   - jsonNumberToString: source is []any (not json.Number), skips
//   - StringToBasicType*: source kind is Slice, skips
//   - unboxIfElementSliceHasSingleElement: unboxes to json.Number("123")
// The unboxed json.Number is the FINAL return value — it does NOT re-run
// through the earlier hooks. So mapstructure must assign json.Number to int.

func TestProbe_SingleElementIntArray_ToInt(t *testing.T) {
	type Req struct {
		X int `json:"x"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"x":[123]}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 123, captured.request.X, "X")
}

// ============ Bug 3 (FIXED): slice-kind TextUnmarshaler types with slice-source tags ============
//
// net.IP is `type IP []byte` — Kind == Slice, but it implements TextUnmarshaler
// (via pointer receiver `func (ip *IP) UnmarshalText`).
//
// Previously, the pure-slice detection in internalDecodeHookFunc used
// `target.Kind() == reflect.Slice` which incorrectly caught net.IP, preventing
// the unbox logic from firing when the source was a single-element []string.
// This caused net.IP to fail with query/header/cookie/form tags.
//
// The fix: pure-slice detection now checks `target.NumMethod() == 0` to
// distinguish plain slices (like []byte, []string) from named slice types
// (like net.IP) that have methods. This allows net.IP to be unboxed from a
// single-element []string and then handled by TextUnmarshaler.
//
// These regression tests verify net.IP works with all tag types.

func TestBug_NetIP_QueryTag(t *testing.T) {
	type Req struct {
		IP net.IP `query:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ip=192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestBug_NetIP_HeaderTag(t *testing.T) {
	type Req struct {
		IP net.IP `header:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withHeader("ip", "192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestBug_NetIP_CookieTag(t *testing.T) {
	type Req struct {
		IP net.IP `cookie:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withCookie("ip", "192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestBug_NetIP_FormTag(t *testing.T) {
	type Req struct {
		IP net.IP `form:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withFormBody(url.Values{"ip": {"192.168.1.1"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

// Invalid IP should return 400 — TextUnmarshaler rejects the invalid input.
func TestBug_NetIP_InvalidQuery(t *testing.T) {
	type Req struct {
		IP net.IP `query:"ip"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ip=not-an-ip"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- *net.IP (pointer): unbox fires (Ptr != Slice), works as a contrast ---

func TestBug_NetIPPointer_QueryTag(t *testing.T) {
	type Req struct {
		IP *net.IP `query:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ip=192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	if assert.NotNil(t, captured.request.IP, "IP") {
		assert.Equal(t, net.IPv4(192, 168, 1, 1), *captured.request.IP, "IP")
	}
}

// ============ Regression: single-value slices ============
//
// After the net.IP fix (pure-slice detection via NumMethod), verify that
// single-value []string and []int still work correctly with query tags.

func TestRegression_SingleValueStringSlice_QueryTag(t *testing.T) {
	type Req struct {
		Tags []string `query:"tag"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("tag=go"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"go"}, captured.request.Tags, "Tags")
}

func TestRegression_SingleValueIntSlice_QueryTag(t *testing.T) {
	type Req struct {
		IDs []int `query:"id"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("id=42"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int{42}, captured.request.IDs, "IDs")
}

// ============ Regression: json.Number through the full HTTP handler ============
//
// The direct bind() tests above verify the hook handles json.Number correctly.
// This test verifies the same path through the full HTTP handler (bindJson →
// bind → internalDecodeHookFunc), ensuring end-to-end correctness.

func TestBug_JSONNumber_IntField_HTTPHandler(t *testing.T) {
	type Req struct {
		Count int `json:"count"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json",
			[]byte(`{"count":42}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 42, captured.request.Count, "Count")
}

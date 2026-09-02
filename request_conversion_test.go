/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test the decode hook behavior for TextUnmarshaler types and type conversions.
//
// Binding goes through common.BindStructWithTag, whose decode hook handles:
//  1. Same-type: direct Set
//  2. Pure-slice detection (NumMethod == 0): short-circuit, wrap, or unbox
//  3. mapstructure.Unmarshaler: delegate to target
//  4. String/json.Number → int/uint/float/bool/complex via strconv
//  5. String/json.Number → encoding.TextUnmarshaler (time.Time, net.IP, netip.*, custom)

// ============ TextUnmarshaler types with query tag ============

func TestHook_Time_QueryTag(t *testing.T) {
	type Req struct {
		T time.Time `query:"t"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("t=2024-01-15T10:30:00Z"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")
	assert.Equal(t, expected, captured.request.T, "T")
}

func TestHook_Time_QueryTag_Invalid(t *testing.T) {
	type Req struct {
		T time.Time `query:"t"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("t=not-a-time"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHook_Time_RFC3339Nano(t *testing.T) {
	type Req struct {
		T time.Time `query:"t"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("t=2024-01-15T10:30:00.123456789Z"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected, _ := time.Parse(time.RFC3339Nano, "2024-01-15T10:30:00.123456789Z")
	assert.Equal(t, expected, captured.request.T, "T")
}

func TestHook_Time_JsonTag(t *testing.T) {
	type Req struct {
		T time.Time `json:"t"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"t":"2024-01-15T10:30:00Z"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")
	assert.Equal(t, expected, captured.request.T, "T")
}

func TestHook_Time_EmptyJsonTag(t *testing.T) {
	type Req struct {
		T time.Time `json:""`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`"2024-01-15T10:30:00Z"`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")
	assert.Equal(t, expected, captured.request.T, "T")
}

func TestHook_TimePointer_QueryTag(t *testing.T) {
	type Req struct {
		T *time.Time `query:"t"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("t=2024-01-15T10:30:00Z"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected, _ := time.Parse(time.RFC3339, "2024-01-15T10:30:00Z")
	if assert.NotNil(t, captured.request.T, "T") {
		assert.Equal(t, expected, *captured.request.T, "T")
	}
}

// ============ netip types (struct kind, work with all tags) ============

func TestHook_NetipAddr_QueryTag(t *testing.T) {
	type Req struct {
		Addr netip.Addr `query:"addr"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("addr=192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected := netip.MustParseAddr("192.168.1.1")
	assert.Equal(t, expected, captured.request.Addr, "Addr")
}

func TestHook_NetipAddr_Invalid(t *testing.T) {
	type Req struct {
		Addr netip.Addr `query:"addr"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("addr=not-an-ip"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHook_NetipAddrPort_QueryTag(t *testing.T) {
	type Req struct {
		AP netip.AddrPort `query:"ap"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ap=192.168.1.1:8080"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected := netip.MustParseAddrPort("192.168.1.1:8080")
	assert.Equal(t, expected, captured.request.AP, "AP")
}

func TestHook_NetipPrefix_QueryTag(t *testing.T) {
	type Req struct {
		P netip.Prefix `query:"p"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("p=192.168.1.0/24"))
	assert.Equal(t, http.StatusOK, rec.Code)
	expected := netip.MustParsePrefix("192.168.1.0/24")
	assert.Equal(t, expected, captured.request.P, "P")
}

// ============ net.IP (slice kind, TextUnmarshaler) ============
//
// net.IP is `type IP []byte` — Kind == Slice, but it implements TextUnmarshaler
// (via pointer receiver). The pure-slice detection in the common package's decode
// hook checks `target.NumMethod() == 0` to distinguish plain slices (like []byte,
// []string) from named slice types (like net.IP) that have methods. This allows
// net.IP to be unboxed from a single-element []string and then handled by
// TextUnmarshaler, working with all tag types.

func TestHook_NetIP_UrlTag(t *testing.T) {
	type Req struct {
		IP net.IP `url:"ip"`
	}
	captured, rec := doServeMuxRequest[Req](t,
		http.MethodGet, "/{ip}", "/192.168.1.1",
		captureHandler[Req])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestHook_NetIP_QueryTag(t *testing.T) {
	type Req struct {
		IP net.IP `query:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ip=192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestHook_NetIP_HeaderTag(t *testing.T) {
	type Req struct {
		IP net.IP `header:"Ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withHeader("Ip", "192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestHook_NetIP_CookieTag(t *testing.T) {
	type Req struct {
		IP net.IP `cookie:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withCookie("ip", "192.168.1.1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

func TestHook_NetIP_FormTag(t *testing.T) {
	type Req struct {
		IP net.IP `form:"ip"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withFormBody(url.Values{"ip": {"192.168.1.1"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, net.IPv4(192, 168, 1, 1), captured.request.IP, "IP")
}

// Invalid IP should return 400 — TextUnmarshaler rejects the invalid input.
func TestHook_NetIP_Invalid(t *testing.T) {
	type Req struct {
		IP net.IP `query:"ip"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("ip=not-an-ip"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// *net.IP (pointer): unbox fires (Ptr != Slice), works as a contrast to net.IP.
func TestHook_NetIPPointer_QueryTag(t *testing.T) {
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

// ============ Custom TextUnmarshaler type ============

type myType struct {
	Value string
}

func (m *myType) UnmarshalText(text []byte) error {
	m.Value = "unmarshalled:" + string(text)
	return nil
}

func TestHook_CustomTextUnmarshaler_QueryTag(t *testing.T) {
	type Req struct {
		Val myType `query:"val"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("val=hello"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "unmarshalled:hello", captured.request.Val.Value, "Val")
}

func TestHook_CustomTextUnmarshaler_JsonTag(t *testing.T) {
	type Req struct {
		Val myType `json:"val"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"val":"world"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "unmarshalled:world", captured.request.Val.Value, "Val")
}

func TestHook_CustomTextUnmarshaler_EmptyJsonTag(t *testing.T) {
	type Req struct {
		Val myType `json:""`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`"test"`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "unmarshalled:test", captured.request.Val.Value, "Val")
}

// ============ Types that DON'T implement TextUnmarshaler ============

func TestHook_URLValue_DoesNotWork(t *testing.T) {
	type Req struct {
		Target url.URL `query:"target"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("target=https://example.com"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHook_URLPointer_DoesNotWork(t *testing.T) {
	type Req struct {
		Target *url.URL `url:"target"`
	}
	_, rec := doServeMuxRequest[Req](t,
		http.MethodGet, "/{target}", "/example.com",
		captureHandler[Req])
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============ Complex numbers (via strconv.ParseComplex) ============

func TestHook_ComplexNumber_QueryTag(t *testing.T) {
	type Req struct {
		C64  complex64  `query:"c64"`
		C128 complex128 `query:"c128"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("c64=1%2B2i&c128=3%2B4i"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, complex64(1+2i), captured.request.C64, "C64")
	assert.Equal(t, 3+4i, captured.request.C128, "C128")
}

func TestHook_ComplexNumber_Invalid(t *testing.T) {
	type Req struct {
		C complex64 `query:"c"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("c=not-a-complex"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============ mapstructure.Unmarshaler via hook ============

func TestHook_MapstructureUnmarshaler_QueryTag(t *testing.T) {
	type Req struct {
		Data mapstructureUnmarshalerType `query:"data"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("data=hello"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "mapstruct:hello", captured.request.Data.Value, "Data")
}

func TestHook_MapstructureUnmarshaler_Error(t *testing.T) {
	type Req struct {
		Data mapstructureUnmarshalerType `json:"data"`
	}
	// Send a JSON number — UnmarshalMapstructure expects string.
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"data":123}`)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============ strconv error paths for all numeric types ============

func TestHook_InvalidUint_QueryTag(t *testing.T) {
	type Req struct {
		V uint `query:"v"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("v=not-a-number"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHook_InvalidFloat_QueryTag(t *testing.T) {
	type Req struct {
		V float64 `query:"v"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("v=not-a-number"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHook_InvalidBool_QueryTag(t *testing.T) {
	type Req struct {
		V bool `query:"v"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("v=not-a-bool"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============ targetIsPureSlice wrapping (hook path) ============
//
// When source is a scalar (e.g. json.Number) but target is a pure slice
// (e.g. []int), the hook wraps the scalar into a single-element slice.
// This can happen with JSON: {"values":42} bound to []int.

func TestHook_TargetPureSlice_Wrapping(t *testing.T) {
	type Req struct {
		Values []int `json:"values"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"values":42}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []int{42}, captured.request.Values, "Values")
}

// ============ json.Number intermediate values (from decoder.UseNumber) ============
//
// When using a non-empty json:"fieldname" tag with a numeric field (int, float,
// etc.), the JSON body is decoded into map[string]any using decoder.UseNumber()
// (request_body.go), which produces json.Number values for JSON numbers. These values
// are then passed to common.BindStructWithTag, whose decode hook handles
// json.Number alongside string.

func TestHook_JSONNumber_JsonTag(t *testing.T) {
	type Req struct {
		Count int `json:"count"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json",
			[]byte(`{"count":42}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 42, captured.request.Count, "Count")
}

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
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Tests for charset detection and conversion in body parsing: BOM sniffing,
// charset parameter, UTF-16, ISO-8859-1, Windows-1252.

func TestCharset_UTF8_BOM(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// JSON with UTF-8 BOM prefix
	jsonBody := []byte(`{"data":"héllo"}`)
	bomBody := append([]byte{0xEF, 0xBB, 0xBF}, jsonBody...)
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", bomBody))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_UTF16LE_BOM(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Encode JSON as UTF-16LE with BOM
	jsonBody := []byte(`{"data":"hello"}`)
	utf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	encoded, _, err := transform.Bytes(utf16le.NewEncoder(), jsonBody)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}
	bomBody := append([]byte{0xFF, 0xFE}, encoded...)
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", bomBody))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_UTF16BE_BOM(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Encode JSON as UTF-16BE with BOM
	jsonBody := []byte(`{"data":"hello"}`)
	utf16be := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	encoded, _, err := transform.Bytes(utf16be.NewEncoder(), jsonBody)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}
	bomBody := append([]byte{0xFE, 0xFF}, encoded...)
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", bomBody))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_ISO8859_1(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Encode a string with ISO-8859-1 specific characters
	// "café" in ISO-8859-1: c=0x63, a=0x61, f=0x66, é=0xE9
	isoBody := []byte(`{"data":"caf` + "\xe9" + `"}`)
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=iso-8859-1", isoBody))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_ISO8859_1_Decodes(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// "café" in ISO-8859-1: é = 0xE9
	isoBody := []byte(`{"data":"caf` + "\xe9" + `"}`)
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=iso-8859-1", isoBody))
	assert.Equal(t, http.StatusOK, rec.Code)
	// After decoding, 0xE9 should become UTF-8 é (0xC3 0xA9)
	assert.Equal(t, "café", captured.request.Data, "Data")
}

func TestCharset_UTF8_Explicit(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=utf-8", []byte(`{"data":"héllo"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_InvalidCharset_415(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=invalid-charset", []byte(`{"data":"hello"}`)))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestCharset_NoCharset_NoBOM(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Plain UTF-8 without BOM and without charset parameter
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json", []byte(`{"data":"héllo"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "héllo", captured.request.Data, "Data")
}

func TestCharset_UTF16_WithoutBOM_WithCharset(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Encode JSON as UTF-16LE without BOM, but with charset=utf-16le
	jsonBody := []byte(`{"data":"hello"}`)
	utf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	encoded, _, err := transform.Bytes(utf16le.NewEncoder(), jsonBody)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}
	_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=utf-16le", encoded))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCharset_Windows1252(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	// Windows-1252 is a superset of ISO-8859-1
	// "€" in Windows-1252 is 0x80
	winBody := []byte(`{"data":"` + "\x80" + `"}`)
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=windows-1252", winBody))
	assert.Equal(t, http.StatusOK, rec.Code)
	// 0x80 in windows-1252 is €
	assert.Equal(t, "€", captured.request.Data, "Data")
}

func TestCharset_ISO8859_1_FormBody(t *testing.T) {
	type Req struct {
		Name string `form:"name"`
	}
	// "café" in ISO-8859-1: é = 0xE9
	formBody := []byte("name=caf" + "\xe9")
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/x-www-form-urlencoded; charset=iso-8859-1", formBody))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "café", captured.request.Name, "Name")
}

func TestCharset_USASCII_DecodesBody(t *testing.T) {
	type Req struct {
		Data string `json:"data"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/",
		withRawBody("application/json; charset=us-ascii", []byte(`{"data":"hello"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", captured.request.Data, "Data")
}

// ============ short body edge cases (BOM peek) ============

func TestCharset_OneByteBody_NoBOM_NoCharset(t *testing.T) {
	type Req struct {
		Data any `json:""`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json", []byte("0")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, captured.request.Data, "Data should be parsed")
}

func TestCharset_TwoByteBody_NoBOM_NoCharset(t *testing.T) {
	type Req struct {
		Data any `json:""`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json", []byte(`""`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Data, "Data should be empty string")
}

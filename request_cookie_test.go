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

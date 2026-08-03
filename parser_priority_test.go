/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for tag priority. When multiple tags are on the same field, the
// higher-priority tag's value wins.
// Priority (lowest → highest): default, header, cookie, query, url, form, json,
// multipart, body.

// ============ tag priority ============

// Tag priority (lowest → highest): default, header, cookie, query, url, form, json, multipart, body.
// When multiple tags are on the same field, the higher-priority tag's value wins.

type priorityHeaderQueryStruct struct {
	Name string `header:"X-Name" query:"name"`
}

func TestPriority_QueryOverridesHeader(t *testing.T) {
	captured, rec := doRequest[priorityHeaderQueryStruct](t, captureHandler[priorityHeaderQueryStruct],
		http.MethodGet, "/",
		withHeader("X-Name", "from-header"),
		withQuery("name=from-query"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-query", captured.request.Name, "query should override header")
}

func TestPriority_HeaderOnly(t *testing.T) {
	captured, rec := doRequest[priorityHeaderQueryStruct](t, captureHandler[priorityHeaderQueryStruct],
		http.MethodGet, "/",
		withHeader("X-Name", "from-header"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-header", captured.request.Name, "header value when no query")
}

func TestPriority_QueryOnly(t *testing.T) {
	captured, rec := doRequest[priorityHeaderQueryStruct](t, captureHandler[priorityHeaderQueryStruct],
		http.MethodGet, "/",
		withQuery("name=from-query"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-query", captured.request.Name, "query value when no header")
}

type priorityHeaderCookieStruct struct {
	Name string `header:"X-Name" cookie:"name"`
}

func TestPriority_CookieOverridesHeader(t *testing.T) {
	captured, rec := doRequest[priorityHeaderCookieStruct](t, captureHandler[priorityHeaderCookieStruct],
		http.MethodGet, "/",
		withHeader("X-Name", "from-header"),
		withCookie("name", "from-cookie"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-cookie", captured.request.Name, "cookie should override header")
}

type priorityCookieQueryStruct struct {
	Name string `cookie:"name" query:"name"`
}

func TestPriority_QueryOverridesCookie(t *testing.T) {
	captured, rec := doRequest[priorityCookieQueryStruct](t, captureHandler[priorityCookieQueryStruct],
		http.MethodGet, "/",
		withCookie("name", "from-cookie"),
		withQuery("name=from-query"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-query", captured.request.Name, "query should override cookie")
}

type priorityDefaultHeaderStruct struct {
	Name string `default:"default-val" header:"X-Name"`
}

func TestPriority_HeaderOverridesDefault(t *testing.T) {
	captured, rec := doRequest[priorityDefaultHeaderStruct](t, captureHandler[priorityDefaultHeaderStruct],
		http.MethodGet, "/",
		withHeader("X-Name", "from-header"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-header", captured.request.Name, "header should override default")
}

func TestPriority_DefaultWhenNoHeader(t *testing.T) {
	captured, rec := doRequest[priorityDefaultHeaderStruct](t, captureHandler[priorityDefaultHeaderStruct],
		http.MethodGet, "/",
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "default-val", captured.request.Name, "default value when no header")
}

type priorityQueryFormStruct struct {
	Name string `query:"name" form:"name"`
}

func TestPriority_FormOverridesQuery(t *testing.T) {
	captured, rec := doRequest[priorityQueryFormStruct](t, captureHandler[priorityQueryFormStruct],
		http.MethodPost, "/?name=from-query",
		withFormBody(url.Values{"name": {"from-form"}}),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-form", captured.request.Name, "form should override query")
}

type priorityQueryJsonStruct struct {
	Name string `query:"name" json:"name"`
}

func TestPriority_JsonOverridesQuery(t *testing.T) {
	captured, rec := doRequest[priorityQueryJsonStruct](t, captureHandler[priorityQueryJsonStruct],
		http.MethodPost, "/?name=from-query",
		withJSONBody(map[string]any{"name": "from-json"}),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from-json", captured.request.Name, "json should override query")
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============ url tag tests ============

type urlSingleStruct struct {
	ID string `url:"id"`
}

func TestUrlTag_SingleRouteParameter(t *testing.T) {
	captured, rec := doServeMuxRequest[urlSingleStruct](t,
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
	captured, rec := doServeMuxRequest[urlMultipleStruct](t,
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
	captured, rec := doServeMuxRequest[urlTypeCoercionStruct](t,
		http.MethodGet, "/items/{id}", "/items/99",
		captureHandler[urlTypeCoercionStruct])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 99, captured.request.ID, "ID")
}

type urlAllStruct struct {
	Params KeyValue `url:""`
}

func TestUrlTag_EmptyTag_BindsAllParams(t *testing.T) {
	captured, rec := doServeMuxRequest[urlAllStruct](t,
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

// ============ ServeMux wildcard/path-binding regression tests ============
//
// These validate the integration between the package's [getPathValues] helper
// and the [http.ServeMux] route-parameter feature.

func TestServeMux_PathParameter_BindsID(t *testing.T) {
	type Req struct {
		ID string `url:"id"`
	}
	captured, rec := doServeMuxRequest[Req](t,
		http.MethodGet, "/items/{id}", "/items/42",
		captureHandler[Req])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "42", captured.request.ID)
}

func TestServeMux_RemainderParameter_BindsPath(t *testing.T) {
	type Req struct {
		Path string `url:"path"`
	}
	mux := http.NewServeMux()
	mux.Handle("GET /files/{path...}", asTestHTTPHandler(RequestParser(func(ctx *Context, req Req) {
		ctx.NewResponse(http.StatusOK).JsonBody(req.Path)
	})))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/a/b/c", nil)
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `"a/b/c"`, rec.Body.String())
}

func TestServeMux_EmptyUrlTag_CollectsAllNamedParameters(t *testing.T) {
	type Req struct {
		Params KeyValue `url:""`
	}
	captured, rec := doServeMuxRequest[Req](t,
		http.MethodGet, "/users/{userId}/posts/{postId}", "/users/u123/posts/p456",
		captureHandler[Req])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, KeyValue{"userId": "u123", "postId": "p456"}, captured.request.Params)
}

// TestServeMux_NoWildcardPattern_LeavesURLFieldZero exercises the
// `len(keyValue) == 0` short-circuit in [requestTags.bindUrl]: a fixed pattern
// matches, but [getPathValues] yields no wildcard values, so the `url`-tagged
// field is left at its zero value.
func TestServeMux_NoWildcardPattern_LeavesURLFieldZero(t *testing.T) {
	type Req struct {
		ID string `url:"id"`
	}
	captured, rec := doServeMuxRequest[Req](t,
		http.MethodGet, "/fixed", "/fixed",
		captureHandler[Req])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.ID)
}

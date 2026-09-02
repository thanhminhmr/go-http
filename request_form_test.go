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
	"net/http/httptest"
	"net/url"
	"testing"
)

// ============ form tag tests ============

type formSingleStruct struct {
	Name string `form:"name"`
	Age  int    `form:"age"`
}

// TestFormTag_BodyMethodMatrix exercises the form binder across the POST/PUT/
// PATCH methods that share the body-parsing case in request_body.go.
func TestFormTag_BodyMethodMatrix(t *testing.T) {
	cases := []struct {
		name   string
		method string
		values url.Values
		want   formSingleStruct
	}{
		{"POST", http.MethodPost, url.Values{"name": {"alice"}, "age": {"30"}}, formSingleStruct{Name: "alice", Age: 30}},
		{"PUT", http.MethodPut, url.Values{"name": {"bob"}, "age": {"25"}}, formSingleStruct{Name: "bob", Age: 25}},
		{"PATCH", http.MethodPatch, url.Values{"name": {"carol"}}, formSingleStruct{Name: "carol"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
				tc.method, "/", withFormBody(tc.values))
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tc.want.Name, captured.request.Name, "Name")
			if tc.want.Age != 0 {
				assert.Equal(t, tc.want.Age, captured.request.Age, "Age")
			}
		})
	}
}

func TestFormTag_GET_IgnoresBody(t *testing.T) {
	captured, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
		http.MethodGet, "/", withFormBody(url.Values{"name": {"ignored"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Name, "Name (GET ignores body)")
}

func TestFormTag_WrongContentType_415(t *testing.T) {
	_, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("name=alice")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestFormTag_ContentLengthZero_SkipsBinding(t *testing.T) {
	var captured formSingleStruct
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	asTestHTTPHandler(RequestParser(func(ctx *Context, req formSingleStruct) {
		captured = req
		ctx.NewResponse(http.StatusOK)
	})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.Name, "Name (no body)")
}

type formAllStruct struct {
	Values KeyValues `form:""`
}

func TestFormTag_EmptyTag_BindsAllValues(t *testing.T) {
	captured, rec := doRequest[formAllStruct](t, captureHandler[formAllStruct],
		http.MethodPost, "/", withFormBody(url.Values{"a": {"1"}, "b": {"2"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"1"}, captured.request.Values["a"], "Values[a]")
	assert.Equal(t, []string{"2"}, captured.request.Values["b"], "Values[b]")
}

type formAllWrongTypeStruct struct {
	Values string `form:""`
}

func TestFormTag_EmptyTag_WrongType_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[formAllWrongTypeStruct])
	})
}

type formMultipleEmptyStruct struct {
	F1 KeyValues `form:""`
	F2 KeyValues `form:""`
}

func TestFormTag_MultipleEmptyTags_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[formMultipleEmptyStruct])
	})
}

type formNonEmptyAfterEmptyStruct struct {
	All    KeyValues `form:""`
	Single string    `form:"name"`
}

func TestFormTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(captureHandler[formNonEmptyAfterEmptyStruct])
	})
}

type formMultipleValuesStruct struct {
	IDs []string `form:"id"`
}

func TestFormTag_MultipleValues_SliceField(t *testing.T) {
	captured, rec := doRequest[formMultipleValuesStruct](t, captureHandler[formMultipleValuesStruct],
		http.MethodPost, "/", withFormBody(url.Values{"id": {"1", "2", "3"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"1", "2", "3"}, captured.request.IDs, "IDs")
}

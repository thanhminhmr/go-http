/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============ Embedded struct field tests ============
//
// These tests verify that embedded structs with tags work correctly, including:
//   - Empty tags (header:"", query:"", etc.) on embedded fields that store field
//     indices for direct binding.
//   - Non-empty tags on embedded fields, which go through common.BindStructWithTag
//     (mapstructure with Squash:true).
//   - Nested anonymous structs (multiple levels of embedding).
//   - Unexported fields are skipped.
//
// Tests that would panic use defer recover as a safety net because Go 1.26's
// testing framework repanics after catching a test panic, crashing the binary
// and preventing other tests from running. The recover calls t.Fatalf so the
// test still FAILs when the bug is present — it just does so cleanly.

func tagPanicGuard(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("unexpected panic — bug is present: %v", r)
	}
}

// ------------ default tag on embedded field ------------

func TestEmbed_DefaultTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Name string `default:"alice"`
	}
	type Base2 struct {
		Base
		Name string `default:"alice"`
	}
	type Req struct {
		Name string `default:"alice"`
		Base2
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Name)
}

// ------------ empty header tag on embedded field ------------

func TestEmbed_EmptyHeaderTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Headers http.Header `header:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withHeader("X-Custom", "value"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, http.Header{"X-Custom": {"value"}}, captured.request.Headers)
}

// ------------ empty cookie tag on embedded field ------------

func TestEmbed_EmptyCookieTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Cookies KeyValues `cookie:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withCookie("name", "value"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, KeyValues{"name": {"value"}}, captured.request.Cookies)
}

// ------------ empty query tag on embedded field ------------

func TestEmbed_EmptyQueryTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Params KeyValues `query:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withQuery("key=value"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, KeyValues{"key": {"value"}}, captured.request.Params)
}

// ------------ empty url tag on embedded field ------------

func TestEmbed_EmptyUrlTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Params KeyValue `url:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doChiRequest[Req](t, http.MethodGet, "/{id}", "/123",
		captureHandler[Req])
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, KeyValue{"id": "123"}, captured.request.Params)
}

// ------------ empty form tag on embedded field ------------
//
// bindForm is called through bindFullTextBody which runs the binder in a
// goroutine with a recover. We call createTags + bindForm directly here to
// test in the main goroutine for simplicity.

func TestEmbed_EmptyFormTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Values KeyValues `form:""`
	}
	type Req struct {
		Base
	}

	reqType := reflect.TypeFor[Req]()
	tags := createTags(reqType)

	var req Req
	parsed := reflect.ValueOf(&req).Elem()

	_, err := tags.bindForm(strings.NewReader("key=value"), parsed)
	assert.NoError(t, err)
	assert.Equal(t, KeyValues{"key": {"value"}}, req.Base.Values)
}

// ------------ empty json tag on embedded field ------------

func TestEmbed_EmptyJsonTag(t *testing.T) {
	type Base struct {
		Data map[string]any `json:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("application/json", []byte(`{"foo":"bar"}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, map[string]any{"foo": "bar"}, captured.request.Data)
}

// ------------ multipart tag on embedded field ------------

func TestEmbed_MultipartTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Reader *multipart.Reader `multipart:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withMultipartBody(t, func(w *multipart.Writer) {
			_ = w.WriteField("key", "value")
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, captured.request.Reader)
}

// ------------ body tag on embedded field ------------

func TestEmbed_BodyTag(t *testing.T) {
	defer tagPanicGuard(t)

	type Base struct {
		Body io.ReadCloser `body:""`
	}
	type Req struct {
		Base
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodPost, "/", withRawBody("text/plain", []byte("hello")))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, captured.request.Body)
}

// ------------ unexported fields are skipped ------------

func TestEmbed_UnexportedField_Skipped(t *testing.T) {
	type Req struct {
		Name string `query:"name"`
		age  int    // unexported — should be skipped
	}
	captured, rec := doRequest[Req](t, captureHandler[Req],
		http.MethodGet, "/", withQuery("name=alice"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Name, "Name")
	assert.Equal(t, 0, captured.request.age, "age (unexported, not set)")
}

// ------------ contrast: non-empty tags on embedded field work ------------
//
// Non-empty tags (header:"X", query:"q", etc.) don't store field indices.
// They set the tags.flags bit and binding goes through common.BindStructWithTag
// (mapstructure with Squash:true), which handles embedding correctly.

func TestEmbed_NonEmptyTag_Works(t *testing.T) {
	type Inner struct {
		Name string `query:"name"`
	}
	type Req struct {
		Inner
		Top string `query:"top"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/",
		withQuery("name=alice&top=hello"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Name)
	assert.Equal(t, "hello", captured.request.Top)
}

// ------------ nested anonymous structs (3 levels) ------------

func TestEmbed_NestedAnonymous(t *testing.T) {
	type Inner struct {
		Val string `query:"val"`
	}
	type Middle struct {
		Inner
		Mid string `query:"mid"`
	}
	type Req struct {
		Middle
		Top string `query:"top"`
	}
	captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("val=a&mid=b&top=c"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "a", captured.request.Val, "Val")
	assert.Equal(t, "b", captured.request.Mid, "Mid")
	assert.Equal(t, "c", captured.request.Top, "Top")
}

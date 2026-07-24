/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============ form tag tests ============

type formSingleStruct struct {
	Name string `form:"name"`
	Age  int    `form:"age"`
}

func TestFormTag_SingleField_POST(t *testing.T) {
	captured, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
		http.MethodPost, "/", withFormBody(url.Values{"name": {"alice"}, "age": {"30"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Name, "Name")
	assert.Equal(t, 30, captured.request.Age, "Age")
}

func TestFormTag_PUT(t *testing.T) {
	captured, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
		http.MethodPut, "/", withFormBody(url.Values{"name": {"bob"}, "age": {"25"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "bob", captured.request.Name, "Name")
	assert.Equal(t, 25, captured.request.Age, "Age")
}

func TestFormTag_PATCH(t *testing.T) {
	captured, rec := doRequest[formSingleStruct](t, captureHandler[formSingleStruct],
		http.MethodPatch, "/", withFormBody(url.Values{"name": {"carol"}}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "carol", captured.request.Name, "Name")
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
	RequestParser(func(ctx Context, req formSingleStruct) *Response {
		captured = req
		return ctx.Response(http.StatusOK)
	}).ServeHTTP(rec, req)
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
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty form tag with wrong type")
		}
	}()
	_ = RequestParser(captureHandler[formAllWrongTypeStruct])
}

type formMultipleEmptyStruct struct {
	F1 KeyValues `form:""`
	F2 KeyValues `form:""`
}

func TestFormTag_MultipleEmptyTags_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multiple empty form tags, got none")
		}
	}()
	_ = RequestParser(captureHandler[formMultipleEmptyStruct])
}

type formNonEmptyAfterEmptyStruct struct {
	All    KeyValues `form:""`
	Single string    `form:"name"`
}

func TestFormTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-empty form tag after empty form tag, got none")
		}
	}()
	_ = RequestParser(captureHandler[formNonEmptyAfterEmptyStruct])
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

// ============ json tag tests ============

type jsonSimpleStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type jsonWholeBodyStruct struct {
	Data jsonSimpleStruct `json:""`
}

func TestJsonTag_EmptyTag_BindsWholeBody(t *testing.T) {
	captured, rec := doRequest[jsonWholeBodyStruct](t, captureHandler[jsonWholeBodyStruct],
		http.MethodPost, "/", withJSONBody(jsonSimpleStruct{Name: "alice", Age: 30}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice", captured.request.Data.Name, "Name")
	assert.Equal(t, 30, captured.request.Data.Age, "Age")
}

type jsonFieldStruct struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Active string `json:"active"`
}

func TestJsonTag_NonEmptyTag_ExtractsFields(t *testing.T) {
	captured, rec := doRequest[jsonFieldStruct](t, captureHandler[jsonFieldStruct],
		http.MethodPost, "/", withJSONBody(map[string]any{"name": "bob", "email": "bob@test.com", "active": "yes"}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "bob", captured.request.Name, "Name")
	assert.Equal(t, "bob@test.com", captured.request.Email, "Email")
	assert.Equal(t, "yes", captured.request.Active, "Active")
}

func TestJsonTag_InvalidJSON_400(t *testing.T) {
	_, rec := doRequest[jsonFieldStruct](t, captureHandler[jsonFieldStruct],
		http.MethodPost, "/", withRawBody("application/json", []byte("{invalid json")))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestJsonTag_WrongContentType_415(t *testing.T) {
	_, rec := doRequest[jsonFieldStruct](t, captureHandler[jsonFieldStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte(`{"name":"x"}`)))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestJsonTag_ContentLengthZero_Skips(t *testing.T) {
	var captured jsonFieldStruct
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	RequestParser(func(ctx Context, req jsonFieldStruct) *Response {
		captured = req
		return ctx.Response(http.StatusOK)
	}).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.Name, "Name (no body)")
}

func TestJsonTag_NestedStruct(t *testing.T) {
	type nestedUser struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	type nestedStruct struct {
		User nestedUser `json:"user"`
	}
	captured, rec := doRequest[nestedStruct](t, captureHandler[nestedStruct],
		http.MethodPost, "/", withJSONBody(map[string]any{
			"user": map[string]any{"name": "nested", "email": "n@est.test"},
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "nested", captured.request.User.Name, "Name")
	assert.Equal(t, "n@est.test", captured.request.User.Email, "Email")
}

func TestJsonTag_UseNumber_Precision(t *testing.T) {
	type numberStruct struct {
		Value json.Number `json:"value"`
	}
	captured, rec := doRequest[numberStruct](t, captureHandler[numberStruct],
		http.MethodPost, "/", withRawBody("application/json", []byte(`{"value":12345678901234567890}`)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "12345678901234567890", captured.request.Value.String(), "Value")
}

type jsonMultipleEmptyStruct struct {
	A jsonSimpleStruct `json:""`
	B jsonSimpleStruct `json:""`
}

func TestJsonTag_EmptyTag_MultipleTags_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multiple empty json tags")
		}
	}()
	_ = RequestParser(captureHandler[jsonMultipleEmptyStruct])
}

type jsonNonEmptyAfterEmptyStruct struct {
	All    jsonSimpleStruct `json:""`
	Single string           `json:"name"`
}

func TestJsonTag_NonEmptyAfterEmpty_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-empty json tag after empty json tag, got none")
		}
	}()
	_ = RequestParser(captureHandler[jsonNonEmptyAfterEmptyStruct])
}

// ============ multipart tag tests ============

type multipartStruct struct {
	Reader *multipart.Reader `multipart:""`
}

func TestMultipartTag_BasicReader(t *testing.T) {
	captured, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withMultipartBody(t, func(w *multipart.Writer) {
			_ = w.WriteField("field1", "value1")
			_ = w.WriteField("field2", "value2")
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Reader == nil {
		t.Fatal("Reader is nil")
	}
	part, err := captured.request.Reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart failed: %v", err)
	}
	assert.Equal(t, "field1", part.FormName(), "first part form name")
	value, _ := io.ReadAll(part)
	assert.Equal(t, "value1", string(value), "first part value")
}

func TestMultipartTag_MissingBoundary_400(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	req.ContentLength = int64(len("garbage"))
	rec := httptest.NewRecorder()
	RequestParser(captureHandler[multipartStruct]).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMultipartTag_WrongContentType_415(t *testing.T) {
	_, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("data")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

type multipartNonEmptyStruct struct {
	Reader *multipart.Reader `multipart:"value"`
}

func TestMultipartTag_NonEmptyValue_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-empty multipart tag value")
		}
	}()
	_ = RequestParser(captureHandler[multipartNonEmptyStruct])
}

type multipartWrongTypeStruct struct {
	Reader string `multipart:""`
}

func TestMultipartTag_WrongType_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multipart tag with wrong type")
		}
	}()
	_ = RequestParser(captureHandler[multipartWrongTypeStruct])
}

type multipartMultipleStruct struct {
	R1 *multipart.Reader `multipart:""`
	R2 *multipart.Reader `multipart:""`
}

func TestMultipartTag_MultipleTags_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multiple multipart tags")
		}
	}()
	_ = RequestParser(captureHandler[multipartMultipleStruct])
}

func TestMultipartTag_FileUpload(t *testing.T) {
	captured, rec := doRequest[multipartStruct](t, captureHandler[multipartStruct],
		http.MethodPost, "/", withMultipartBody(t, func(w *multipart.Writer) {
			writer, err := w.CreateFormFile("upload", "test.txt")
			if err != nil {
				t.Fatalf("CreateFormFile failed: %v", err)
			}
			_, _ = writer.Write([]byte("file content"))
		}))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Reader == nil {
		t.Fatal("Reader is nil")
	}
	part, err := captured.request.Reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart failed: %v", err)
	}
	assert.Equal(t, "test.txt", part.FileName(), "file name")
	content, _ := io.ReadAll(part)
	assert.Equal(t, "file content", string(content), "content")
}

// ============ body tag tests ============

type bodyStruct struct {
	Body io.ReadCloser `body:""`
}

func TestBodyTag_Basic_BindsRawBody(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("raw body content")))
	assert.Equal(t, http.StatusOK, rec.Code)
	if captured.request.Body == nil {
		t.Fatal("Body is nil")
	}
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "raw body content", string(data), "Body")
}

type bodyWithContentTypesStruct struct {
	Body io.ReadCloser `body:"text/plain;application/xml"`
}

func TestBodyTag_WithContentTypes_AcceptsMatching(t *testing.T) {
	captured, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte("plain text")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "plain text", string(data), "Body")
}

func TestBodyTag_WithContentTypes_RejectsNonMatching_415(t *testing.T) {
	_, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("application/json", []byte("{}")))
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestBodyTag_MultipleContentTypes_AcceptsAny(t *testing.T) {
	captured, rec := doRequest[bodyWithContentTypesStruct](t, captureHandler[bodyWithContentTypesStruct],
		http.MethodPost, "/", withRawBody("application/xml", []byte("<x/>")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "<x/>", string(data), "Body")
}

type bodyConflictFormStruct struct {
	Body io.ReadCloser `body:"application/x-www-form-urlencoded"`
	Form url.Values    `form:"name"`
}

type bodyConflictJsonStruct struct {
	Body io.ReadCloser    `body:"application/json"`
	Data jsonSimpleStruct `json:""`
}

type bodyConflictMultipartStruct struct {
	Body   io.ReadCloser     `body:"multipart/form-data"`
	Reader *multipart.Reader `multipart:""`
}

type bodyMultipleStruct struct {
	B1 io.ReadCloser `body:""`
	B2 io.ReadCloser `body:""`
}

type bodyWrongTypeStruct struct {
	Body string `body:""`
}

func TestBodyTag_ConflictsWithForm_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for body tag conflicting with form tag")
		}
	}()
	_ = RequestParser(captureHandler[bodyConflictFormStruct])
}

func TestBodyTag_ConflictsWithJson_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for body tag conflicting with json tag")
		}
	}()
	_ = RequestParser(captureHandler[bodyConflictJsonStruct])
}

func TestBodyTag_ConflictsWithMultipart_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for body tag conflicting with multipart tag")
		}
	}()
	_ = RequestParser(captureHandler[bodyConflictMultipartStruct])
}

func TestBodyTag_MultipleTags_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multiple body tags")
		}
	}()
	_ = RequestParser(captureHandler[bodyMultipleStruct])
}

func TestBodyTag_WrongType_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for body tag with wrong type")
		}
	}()
	_ = RequestParser(captureHandler[bodyWrongTypeStruct])
}

// Test that body tag is used as fallback when no other body tags match
type bodyFallbackStruct struct {
	Body io.ReadCloser `body:""`
}

func TestBodyTag_Fallback_WhenNoOtherBodyTagMatches(t *testing.T) {
	captured, rec := doRequest[bodyFallbackStruct](t, captureHandler[bodyFallbackStruct],
		http.MethodPost, "/", withRawBody("application/octet-stream", []byte("binary data")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "binary data", string(data), "Body")
}

func TestBodyTag_BodyReadable(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 1024)
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", body))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Len(t, data, 1024, "Body length")
}

// ============ method variation tests ============

func TestJsonTag_PUT_ParsesBody(t *testing.T) {
	captured, rec := doRequest[jsonSimpleStruct](t, captureHandler[jsonSimpleStruct],
		http.MethodPut, "/", withJSONBody(map[string]any{"name": "put-data", "age": 25}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "put-data", captured.request.Name, "Name")
	assert.Equal(t, 25, captured.request.Age, "Age")
}

func TestJsonTag_PATCH_ParsesBody(t *testing.T) {
	captured, rec := doRequest[jsonSimpleStruct](t, captureHandler[jsonSimpleStruct],
		http.MethodPatch, "/", withJSONBody(map[string]any{"name": "patch-data", "age": 35}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "patch-data", captured.request.Name, "Name")
	assert.Equal(t, 35, captured.request.Age, "Age")
}

func TestBodyTag_PUT_BindsRawBody(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPut, "/", withRawBody("text/plain", []byte("put body")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "put body", string(data), "Body")
}

func TestBodyTag_PATCH_BindsRawBody(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPatch, "/", withRawBody("text/plain", []byte("patch body")))
	assert.Equal(t, http.StatusOK, rec.Code)
	data, _ := io.ReadAll(captured.request.Body)
	assert.Equal(t, "patch body", string(data), "Body")
}

type deleteBodyStruct struct {
	Name string        `json:"name"`
	Body io.ReadCloser `body:""`
}

func TestMethod_DELETE_SkipsBodyParsing(t *testing.T) {
	captured, rec := doRequest[deleteBodyStruct](t, captureHandler[deleteBodyStruct],
		http.MethodDelete, "/", withJSONBody(map[string]any{"name": "should-not-parse"}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Name, "Name should be zero value")
	assert.Nil(t, captured.request.Body, "Body should be nil")
}

func TestMethod_HEAD_SkipsBodyParsing(t *testing.T) {
	captured, rec := doRequest[deleteBodyStruct](t, captureHandler[deleteBodyStruct],
		http.MethodHead, "/", withJSONBody(map[string]any{"name": "should-not-parse"}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", captured.request.Name, "Name should be zero value")
	assert.Nil(t, captured.request.Body, "Body should be nil")
}

// ============ body tag content-length zero ============

func TestBodyTag_ContentLengthZero_SkipsBinding(t *testing.T) {
	captured, rec := doRequest[bodyStruct](t, captureHandler[bodyStruct],
		http.MethodPost, "/", withRawBody("text/plain", []byte{}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, captured.request.Body, "Body should be nil when ContentLength is 0")
}

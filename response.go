/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-exception"
)

// Context is passed to a [RequestHandler] and carries the request-scoped
// context along with the response header map. Use [Context.Response] to build a
// response.
type Context struct {
	// Ctx is the [context.Context] of the incoming request. It is canceled when
	// the client disconnects or the request completes.
	Ctx    context.Context
	header http.Header
}

// Response starts building a [*Response] with the given HTTP status code.
func (c Context) Response(status int) *Response {
	return &Response{status: status, header: c.header}
}

// Response represents an HTTP response returned by a [RequestHandler]. Create
// one with [Context.Response], then chain methods such as [Response.JsonBody]
// or [Response.Cookie] to configure it. The framework writes the response to
// the client after the handler returns.
type Response struct {
	status int
	header http.Header
	body   any
}

// Status returns the HTTP status code of the response.
func (r Response) Status() int {
	return r.status
}

// Header returns the response header map. Headers set here are sent with the
// response. Mutations affect the response directly.
func (r Response) Header() http.Header {
	return r.header
}

// Cookie adds a Set-Cookie header to the response and returns r for chaining.
func (r *Response) Cookie(cookie http.Cookie) *Response {
	r.header.Add("Set-Cookie", cookie.String())
	return r
}

// BytesBody sets the response body to body, written verbatim with no
// Content-Type. Returns r for chaining.
func (r *Response) BytesBody(body []byte) *Response {
	r.body = body
	return r
}

// StringBody sets the response body to body, written verbatim with no
// Content-Type. Returns r for chaining.
func (r *Response) StringBody(body string) *Response {
	r.body = body
	return r
}

// StreamBody sets the response body to a function that writes content directly
// to the [io.Writer]. The status code is written before body is invoked. No
// Content-Type is set. Returns r for chaining.
func (r *Response) StreamBody(body func(io.Writer) error) *Response {
	r.body = body
	return r
}

// PlainTextBody sets the response body to body with Content-Type
// "text/plain; charset=utf-8". Returns r for chaining.
func (r *Response) PlainTextBody(body string) *Response {
	r.body = plainTextBody{body: body}
	return r
}

// OctetsBody sets the response body to body with Content-Type
// "application/octet-stream". Returns r for chaining.
func (r *Response) OctetsBody(body []byte) *Response {
	r.body = octetsBody{body: body}
	return r
}

// JsonBody sets the response body to the JSON encoding of body with
// Content-Type "application/json; charset=utf-8". Returns r for chaining.
func (r *Response) JsonBody(body any) *Response {
	r.body = jsonBody{body: body}
	return r
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] so a Response
// can be embedded in structured log entries.
func (r Response) MarshalZerologObject(e *zerolog.Event) {
	e.Int("status", r.status)
	if len(r.header) > 0 {
		e.Any("header", r.header)
	}
	if r.body != nil {
		switch body := r.body.(type) {
		case plainTextBody:
			e.Str("body", body.body)
		case octetsBody:
			e.Bytes("body", body.body)
		case jsonBody:
			e.Any("body", body.body)
		default:
			e.Any("body", r.body)
		}
	}
}

type plainTextBody = struct{ body string }
type octetsBody = struct{ body []byte }
type jsonBody = struct{ body any }

func (r Response) write(writer http.ResponseWriter) error {
	switch body := r.body.(type) {
	case nil:
		writer.WriteHeader(r.status)
		return nil
	case []byte:
		writer.WriteHeader(r.status)
		_, err := writer.Write(body)
		return err
	case string:
		writer.WriteHeader(r.status)
		_, err := writer.Write(unsafeStringToBytes(body))
		return err
	case func(io.Writer) error:
		writer.WriteHeader(r.status)
		return body(writer)
	case plainTextBody:
		r.header.Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(r.status)
		_, err := writer.Write(unsafeStringToBytes(body.body))
		return err
	case octetsBody:
		r.header.Set("Content-Type", "application/octet-stream")
		writer.WriteHeader(r.status)
		_, err := writer.Write(body.body)
		return err
	case jsonBody:
		data, err := json.Marshal(body.body)
		if err == nil {
			r.header.Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(r.status)
			_, err = writer.Write(data)
		} else {
			writer.WriteHeader(http.StatusInternalServerError)
		}
		return err
	}
	writer.WriteHeader(http.StatusInternalServerError)
	return exception.String("Response: unsupported body type")
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"unsafe"

	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

type Context struct {
	ctx    context.Context
	header http.Header
}

func (c Context) Response(status Status) *Response {
	return &Response{status: status, header: c.header}
}

func (c Context) Ctx() context.Context {
	return c.ctx
}

type Response struct {
	status Status
	header Header
	body   any
}

func (r Response) Status() Status {
	return r.status
}

func (r Response) Header() Header {
	return r.header
}

func (r *Response) Cookie(cookie http.Cookie) *Response {
	r.header.Add("Set-Cookie", cookie.String())
	return r
}

func (r *Response) BytesBody(body []byte) *Response {
	r.body = body
	return r
}

func (r *Response) StringBody(body string) *Response {
	r.body = body
	return r
}

func (r *Response) StreamBody(body func(io.Writer) error) *Response {
	r.body = body
	return r
}

func (r *Response) PlainTextBody(body string) *Response {
	r.body = plainTextBody{body: body}
	return r
}

func (r *Response) OctetsBody(body []byte) *Response {
	r.body = octetsBody{body: body}
	return r
}

func (r *Response) JsonBody(body any) *Response {
	r.body = jsonBody{body: body}
	return r
}

func (r Response) MarshalZerologObject(e *ctrl.LogEvent) {
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
		_, err := writer.Write([]byte(body))
		return err
	case func(io.Writer) error:
		writer.WriteHeader(r.status)
		return body(writer)
	case plainTextBody:
		r.header.Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(r.status)
		_, err := writer.Write(unsafe.Slice(unsafe.StringData(body.body), len(body.body)))
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
		}
		return err
	}
	return exception.String("Response: unsupported body type")
}

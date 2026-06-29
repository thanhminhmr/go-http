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
	"reflect"

	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

func newContext(ctx *ctrl.LogCtx, writer http.ResponseWriter) *ctrl.LogCtx {
	return ctx.WithValue(reflect.TypeFor[Response](), writer.Header())
}

func NewResponse(ctx context.Context, status Status) Response {
	if header, ok := ctx.Value(reflect.TypeFor[Response]()).(http.Header); !ok || header == nil {
		panic("BUG: response header missing")
	} else {
		return Response{status: status, header: header}
	}
}

type Response struct {
	status Status
	header Header
	Body   any
}

func (r Response) Status() Status {
	return r.status
}

func (r Response) Header() Header {
	return r.header
}

func (r Response) Cookie(cookie http.Cookie) Response {
	r.header.Add("Set-Cookie", cookie.String())
	return r
}

func (r Response) BytesBody(body []byte) Response {
	r.Body = body
	return r
}

func (r Response) StringBody(body string) Response {
	r.Body = body
	return r
}

func (r Response) StreamBody(body func(io.Writer) error) Response {
	r.Body = body
	return r
}

func (r Response) OctetsBody(body []byte) Response {
	r.Body = OctetsBody{body: body}
	return r
}

func (r Response) JsonBody(body any) Response {
	r.Body = JsonBody{body: body}
	return r
}

func (r Response) MarshalZerologObject(e *ctrl.LogEvent) {
	e.Int("status", r.status)
	if len(r.header) > 0 {
		e.Any("header", r.header)
	}
	if r.Body != nil {
		e.Any("body", r.Body)
	}
}

type BytesBody = []byte
type StringBody = string
type StreamBody = func(io.Writer) error
type OctetsBody = struct{ body []byte }
type JsonBody = struct{ body any }

func (r Response) write(writer http.ResponseWriter) error {
	switch body := r.Body.(type) {
	case nil:
		writer.WriteHeader(r.status)
		return nil
	case BytesBody:
		writer.WriteHeader(r.status)
		_, err := writer.Write(body)
		return err
	case StringBody:
		writer.WriteHeader(r.status)
		_, err := writer.Write([]byte(body))
		return err
	case StreamBody:
		writer.WriteHeader(r.status)
		return body(writer)
	case OctetsBody:
		r.header.Set("Content-Type", "application/octet-stream")
		writer.WriteHeader(r.status)
		_, err := writer.Write(body.body)
		return err
	case JsonBody:
		r.header.Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(r.status)
		data, err := json.Marshal(body.body)
		if err == nil {
			_, err = writer.Write(data)
		}
		return err
	}
	return exception.String("Response: unsupported body type")
}

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

	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

type Context struct {
	context.Context
	header Header
}

func (c Context) Response(status Status) Response {
	return Response{
		status: status,
		header: c.header,
		body:   nil,
	}
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

func (r Response) Body() any {
	return r.body
}

func (r Response) SetBody(body any) {
	r.body = body
}

func (r Response) MarshalZerologObject(e *ctrl.LogEvent) {
	e.Int("status", r.status)
	if len(r.header) > 0 {
		e.Any("header", r.header)
	}
	if r.body != nil {
		e.Any("body", r.body)
	}
}

type RawBody = []byte
type StringBody = string
type StreamBody = func(io.Writer) error
type JsonBody = struct{ any }

func (r Response) write(writer http.ResponseWriter) error {
	switch body := r.body.(type) {
	case nil:
		writer.WriteHeader(r.status)
		return nil
	case RawBody:
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
	case JsonBody:
		r.header.Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(r.status)
		data, err := json.Marshal(body.any)
		if err == nil {
			_, err = writer.Write(data)
		}
		return err
	}
	return exception.String("Response: unsupported body type")
}

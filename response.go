/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/thanhminhmr/go-common/common"
	"github.com/thanhminhmr/go-exception"
)

type ResponseBuilder struct {
	header Header
}

func (c ResponseBuilder) HeaderAdd(key, value string) ResponseBuilder {
	c.header.Add(key, value)
	return c
}

func (c ResponseBuilder) HeaderSet(key, value string) ResponseBuilder {
	c.header.Set(key, value)
	return c
}

//func (c ResponseHeader) HeaderGet(key string) string { return c.header.Get(key) }

//func (c ResponseHeader) HeaderValues(key string) []string { return c.header.Values(key) }

func (c ResponseBuilder) AddCookie(cookie *Cookie) ResponseBuilder {
	c.header.Add("Set-Cookie", cookie.String())
	return c
}

func (c ResponseBuilder) BodyEmpty(status Status) Response {
	return Response{status: status}
}

func (c ResponseBuilder) BodyRaw(status Status, body []byte) Response {
	return Response{status: status, body: BodyRaw(body)}
}

func (c ResponseBuilder) BodyWriter(status Status, body ResponseBodyWriter) Response {
	return Response{status: status, body: BodyWriter(body)}
}

func (c ResponseBuilder) Body(status Status, body ResponseBody) Response {
	return Response{status: status, body: body}
}

func (c ResponseBuilder) BodyJson(status Status, body any) Response {
	c.header.Set("Content-Type", "application/json; charset=utf-8")
	return Response{
		status: status,
		body: BodyWriter(func(writer io.Writer) error {
			encoder := json.NewEncoder(writer)
			encoder.SetEscapeHTML(false)
			return encoder.Encode(body)
		}),
	}
}

type ResponseBody = common.Either[[]byte, ResponseBodyWriter]

type ResponseBodyWriter = func(io.Writer) error

func BodyWriter(b ResponseBodyWriter) ResponseBody {
	return common.Right[[]byte, ResponseBodyWriter](b)
}

func BodyRaw(b []byte) ResponseBody {
	return common.Left[[]byte, ResponseBodyWriter](b)
}

type Response struct {
	status Status
	body   ResponseBody
}

func (r Response) MarshalJSON() ([]byte, error) {
	bodyRaw, _ := r.body.Left()
	var bodyWriter exception.StackFrame
	if writer, exists := r.body.Right(); exists {
		bodyWriter = funcObject(writer)
	}
	return json.Marshal(struct {
		Status     string               `json:"status,omitempty"`
		BodyRaw    []byte               `json:"body_raw,omitempty"`
		BodyWriter exception.StackFrame `json:"body_writer,omitzero"`
	}{
		Status:     http.StatusText(r.status),
		BodyRaw:    bodyRaw,
		BodyWriter: bodyWriter,
	})
}

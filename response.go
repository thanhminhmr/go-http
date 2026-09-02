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
	"unsafe"

	"github.com/rs/zerolog"
)

const (
	marshallerIsDirect uint = iota
	marshallerIsJson
)

// Response is a handle to response state owned by a [Context]. Copies share the
// same state. The zero value is invalid.
type Response struct{ ctx *Context }

// Status returns the configured HTTP status, or zero before
// [Context.NewResponse] is called.
func (r Response) Status() int { return r.ctx.status }

// Header returns the live response header map. A later
// [Context.NewResponse] call clears it.
func (r Response) Header() http.Header { return r.ctx.writer.Header() }

// Body returns the configured body value, or nil if no body is set.
func (r Response) Body() any { return r.ctx.body }

// Cookie appends a Set-Cookie header for cookie to the response.
func (r Response) Cookie(cookie http.Cookie) {
	r.Header().Add("Set-Cookie", cookie.String())
}

// BytesBody sets a raw byte body without setting Content-Type.
func (r Response) BytesBody(body []byte) {
	r.ctx.body, r.ctx.marshaller = body, marshallerIsDirect
}

// StringBody sets a raw string body without setting Content-Type.
func (r Response) StringBody(body string) {
	r.ctx.body, r.ctx.marshaller = body, marshallerIsDirect
}

// StreamBody sets a body writer without setting Content-Type. The HTTP status is
// committed before body runs, so an error returned by body can be logged but
// cannot change the response status.
func (r Response) StreamBody(body func(io.Writer) error) {
	r.ctx.body, r.ctx.marshaller = body, marshallerIsDirect
}

// PlainTextBody sets body with Content-Type "text/plain; charset=utf-8".
func (r Response) PlainTextBody(body string) {
	r.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.ctx.body, r.ctx.marshaller = body, marshallerIsDirect
}

// OctetsBody sets body with Content-Type "application/octet-stream".
func (r Response) OctetsBody(body []byte) {
	r.Header().Set("Content-Type", "application/octet-stream")
	r.ctx.body, r.ctx.marshaller = body, marshallerIsDirect
}

// JsonBody stores body for JSON marshaling when the response is written.
// Successful marshaling sets Content-Type to "application/json; charset=utf-8".
// A marshal failure writes 500 Internal Server Error with an empty body.
func (r Response) JsonBody(body any) {
	r.ctx.body, r.ctx.marshaller = body, marshallerIsJson
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] for the
// configured status, headers, and body.
func (r Response) MarshalZerologObject(e *zerolog.Event) {
	e.Int("status", r.ctx.status)
	if header := r.ctx.writer.Header(); len(header) > 0 {
		e.Any("header", header)
	}
	if r.ctx.body != nil {
		e.Any("body", r.ctx.body)
	}
}

// writeResponse commits the response currently stored in c to the underlying
// http.ResponseWriter. Router.Handle calls it once after the handler chain
// returns. JSON marshal failures and unsupported body types become empty 500
// responses because they are caught before any header is committed. Body write
// and stream errors occur after [http.ResponseWriter.WriteHeader]; the
// response status is already on the wire and cannot be replaced, so the error
// is logged and the connection is then aborted via panic(http.ErrAbortHandler),
// which server.ServeHTTP and net/http treat as a silent connection close.
func (c *Context) writeResponse(requestCtx context.Context) {
	logger := zerolog.Ctx(requestCtx)
	switch c.marshaller {
	case marshallerIsJson:
		data, err := json.Marshal(c.body)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to marshal response as JSON")
			clear(c.writer.Header())
			c.writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		c.writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		c.writer.WriteHeader(c.status)
		if count, err := c.writer.Write(data); err != nil {
			logger.Error().Err(err).Int("count", count).Msg("Failed to write response")
			break
		}
		return
	default:
		switch body := c.body.(type) {
		case nil:
			if c.status == 0 {
				logger.Error().Msg("Response is missing")
				clear(c.writer.Header())
				c.writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			c.writer.WriteHeader(c.status)
			return
		case []byte:
			c.writer.WriteHeader(c.status)
			if count, err := c.writer.Write(body); err != nil {
				logger.Error().Err(err).Int("count", count).Msg("Failed to write response body")
				break
			}
			return
		case string:
			c.writer.WriteHeader(c.status)
			if count, err := c.writer.Write(unsafeStringToBytes(body)); err != nil {
				logger.Error().Err(err).Int("count", count).Msg("Failed to write response body")
				break
			}
			return
		case func(io.Writer) error:
			c.writer.WriteHeader(c.status)
			if err := body(c.writer); err != nil {
				logger.Error().Err(err).Msg("Failed to write response body")
				break
			}
			return
		default:
			logger.Error().Any("body", body).Msg("Unsupported response body type")
			clear(c.writer.Header())
			c.writer.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	panic(http.ErrAbortHandler)
}

// unsafeStringToBytes returns a zero-copy byte view of value for the immediate
// response write path. The returned slice aliases immutable string storage and
// must never be modified.
func unsafeStringToBytes(value string) []byte {
	return unsafe.Slice(unsafe.StringData(value), len(value))
}

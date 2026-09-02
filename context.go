/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"time"

	"github.com/thanhminhmr/go-common/common"
)

// Context is the per-request state passed to [Handler] and [Middleware]. It
// implements [context.Context] by delegating to the underlying HTTP request and
// owns the response state assembled by the handler chain.
//
// Context values are created by [Router.Handle]. The zero value is invalid, and
// a Context must not be copied after first use.
type Context struct {
	_ common.NoCopy

	request *http.Request
	writer  http.ResponseWriter

	// response
	status     int
	body       any
	marshaller uint
}

// Deadline delegates to the HTTP request context.
func (c *Context) Deadline() (deadline time.Time, ok bool) { return c.request.Context().Deadline() }

// Done delegates to the HTTP request context.
func (c *Context) Done() <-chan struct{} { return c.request.Context().Done() }

// Err delegates to the HTTP request context.
func (c *Context) Err() error { return c.request.Context().Err() }

// Value delegates to the HTTP request context.
func (c *Context) Value(key any) any { return c.request.Context().Value(key) }

// Response returns a handle to the current response without changing it. Its
// status is zero until [Context.NewResponse] is called.
func (c *Context) Response() Response { return Response{ctx: c} }

// NewResponse starts a new response with status and returns its handle. It
// clears the previous body and all response headers. The response is not written
// until the [Router.Handle] middleware and handler chain returns.
//
// NewResponse panics unless status is between 200 and 599.
func (c *Context) NewResponse(status int) Response {
	if status < 200 || status > 599 {
		panic("BUG: invalid status")
	}
	c.status, c.body, c.marshaller = status, nil, marshallerIsDirect
	clear(c.writer.Header())
	return Response{ctx: c}
}

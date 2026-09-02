/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"

	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-exception"
)

// Handler handles one HTTP request through a [Context]. A handler normally
// creates or replaces the response with [Context.NewResponse]. If the complete
// middleware and handler chain returns without creating a response,
// [Router.Handle] sends 500 Internal Server Error.
type Handler = func(ctx *Context)

// Middleware wraps a [Handler] in a chain. Call next to continue to the next
// middleware or the route handler. Returning without calling next short-circuits
// the chain. Code after next runs on the way out and may inspect or replace the
// downstream response through [Context.Response] or [Context.NewResponse].
type Middleware = func(ctx *Context, next func())

// Router registers [Handler] values on a shared [http.ServeMux] with an ordered
// middleware chain. Routers returned by [Router.Group] share the same ServeMux
// but keep independent middleware slices.
//
// The zero value is invalid. Create a Router with [NewServer] or derive one from
// an existing Router with [Router.Group].
type Router struct {
	serveMux    *http.ServeMux
	logger      *zerolog.Logger
	middlewares []Middleware
}

// WithLogger returns a Router that reports route registrations to logger.
// The receiver is not modified.
func (r Router) WithLogger(logger *zerolog.Logger) Router {
	return Router{
		serveMux:    r.serveMux,
		logger:      logger,
		middlewares: r.middlewares,
	}
}

// Group returns a Router that shares r's routes and logger and appends
// middlewares to r's middleware chain. Group does not mutate r and does not
// retain the caller's middleware slice.
func (r Router) Group(middlewares ...Middleware) Router {
	return Router{
		serveMux:    r.serveMux,
		logger:      r.logger,
		middlewares: append(append([]Middleware(nil), r.middlewares...), middlewares...),
	}
}

// Handle registers handler for pattern using [http.ServeMux] pattern syntax.
// Requests run r's middleware in order followed by handler. Middleware may stop
// the chain by returning without calling next.
//
// The response is written after the complete chain returns, so middleware may
// inspect or replace a downstream response after next returns. If the chain
// returns without creating a response, Handle writes 500 Internal Server Error.
// Registration errors and pattern conflicts follow [http.ServeMux] behavior.
func (r Router) Handle(pattern string, handler Handler) {
	var dispatcher func(*Context, int)
	dispatcher = func(ctx *Context, i int) {
		if i < len(r.middlewares) {
			r.middlewares[i](ctx, func() { dispatcher(ctx, i+1) })
		} else {
			handler(ctx)
		}
	}
	if r.logger != nil {
		r.logger.Info().Str("pattern", pattern).Array("middlewares", funcObjects(r.middlewares)).
			Object("handler", funcObject(handler)).Msg("Registering route")
	}
	r.serveMux.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
		ctx := &Context{request: request, writer: writer}
		dispatcher(ctx, 0)
		ctx.writeResponse(request.Context())
	})
}

// funcObject resolves a function value to the stack-frame representation used
// in route-registration logs. Values that cannot be resolved use an explicit
// <unknown> frame instead of failing route registration.
func funcObject(v any) exception.StackFrame {
	if frame, ok := exception.Function(v); ok {
		return frame
	}
	return exception.StackFrame{
		Function: "<unknown>",
		File:     "",
		Line:     0,
	}
}

// funcObjects resolves a slice of function values with [funcObject] while
// preserving order.
func funcObjects[S ~[]E, E any](values S) exception.StackFrames {
	frames := make(exception.StackFrames, 0, len(values))
	for _, value := range values {
		frames = append(frames, funcObject(value))
	}
	return frames
}

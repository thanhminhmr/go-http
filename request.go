/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"reflect"

	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-common/common"
)

// KeyValue contains all named ServeMux path wildcard values for an empty
// `url:""` request tag.
type KeyValue = map[string]string

// KeyValues contains all values for an empty `cookie:""`, `query:""`, or
// `form:""` request tag.
type KeyValues = map[string][]string

// RequestHandler handles a request after defaults, request binding, and
// validation have completed. The handler normally creates its response through
// [Context.NewResponse].
type RequestHandler[Request any] = func(ctx *Context, request Request)

// RequestParser converts a typed [RequestHandler] into a [Handler].
//
// Request must be a non-pointer struct. Its default values and request-binding
// tag layout are checked when RequestParser is called; an invalid request
// definition panics. For each HTTP request, RequestParser creates a fresh
// Request value, applies defaults, binds request data, validates the result, and
// then calls handler.
//
// Binding or validation failures configure an empty HTTP error response and do
// not call handler. RequestParser does not write the response itself; the
// enclosing [Router.Handle] writes it after the middleware and handler chain
// returns. Panics from handler propagate to the server boundary, where servers
// created by [NewServer] recover them.
func RequestParser[Request any](handler RequestHandler[Request]) Handler {
	tags := createTags(reflect.TypeFor[Request]())
	return func(ctx *Context) {
		var parsed Request
		requestHandler(ctx, &tags, &parsed, func(ctx *Context) { handler(ctx, parsed) })
	}
}

// MiddlewareHandler handles a parsed request around the next middleware or
// route handler. Call next to continue the chain. Returning without calling
// next short-circuits the chain; code after next may inspect or replace the
// downstream response.
type MiddlewareHandler[Request any] = func(ctx *Context, request Request, next func())

// MiddlewareParser converts a typed [MiddlewareHandler] into [Middleware].
// Request defaults, binding, and validation follow the same rules as
// [RequestParser].
//
// Parser middleware shares the same [Context] with downstream middleware and
// the route handler. Request bodies are not buffered or rewound, so a body
// consumed by one parser cannot be parsed again downstream.
//
// A binding or validation failure configures an error response and stops the
// chain. The response is written later by [Router.Handle].
func MiddlewareParser[Request any](handler MiddlewareHandler[Request]) Middleware {
	tags := createTags(reflect.TypeFor[Request]())
	return func(ctx *Context, next func()) {
		var parsed Request
		requestHandler(ctx, &tags, &parsed, func(ctx *Context) { handler(ctx, parsed, next) })
	}
}

// requestHandler is the shared execution path for RequestParser and
// MiddlewareParser. It applies defaults, binds request data, validates the
// resulting value, and calls next only on success. Failures update Context
// response state but never write directly to the network.
func requestHandler(ctx *Context, tags *requestTags, parsed any, next Handler) {
	logger := zerolog.Ctx(ctx)
	if err := common.ApplyDefaults(parsed); err != nil {
		logger.Error().Err(err).Msg("Failed to apply request defaults")
		ctx.NewResponse(http.StatusInternalServerError)
		return
	}
	if status, err := tags.parse(ctx.request, reflect.ValueOf(parsed).Elem()); err != nil {
		logger.Error().Err(err).Msg("Failed to parse request")
		ctx.NewResponse(status)
		return
	}
	if err := common.ValidateStruct(parsed); err != nil {
		logger.Error().Err(err).Msg("Failed to validate request")
		ctx.NewResponse(http.StatusBadRequest)
		return
	}
	logger.Trace().Any("parsed", parsed).Msg("Request parsed, calling handler...")
	next(ctx)
	logger.Trace().Object("response", ctx.Response()).Msg("Handler returned")
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

// Package httpserver provides typed request binding and response construction on
// top of the standard library [http.ServeMux].
//
// # Request flow
//
// Create a server with [NewServer], optionally derive routers with
// [Router.Group], and register routes with [Router.Handle].
//
// [RequestParser] turns a typed request handler into a [Handler]:
//
//	type GetUserRequest struct {
//		ID      string `url:"id" validate:"required"`
//		Verbose bool   `query:"verbose" default:"false"`
//	}
//
//	router.Handle("GET /users/{id}", RequestParser(
//		func(ctx *Context, request GetUserRequest) {
//			ctx.NewResponse(http.StatusOK).JsonBody(map[string]any{
//				"id":      request.ID,
//				"verbose": request.Verbose,
//			})
//		},
//	))
//
// [MiddlewareParser] provides the same typed request binding for [Middleware].
//
// For each request, defaults are applied first, request values are bound next,
// and validation runs last. The typed handler or middleware runs only when all
// steps succeed.
//
// Route patterns use standard [http.ServeMux] syntax. URL tags bind wildcards
// from the matched pattern.
//
// # Request tags
//
// Request fields are bound with tags of the form `source:"name"`:
//
//	type Request struct {
//		ID     string `url:"id"`
//		Search string `query:"q"`
//		Token  string `header:"Authorization"`
//	}
//
// The supported sources are:
//
//	header    HTTP headers
//	cookie    cookies
//	query     URL query parameters
//	url       ServeMux wildcards
//	form      application/x-www-form-urlencoded fields
//	json      JSON object fields
//	multipart multipart body parts (stream; see below)
//	body      raw request body (stream; see below)
//
// Named values are converted to the destination field type. Conversion failures
// are request errors and prevent the typed handler or middleware from running.
//
// An empty tag binds the complete source instead of one named value:
//
//	header:""    -> http.Header
//	cookie:""    -> KeyValues
//	query:""     -> KeyValues
//	url:""       -> KeyValue
//	form:""      -> KeyValues
//
// `json:""` is slightly different: it decodes the complete JSON value directly
// into the field.
//
// For a source, use either named fields or one whole-source field; do not mix
// both forms.
//
// Multipart and raw bodies are exposed as streams:
//
//	multipart:""             -> *multipart.Reader
//	body:""                  -> io.ReadCloser
//	body:"type/subtype ..."  -> io.ReadCloser for the listed media types
//
// The framework applies no size cap or read timeout to these streams; the
// handler owns any size or time budget (e.g. via [http.MaxBytesReader], the
// request context, or a self-imposed deadline). Form and JSON bindings are
// bounded by maxBodyLength (1 MiB) and maxReadBodyDuration (5s).
//
// `default:"value"` supplies a value before request binding.
//
// `validate:"rule"` validates the completed request after all binding has
// finished.
//
// # Binding
//
// A request starts at its zero value. Values are applied in this order:
//
//	default -> header -> cookie -> query -> URL -> body -> validation
//
// Later sources may overwrite values supplied by earlier sources.
//
// Body binding is considered for POST, PUT, PATCH, and DELETE requests. The body
// binder is selected from form, JSON, multipart, or raw body according to the
// request Content-Type.
//
// Form and JSON bodies are buffered and decoded before the typed handler runs.
// Multipart and raw body tags instead expose the live request stream and should
// be consumed during the handler or middleware that receives them. The framework
// applies no size or time cap to these streams; the handler owns any budget.
//
// # Responses and middleware
//
// Handlers construct responses with [Context.NewResponse] and [Response].
//
// A response is written only after the complete middleware and handler chain
// returns. Middleware can therefore inspect or replace the downstream response
// after calling next:
//
//	func(ctx *Context, next func()) {
//		// Before the downstream chain.
//
//		next()
//
//		// After the downstream chain.
//	}
//
// Middleware may short-circuit a request by returning without calling next.
//
// If the chain completes without creating a response, [Router.Handle] returns
// 500 Internal Server Error. Servers created by [NewServer] also recover panics
// at the HTTP boundary and return 500 when no final response has been committed.
package httpserver

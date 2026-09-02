/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for [MiddlewareParser]: a typed middleware that parses the request
// before calling next. All tests go through the real [Router.Handle] / ServeMux
// path via [newTestRouter] and [doRouterRequest].

// Successful parser middleware binds request data and calls downstream exactly
// once.
func TestMiddlewareParser_BindsData_CallsDownstreamOnce(t *testing.T) {
	type MWReq struct {
		UserID string `header:"X-User-Id"`
	}
	var capturedUserID string
	var downstreamCalls int32

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		capturedUserID = req.UserID
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		atomic.AddInt32(&downstreamCalls, 1)
		ctx.NewResponse(http.StatusOK)
	})

	rec := doRouterRequest(router, http.MethodGet, "/", withHeader("X-User-Id", "user123"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "user123", capturedUserID, "middleware should capture bound header")
	assert.Equal(t, int32(1), atomic.LoadInt32(&downstreamCalls), "downstream called exactly once")
}

// Defaults are applied before the middleware handler runs.
func TestMiddlewareParser_DefaultsAppliedBeforeHandler(t *testing.T) {
	type MWReq struct {
		Role string `default:"guest"`
	}
	var capturedRole string

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		capturedRole = req.Role
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		ctx.NewResponse(http.StatusOK)
	})

	rec := doRouterRequest(router, http.MethodGet, "/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "guest", capturedRole, "default should be applied before handler")
}

// Validation failure sets 400 and does not call downstream.
func TestMiddlewareParser_ValidationFailure_400_NoDownstream(t *testing.T) {
	type MWReq struct {
		Name string `query:"name" validate:"required"`
	}
	var mwHandlerCalled, downstreamCalled bool

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		mwHandlerCalled = true
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		downstreamCalled = true
		ctx.NewResponse(http.StatusOK)
	})

	rec := doRouterRequest(router, http.MethodGet, "/")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, mwHandlerCalled, "middleware handler should not run on validation failure")
	assert.False(t, downstreamCalled, "downstream should not run on validation failure")
}

// Parse/bind failure sets the expected status and does not call downstream.
func TestMiddlewareParser_BindFailure_400_NoDownstream(t *testing.T) {
	type MWReq struct {
		Count int `header:"X-Count"`
	}
	var mwHandlerCalled, downstreamCalled bool

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		mwHandlerCalled = true
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		downstreamCalled = true
		ctx.NewResponse(http.StatusOK)
	})

	rec := doRouterRequest(router, http.MethodGet, "/", withHeader("X-Count", "not-a-number"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, mwHandlerCalled, "middleware handler should not run on bind failure")
	assert.False(t, downstreamCalled, "downstream should not run on bind failure")
}

// Parsed middleware and a downstream [RequestParser] share the same [*Context]
// and response state.
func TestMiddlewareParser_SharesContextWithDownstreamRequestParser(t *testing.T) {
	type MWReq struct {
		Token string `header:"X-Token"`
	}
	type HandlerReq struct {
		Name string `query:"name"`
	}

	var mwCtx, handlerCtx *Context
	var statusAfterNext int

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		mwCtx = ctx
		next()
		statusAfterNext = ctx.Response().Status()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", RequestParser(func(ctx *Context, req HandlerReq) {
		handlerCtx = ctx
		ctx.NewResponse(http.StatusOK)
	}))

	rec := doRouterRequest(router, http.MethodGet, "/?name=alice", withHeader("X-Token", "tok123"))
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mwCtx)
	require.NotNil(t, handlerCtx)
	assert.Same(t, mwCtx, handlerCtx, "middleware and downstream must share the same *Context")
	assert.Equal(t, http.StatusOK, statusAfterNext, "middleware should see downstream's status via shared Context")
}

// A middleware can inspect the downstream response after next and then
// modify/replace it.
func TestMiddlewareParser_InspectAndReplaceDownstreamResponse(t *testing.T) {
	type MWReq struct{}

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		next()
		if ctx.Response().Status() == http.StatusOK {
			ctx.NewResponse(http.StatusTeapot).StringBody("replaced")
		}
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		ctx.NewResponse(http.StatusOK).StringBody("original")
	})

	rec := doRouterRequest(router, http.MethodGet, "/")
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "replaced", rec.Body.String())
}

// A middleware can configure a response and short-circuit without calling
// next.
func TestMiddlewareParser_ShortCircuitWithoutNext(t *testing.T) {
	type MWReq struct{}

	var downstreamCalled bool

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		ctx.NewResponse(http.StatusTeapot).StringBody("short-circuit")
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		downstreamCalled = true
		ctx.NewResponse(http.StatusOK)
	})

	rec := doRouterRequest(router, http.MethodGet, "/")
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "short-circuit", rec.Body.String())
	assert.False(t, downstreamCalled, "downstream must not run when middleware short-circuits")
}

// A chain that finishes without any response still becomes 500 via
// [Router.Handle].
func TestMiddlewareParser_ChainWithoutResponse_Becomes500(t *testing.T) {
	type MWReq struct{}

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("GET /", func(ctx *Context) {
		// no response configured
	})

	rec := doRouterRequest(router, http.MethodGet, "/")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// Lock in the documented body behavior.
//
// header/query-only parser middleware followed by a body parser succeeds: the
// middleware does not touch the body, so the downstream [RequestParser] can
// still read it.
func TestMiddlewareParser_HeaderMiddleware_ThenBodyParser_Succeeds(t *testing.T) {
	type MWReq struct {
		Token string `header:"X-Token"`
	}
	type HandlerReq struct {
		Name string `json:"name"`
	}

	var capturedToken, capturedName string

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		capturedToken = req.Token
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("POST /", RequestParser(func(ctx *Context, req HandlerReq) {
		capturedName = req.Name
		ctx.NewResponse(http.StatusOK)
	}))

	rec := doRouterRequest(router, http.MethodPost, "/",
		withHeader("X-Token", "tok123"),
		withJSONBody(map[string]any{"name": "alice"}),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "tok123", capturedToken, "middleware should capture token from header")
	assert.Equal(t, "alice", capturedName, "handler should capture name from JSON body")
}

// Two parser layers that both consume the body are not silently rewound or
// replayed: the second consumer hits EOF and fails.
func TestMiddlewareParser_TwoBodyConsumers_SecondFails(t *testing.T) {
	type MWReq struct {
		Token string `json:"token"`
	}
	type HandlerReq struct {
		Name string `json:"name"`
	}

	var mwHandlerCalled, downstreamHandlerCalled bool

	mw := MiddlewareParser(func(ctx *Context, req MWReq, next func()) {
		mwHandlerCalled = true
		next()
	})

	router := newTestRouter().Group(mw)
	router.Handle("POST /", RequestParser(func(ctx *Context, req HandlerReq) {
		downstreamHandlerCalled = true
		ctx.NewResponse(http.StatusOK)
	}))

	rec := doRouterRequest(router, http.MethodPost, "/",
		withJSONBody(map[string]any{"token": "tok", "name": "alice"}),
	)
	// Middleware consumed the body; downstream JSON parser sees EOF → 400.
	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"second body consumer should fail because body is already consumed")
	assert.True(t, mwHandlerCalled, "middleware handler should have run (first consumer)")
	assert.False(t, downstreamHandlerCalled, "downstream handler should not run (body parse failed)")
}

// Add one nested [Router.Group]([MiddlewareParser]) case so group composition
// and typed middleware are tested together.
func TestMiddlewareParser_NestedGroupComposition(t *testing.T) {
	type OuterMWReq struct {
		Token string `header:"X-Token"`
	}
	type InnerMWReq struct {
		APIKey string `header:"X-Api-Key"`
	}
	type HandlerReq struct {
		Name string `query:"name"`
	}

	var trace []string

	outerMW := MiddlewareParser(func(ctx *Context, req OuterMWReq, next func()) {
		trace = append(trace, "outer-before:"+req.Token)
		next()
		trace = append(trace, "outer-after")
	})

	innerMW := MiddlewareParser(func(ctx *Context, req InnerMWReq, next func()) {
		trace = append(trace, "inner-before:"+req.APIKey)
		next()
		trace = append(trace, "inner-after")
	})

	router := newTestRouter()
	outer := router.Group(outerMW)
	inner := outer.Group(innerMW)
	inner.Handle("GET /", RequestParser(func(ctx *Context, req HandlerReq) {
		trace = append(trace, "handler:"+req.Name)
		ctx.NewResponse(http.StatusOK)
	}))

	rec := doRouterRequest(router, http.MethodGet, "/?name=alice",
		withHeader("X-Token", "tok"),
		withHeader("X-Api-Key", "key"),
	)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{
		"outer-before:tok",
		"inner-before:key",
		"handler:alice",
		"inner-after",
		"outer-after",
	}, trace, "middleware execution order must be outer→inner→handler→inner→outer")
}

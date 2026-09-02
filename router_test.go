/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouterTestHelperSmoke proves the test helper reaches a handler registered
// with the real [Router.Handle] and that the configured response is written.
func TestRouterTestHelperSmoke(t *testing.T) {
	router := newTestRouter()
	router.Handle("GET /ping",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusOK).StringBody("pong")
		})

	rec := doRouterRequest(router, http.MethodGet, "/ping")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "pong", rec.Body.String())
}

// TestRouter_Handle_WritesConfiguredStatusAndBody verifies that a handler that
// configures a response has that response written through [Router.Handle].
func TestRouter_Handle_WritesConfiguredStatusAndBody(t *testing.T) {
	router := newTestRouter()
	router.Handle("GET /items",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusCreated).JsonBody(map[string]string{"id": "42"})
		})

	rec := doRouterRequest(router, http.MethodGet, "/items")

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"id":"42"}`, rec.Body.String())
}

// TestRouter_Handle_NoResponse_Produces500 verifies the fallback inside
// [Router.Handle]: a handler that never calls [Context.NewResponse] results in
// HTTP 500.
func TestRouter_Handle_NoResponse_Produces500(t *testing.T) {
	router := newTestRouter()
	router.Handle("GET /noop",
		func(ctx *Context) {
			// intentionally do not configure a response
		})

	rec := doRouterRequest(router, http.MethodGet, "/noop")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// recordingMiddleware appends label before calling next, then appends
// label+":after" once next returns.
func recordingMiddleware(trace *[]string, label string) Middleware {
	return func(ctx *Context, next func()) {
		*trace = append(*trace, label+":before")
		next()
		*trace = append(*trace, label+":after")
	}
}

// TestRouter_Middleware_Order verifies middleware execution order is exactly:
// outer before -> inner before -> handler -> inner after -> outer after.
func TestRouter_Middleware_Order(t *testing.T) {
	trace := make([]string, 0, 6)
	router := newTestRouter()
	router.middlewares = []Middleware{recordingMiddleware(&trace, "outer")}
	router.Group(recordingMiddleware(&trace, "inner")).Handle("GET /x",
		func(ctx *Context) {
			trace = append(trace, "handler")
			ctx.NewResponse(http.StatusOK)
		})

	rec := doRouterRequest(router, http.MethodGet, "/x")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{
		"outer:before",
		"inner:before",
		"handler",
		"inner:after",
		"outer:after",
	}, trace)
}

// TestRouter_Middleware_ShortCircuit verifies that middleware can short-circuit
// the chain by not calling next; downstream middleware/handler must not run.
func TestRouter_Middleware_ShortCircuit(t *testing.T) {
	trace := make([]string, 0, 4)
	router := newTestRouter()
	router.middlewares = []Middleware{
		func(ctx *Context, next func()) {
			trace = append(trace, "outer:before")
			next()
			trace = append(trace, "outer:after")
		},
	}
	router.Group(
		func(ctx *Context, next func()) {
			// do not call next; this short-circuits
			trace = append(trace, "short:before")
			ctx.NewResponse(http.StatusTeapot)
			trace = append(trace, "short:after")
		},
	).Handle("GET /x",
		func(ctx *Context) {
			t.Errorf("handler should not run when middleware short-circuits")
		})

	rec := doRouterRequest(router, http.MethodGet, "/x")

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, []string{
		"outer:before",
		"short:before",
		"short:after",
		"outer:after",
	}, trace)
}

// TestRouter_Middleware_ReplaceResponse_AfterNext verifies that a middleware
// can inspect the downstream response after next and then modify/replace it.
func TestRouter_Middleware_ReplaceResponse_AfterNext(t *testing.T) {
	router := newTestRouter()
	router.middlewares = []Middleware{
		func(ctx *Context, next func()) {
			next()
			// overwrite whatever downstream configured
			ctx.NewResponse(http.StatusTeapot).StringBody("overwritten")
		},
	}
	router.Handle("GET /x",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusOK).StringBody("original")
		})

	rec := doRouterRequest(router, http.MethodGet, "/x")

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "overwritten", rec.Body.String())
}

// TestRouter_Group_InheritsParentMiddleware verifies that a group inherits its
// parent's middleware in parent-first order.
func TestRouter_Group_InheritsParentMiddleware(t *testing.T) {
	trace := make([]string, 0, 2)
	router := newTestRouter()
	router.middlewares = []Middleware{recordingMiddleware(&trace, "parent")}
	// call Group with no additional middleware; the group should still run parent
	group := router.Group()
	assert.Len(t, group.middlewares, 1)
	group.Handle("GET /x",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusOK)
		})

	rec := doRouterRequest(router, http.MethodGet, "/x")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"parent:before", "parent:after"}, trace)
}

// TestRouter_NestedGroups_PreserveOrder verifies that nested groups preserve
// middleware order across all levels.
func TestRouter_NestedGroups_PreserveOrder(t *testing.T) {
	trace := make([]string, 0, 8)
	router := newTestRouter()
	router.middlewares = []Middleware{recordingMiddleware(&trace, "L1")}
	level2 := router.Group(recordingMiddleware(&trace, "L2"))
	level3 := level2.Group(recordingMiddleware(&trace, "L3"))
	assert.Len(t, level3.middlewares, 3)
	level3.Handle("GET /x",
		func(ctx *Context) {
			trace = append(trace, "handler")
			ctx.NewResponse(http.StatusOK)
		})

	rec := doRouterRequest(router, http.MethodGet, "/x")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{
		"L1:before", "L2:before", "L3:before",
		"handler",
		"L3:after", "L2:after", "L1:after",
	}, trace)
}

// TestRouter_Group_DoesNotMutateParentOrSibling verifies that creating a child
// group does not mutate the parent or a sibling group.
func TestRouter_Group_DoesNotMutateParentOrSibling(t *testing.T) {
	parentMW := recordingMiddleware(nil, "parent")
	router := newTestRouter()
	router.middlewares = []Middleware{parentMW}

	childA := router.Group(recordingMiddleware(nil, "A"))
	childB := router.Group(recordingMiddleware(nil, "B"))

	// parent has only the original middleware
	assert.Len(t, router.middlewares, 1)
	// childA and childB each carry one of their own, plus the parent
	assert.Len(t, childA.middlewares, 2)
	assert.Len(t, childB.middlewares, 2)
	// adding to childA must not affect parent or childB
	childA.Group(recordingMiddleware(nil, "A.child"))
	assert.Len(t, router.middlewares, 1)
	assert.Len(t, childB.middlewares, 2)
}

// TestRouter_Group_DoesNotAliasCallerSlice verifies that mutating the caller's
// input slice after calling Group does not affect the resulting group: Group
// does not retain the caller's middleware slice.
func TestRouter_Group_DoesNotAliasCallerSlice(t *testing.T) {
	m1 := func(ctx *Context, next func()) {
		next()
	}
	middlewares := []Middleware{m1}
	group := newTestRouter().Group(middlewares...)

	// caller now mutates the slice contents
	middlewares[0] = func(ctx *Context, next func()) {
		t.Errorf("mutated middleware should not be used")
		next()
	}

	// the group should still use m1
	require.Len(t, group.middlewares, 1)
	// identity check: the captured middleware must be m1
	ran := false
	group.middlewares[0](nil, func() { ran = true })
	assert.True(t, ran, "group middleware should still call next through m1")
}

// TestRouter_IndependentRoutes verifies that two routes registered from the
// same router remain independent.
func TestRouter_IndependentRoutes(t *testing.T) {
	router := newTestRouter()
	router.Handle("GET /a",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusOK).StringBody("a")
		})
	router.Handle("GET /b",
		func(ctx *Context) {
			ctx.NewResponse(http.StatusOK).StringBody("b")
		})

	recA := doRouterRequest(router, http.MethodGet, "/a")
	recB := doRouterRequest(router, http.MethodGet, "/b")

	assert.Equal(t, http.StatusOK, recA.Code)
	assert.Equal(t, "a", recA.Body.String())
	assert.Equal(t, http.StatusOK, recB.Code)
	assert.Equal(t, "b", recB.Body.String())
}

// TestRouter_Handle_WriteResponseError_LogsFailure exercises the
// `ctx.writeResponse()` error path in [Router.Handle]: a handler that returns a
// failing [Response.StreamBody] must produce a 200 (status already committed by
// StreamBody), must not panic, and the failure must be logged through zerolog.
func TestRouter_Handle_WriteResponseError_LogsFailure(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	router := Router{serveMux: http.NewServeMux(), logger: &logger}
	streamErr := errors.New("stream failed")
	router.Handle("GET /", func(ctx *Context) {
		ctx.NewResponse(http.StatusOK).StreamBody(func(io.Writer) error {
			return streamErr
		})
	})

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req = req.WithContext(logger.WithContext(req.Context()))

	rec := httptest.NewRecorder()
	// The stream-body error occurs after WriteHeader: the committed status
	// stays on the recorder, the error is logged, and writeResponse aborts the
	// connection via http.ErrAbortHandler.
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		router.serveMux.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, logBuf.String(), "Failed to write response")
	assert.Contains(t, logBuf.String(), "stream failed")
}

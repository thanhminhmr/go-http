/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// concurrencyResult captures the outcome of a single concurrent worker so the
// main test goroutine can run all assertions.
type concurrencyResult struct {
	index  int
	status int
	body   string
}

// TestConcurrency_SharedRouter_PerRequestIsolation proves that a single shared
// Router dispatches concurrent requests without cross-talk. Each worker sends a
// unique query value and the response body must echo back exactly that worker's
// own value (status-only assertions would miss data races in the parser cache).
func TestConcurrency_SharedRouter_PerRequestIsolation(t *testing.T) {
	type Req struct {
		Name string `query:"name" validate:"required"`
	}
	router := newTestRouter()
	router.Handle("GET /", RequestParser(func(ctx *Context, req Req) {
		ctx.NewResponse(http.StatusOK).StringBody(req.Name)
	}))

	const n = 200
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	results := make(chan concurrencyResult, n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			<-start
			target := "/?name=user" + strconv.Itoa(idx)
			rec := doRouterRequest(router, http.MethodGet, target)
			results <- concurrencyResult{
				index:  idx,
				status: rec.Code,
				body:   rec.Body.String(),
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	bodies := make(map[int]string, n)
	statuses := make(map[int]int, n)
	for r := range results {
		bodies[r.index] = r.body
		statuses[r.index] = r.status
	}
	for i := range n {
		assert.Equal(t, http.StatusOK, statuses[i], "worker %d status", i)
		expected := "user" + strconv.Itoa(i)
		assert.Equal(t, expected, bodies[i], "worker %d body", i)
	}
}

// TestConcurrency_SharedRouter_WithMiddleware race-tests the dispatcher with
// middleware attached. The middleware increments a shared atomic counter to
// prove every request traverses the chain; the body still echoes the unique
// per-worker value.
func TestConcurrency_SharedRouter_WithMiddleware(t *testing.T) {
	type Req struct {
		ID string `query:"id" validate:"required"`
	}
	router := newTestRouter()
	var mwHits atomic.Int64
	mw := func(ctx *Context, next func()) {
		mwHits.Add(1)
		next()
	}
	router.Group(mw).Handle("GET /", RequestParser(func(ctx *Context, req Req) {
		ctx.NewResponse(http.StatusOK).StringBody(req.ID)
	}))

	const n = 200
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	results := make(chan concurrencyResult, n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			<-start
			target := "/?id=req" + strconv.Itoa(idx)
			rec := doRouterRequest(router, http.MethodGet, target)
			results <- concurrencyResult{
				index:  idx,
				status: rec.Code,
				body:   rec.Body.String(),
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	assert.Equal(t, int64(n), mwHits.Load(), "middleware should run once per request")
	bodies := make(map[int]string, n)
	statuses := make(map[int]int, n)
	for r := range results {
		bodies[r.index] = r.body
		statuses[r.index] = r.status
	}
	for i := range n {
		assert.Equal(t, http.StatusOK, statuses[i], "worker %d status", i)
		expected := "req" + strconv.Itoa(i)
		assert.Equal(t, expected, bodies[i], "worker %d body", i)
	}
}

/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============ Goroutine: body close cancels in-flight reader ============
//
// In bindFullTextBody (parser.go), the binder runs in a separate goroutine.
// Two paths can trigger a return before the binder finishes:
//
//  1. Timeout: after maxReadBodyDuration (5 seconds), request.Body.Close()
//     is called, unblocking the binder's pending read.
//  2. Context cancellation: request.Context().Done() fires, returning 408.
//     The body is also closed via AddSuppressed(request.Body.Close()).
//
// In both cases, the goroutine leak is bounded: once the body closes, the
// blocked read fails and the goroutine exits quickly.

func TestGoroutine_BodyClose_CancelsInFlightRead(t *testing.T) {
	pr, _ := io.Pipe()

	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(pr)
		readDone <- err
	}()

	time.Sleep(10 * time.Millisecond)

	_ = pr.Close()

	select {
	case err := <-readDone:
		assert.Error(t, err, "read should fail after body close")
	case <-time.After(2 * time.Second):
		t.Fatal("reader goroutine did not unblock after body close (goroutine leak)")
	}
}

// TestGoroutine_BindFullTextBody_TimeoutClosesBody exercises the full 5-second
// timeout path. It is skipped in short mode.
func TestGoroutine_BindFullTextBody_TimeoutClosesBody(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 5-second timeout test in short mode")
	}

	pr, _ := io.Pipe()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          pr,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}

	binderDone := make(chan error, 1)
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		_, err := io.ReadAll(reader)
		binderDone <- err
		return http.StatusInternalServerError, err
	}

	contentTypeParams := map[string]string{"charset": "utf-8"}

	start := time.Now()
	status, err := bindFullTextBody(req, contentTypeParams, reflect.Value{}, binder)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusRequestTimeout, status, "expected timeout")
	assert.Error(t, err, "expected timeout error")
	assert.GreaterOrEqual(t, elapsed, 5*time.Second,
		"should not return before maxReadBodyDuration")

	select {
	case binderErr := <-binderDone:
		assert.Error(t, binderErr, "binder should fail after body close")
	case <-time.After(2 * time.Second):
		t.Fatal("binder goroutine did not exit after body close (goroutine leak)")
	}
}

// TestGoroutine_BindFullTextBody_ContextCancel_Returns408 verifies that when the
// request context is cancelled (client disconnect), bindFullTextBody returns
// 408 quickly without waiting for the 5-second timeout.
func TestGoroutine_BindFullTextBody_ContextCancel_Returns408(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          pr,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}
	req = req.WithContext(ctx)

	contentTypeParams := map[string]string{"charset": "utf-8"}
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		_, err := io.ReadAll(reader)
		return http.StatusInternalServerError, err
	}

	resultCh := make(chan struct {
		status int
		err    error
	}, 1)
	go func() {
		status, err := bindFullTextBody(req, contentTypeParams, reflect.Value{}, binder)
		resultCh <- struct {
			status int
			err    error
		}{status, err}
	}()

	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case result := <-resultCh:
		assert.Equal(t, http.StatusRequestTimeout, result.status)
		assert.Error(t, result.err)
		assert.Less(t, time.Since(start), time.Second,
			"should return quickly after context cancel, not wait 5s")
	case <-time.After(2 * time.Second):
		t.Fatal("bindFullTextBody did not return after context cancel")
	}

	// In real usage the HTTP server closes body after handler returns.
	// We simulate that here so the binder goroutine can exit.
	_ = pr.Close()
}

// TestGoroutine_BindFullTextBody_ContextCancel_BodyClose_KillsBinder verifies that
// after bindFullTextBody returns due to context cancellation, closing the
// request body causes the binder goroutine to exit promptly.
func TestGoroutine_BindFullTextBody_ContextCancel_BodyClose_KillsBinder(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          pr,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}
	req = req.WithContext(ctx)

	contentTypeParams := map[string]string{"charset": "utf-8"}
	binderDone := make(chan error, 1)
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		_, err := io.ReadAll(reader)
		binderDone <- err
		return http.StatusInternalServerError, err
	}

	go func() {
		_, _ = bindFullTextBody(req, contentTypeParams, reflect.Value{}, binder)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Close body (simulating what HTTP server does after handler returns)
	_ = pr.Close()

	select {
	case err := <-binderDone:
		assert.Error(t, err, "binder should fail after body close")
	case <-time.After(2 * time.Second):
		t.Fatal("binder goroutine did not exit after body close (goroutine leak)")
	}
}

// ============ Binder goroutine panic recovery ============
//
// When the binder goroutine panics (e.g. a user's UnmarshalText panics),
// the defer exception.Recover inside the goroutine catches it and returns
// 500 Internal Server Error instead of crashing the process.

type panicTextType string

func (p *panicTextType) UnmarshalText([]byte) error {
	panic("coverage: binder panic")
}

func TestGoroutine_BinderPanic_Recovered(t *testing.T) {
	type Req struct {
		Value panicTextType `form:"value"`
	}
	handler := RequestParser(func(_ Context, _ Req) *Response {
		return &Response{status: http.StatusOK}
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/",
		strings.NewReader("value=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.ContentLength = int64(len("value=hello"))
	handler.ServeHTTP(rec, req)
	// The panic in the binder goroutine should be caught by exception.Recover
	// and returned as 500 Internal Server Error.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

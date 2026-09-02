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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test infrastructure: deterministic synchronization for body-binding paths
// ============================================================================

// blockingReadCloser is a test-only [io.ReadCloser] that blocks Reads until
// Close is called. It signals readStarted the first time Read blocks so callers
// can deterministically know the binder goroutine is parked on the body read,
// then trigger the timeout or context-cancellation path without any sleeps.
type blockingReadCloser struct {
	readStartedOnce sync.Once
	readStarted     chan struct{}

	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (b *blockingReadCloser) Read(p []byte) (int, error) {
	b.readStartedOnce.Do(func() { close(b.readStarted) })
	<-b.closed
	return 0, io.EOF
}

func (b *blockingReadCloser) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// binderResult captures the outcome of a [bindFullTextBodyWithTimeout] call so
// worker goroutines can return errors and results to the test goroutine.
type binderResult struct {
	status int
	err    error
}

// ============================================================================
// Timeout path: binder exceeds the timeout, body is closed, goroutine exits
// ============================================================================

func TestBindFullTextBody_TimeoutClosesBodyAndBinderExits(t *testing.T) {
	body := newBlockingReadCloser()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          body,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}

	binderDone := make(chan struct{})
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		defer close(binderDone)
		_, err := io.ReadAll(reader)
		return http.StatusInternalServerError, err
	}

	const timeout = 50 * time.Millisecond
	start := time.Now()
	status, err := bindFullTextBodyWithTimeout(req, map[string]string{"charset": "utf-8"},
		reflect.Value{}, binder, timeout)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusRequestTimeout, status, "timeout should produce 408")
	assert.Error(t, err)
	assert.GreaterOrEqual(t, elapsed, timeout, "should wait at least the timeout")
	assert.Less(t, elapsed, 5*time.Second, "should not wait the production 5s duration")

	// Wait for the binder goroutine to exit: the production path closes the
	// body which unblocks the pending Read.
	select {
	case <-binderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("binder goroutine did not exit after body close (goroutine leak)")
	}
}

// ============================================================================
// Context cancellation: request context cancelled, body closed, binder exits
// ============================================================================

func TestBindFullTextBody_ContextCancel_Returns408AndClosesBody(t *testing.T) {
	body := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          body,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}
	req = req.WithContext(ctx)

	binderDone := make(chan struct{})
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		defer close(binderDone)
		_, err := io.ReadAll(reader)
		return http.StatusInternalServerError, err
	}

	resultCh := make(chan binderResult, 1)
	go func() {
		status, err := bindFullTextBodyWithTimeout(req, map[string]string{"charset": "utf-8"},
			reflect.Value{}, binder, 30*time.Second)
		resultCh <- binderResult{status: status, err: err}
	}()

	// Wait deterministically for the binder goroutine to be parked on the body
	// Read before cancelling the context.
	<-body.readStarted

	start := time.Now()
	cancel()

	select {
	case result := <-resultCh:
		assert.Equal(t, http.StatusRequestTimeout, result.status, "cancel should produce 408")
		assert.Error(t, result.err)
		assert.Less(t, time.Since(start), time.Second, "should return quickly after cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("bindFullTextBody did not return after context cancel")
	}

	// Production path already closed the body. Verify binder goroutine exits.
	select {
	case <-binderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("binder goroutine did not exit after body close (goroutine leak)")
	}
}

// ============================================================================
// Timeout vs cancellation race: both paths close the body cleanly
// ============================================================================

// TestBindFullTextBody_BodyCloseUnblocksBinderAfterContextCancel verifies that
// after the context-cancel path returns, closing the body causes the binder
// goroutine to exit promptly.
func TestBindFullTextBody_BodyCloseUnblocksBinderAfterContextCancel(t *testing.T) {
	body := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := &http.Request{
		Method:        http.MethodPost,
		Body:          body,
		ContentLength: 100,
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
	}
	req = req.WithContext(ctx)

	binderDone := make(chan struct{})
	binder := func(reader io.Reader, _ reflect.Value) (int, error) {
		defer close(binderDone)
		_, err := io.ReadAll(reader)
		return http.StatusInternalServerError, err
	}

	go func() {
		_, _ = bindFullTextBodyWithTimeout(req, map[string]string{"charset": "utf-8"},
			reflect.Value{}, binder, 30*time.Second)
	}()

	<-body.readStarted
	cancel()

	select {
	case <-binderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("binder goroutine did not exit after body close (goroutine leak)")
	}
}

// ============================================================================
// Binder goroutine panic propagation
// ============================================================================

type panicTextType string

func (p *panicTextType) UnmarshalText([]byte) error {
	panic("coverage: binder panic")
}

// TestBindFullTextBody_BinderPanicPropagates verifies that when the binder
// goroutine panics, the panic is re-raised in the caller goroutine. The test
// uses a form binder whose UnmarshalText panics; the form binder runs inside
// bindFullTextBody's goroutine via [tags.bindForm].
func TestBindFullTextBody_BinderPanicPropagates(t *testing.T) {
	require.Panics(t, func() {
		type Req struct {
			Value panicTextType `form:"value"`
		}
		handler := RequestParser(func(_ *Context, _ Req) {})
		rec := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/",
			strings.NewReader("value=hello"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		req.ContentLength = int64(len("value=hello"))
		asTestHTTPHandler(handler).ServeHTTP(rec, req)
	})
}

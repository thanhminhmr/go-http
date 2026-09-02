/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanhminhmr/go-common/ctrl"
)

// ============ httpServer.ServeHTTP integration tests ============

type testContextKey struct{ name string }

func newTestHTTPServer() *httpServer {
	serveMux := http.NewServeMux()
	return &httpServer{config: &ServerConfig{}, serveMux: serveMux}
}

func TestHTTPServer_NormalHandlerResponse(t *testing.T) {
	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}

func TestHTTPServer_PanicBeforeResponse_Becomes500(t *testing.T) {
	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		panic("before any response")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHTTPServer_PanicAfterResponse_StatusPreserved(t *testing.T) {
	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("committed"))
		panic("after commit")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	// After commit, the recover handler re-panics with http.ErrAbortHandler so
	// net/http silently closes the connection; the already-committed status and
	// body remain observable on the recorder.
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		server.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "committed", rec.Body.String())
}

func TestHTTPServer_InformationalThenPanic_500(t *testing.T) {
	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints) // 103 - informational
		panic("after informational")
	})

	fake := newFakeResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.ServeHTTP(fake, req)

	// 103 is informational (not a final commit), so recovery still writes 500.
	assert.Equal(t, []int{http.StatusEarlyHints, http.StatusInternalServerError}, fake.statuses)
}

func TestHTTPServer_RequestContextValuesAvailable(t *testing.T) {
	key := testContextKey{name: "user-id"}
	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		val := r.Context().Value(key)
		_, _ = w.Write([]byte(val.(string)))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), key, "u123"))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, "u123", rec.Body.String())
}

func TestHTTPServer_LoggerInstalledInContext(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		l := zerolog.Ctx(r.Context())
		l.Info().Msg("handler-visible")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(logger.WithContext(req.Context()))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Contains(t, logBuf.String(), "handler-visible")
}

func TestHTTPServer_LogFields_RequestAndResponse(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	server := newTestHTTPServer()
	server.serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("tea"))
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req = req.WithContext(logger.WithContext(req.Context()))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	require.Len(t, lines, 2, "expected request + response log lines")

	var reqLog map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &reqLog))
	assert.Equal(t, "GET", reqLog["method"])
	assert.Equal(t, "example.com", reqLog["host"])
	assert.Equal(t, "/", reqLog["path"])
	assert.NotEmpty(t, reqLog["request_id"])
	assert.Equal(t, "Request", reqLog["message"])

	var respLog map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &respLog))
	assert.EqualValues(t, http.StatusTeapot, respLog["status"])
	assert.EqualValues(t, 3, respLog["bytes"])
	// Don't assert on duration (non-deterministic) or request_id (random).
}

func TestHTTPServer_RouterHandleFullStack(t *testing.T) {
	serveMux := http.NewServeMux()
	server := &httpServer{config: &ServerConfig{}, serveMux: serveMux}

	nopLog := zerolog.Nop()
	router := Router{serveMux: serveMux, logger: &nopLog}
	router.Handle("/api", func(ctx *Context) {
		ctx.NewResponse(http.StatusOK).StringBody("from router")
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "from router", rec.Body.String())
}

// ============================================================================
// httpServer.runner serving-error policy
// ============================================================================

// TestHTTPServer_Runner_ListenError_ShutdownPolicy verifies that an unexpected
// ListenAndServe failure is logged, and that the shutdown callback is invoked
// exactly once when ShutdownOnError is true, and never when it is false.
func TestHTTPServer_Runner_ListenError_ShutdownPolicy(t *testing.T) {
	cases := []struct {
		name            string
		shutdownOnError bool
		expectShutdown  int32
	}{
		{"true", true, 1},
		{"false", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := zerolog.New(&logBuf)

			ctx := logger.WithContext(context.Background())
			var shutdownCalled atomic.Int32
			shutdown := func() { shutdownCalled.Add(1) }

			server := &httpServer{
				config: &ServerConfig{ShutdownOnError: tc.shutdownOnError},
				server: http.Server{Addr: "127.0.0.1:not-a-port"},
			}

			assert.NotPanics(t, func() { server.runner(ctx, shutdown) })

			assert.Equal(t, tc.expectShutdown, shutdownCalled.Load())
			assert.Contains(t, logBuf.String(), "Server closed with error")
		})
	}
}

// TestHTTPServer_Runner_ServerClosed_IsIgnored verifies that the normal
// ErrServerClosed case is silently ignored by [httpServer.runner]: no error is
// logged and the shutdown callback is not invoked.
func TestHTTPServer_Runner_ServerClosed_IsIgnored(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	ctx := logger.WithContext(context.Background())

	var shutdownCalled atomic.Int32
	shutdown := func() { shutdownCalled.Add(1) }

	server := &httpServer{
		config: &ServerConfig{ShutdownOnError: true},
		server: http.Server{Addr: "127.0.0.1:not-a-port"},
	}

	// Shutdown the server first so ListenAndServe returns http.ErrServerClosed.
	require.NoError(t, server.server.Shutdown(context.Background()))

	assert.NotPanics(t, func() { server.runner(ctx, shutdown) })

	assert.Equal(t, int32(0), shutdownCalled.Load())
	assert.NotContains(t, logBuf.String(), "Server closed with error")
}

// ============================================================================
// httpServer.cleaner
// ============================================================================

// TestHTTPServer_Cleaner_Success verifies that the success path emits the
// expected log sequence ("Shutting down...", "Shutdown complete") and does not
// log "Error while shutting down".
func TestHTTPServer_Cleaner_Success(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	ctx := logger.WithContext(context.Background())

	server := &httpServer{
		config: &ServerConfig{},
		server: http.Server{Addr: "127.0.0.1:0"},
	}

	server.cleaner(ctx)

	assert.Contains(t, logBuf.String(), "Shutting down...")
	assert.Contains(t, logBuf.String(), "Shutdown complete")
	assert.NotContains(t, logBuf.String(), "Error while shutting down")
}

// TestHTTPServer_Cleaner_ShutdownError_LogsAndCompletes verifies that when
// server.Shutdown(ctx) returns an error (due to a canceled context with an
// active request still in flight), cleaner logs the error and the final
// "Shutdown complete" message.
func TestHTTPServer_Cleaner_ShutdownError_LogsAndCompletes(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	started := make(chan struct{})
	release := make(chan struct{})

	server := &httpServer{
		config: &ServerConfig{},
		server: http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				w.WriteHeader(http.StatusOK)
			}),
		},
	}

	// Start serving in a goroutine.
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.server.Serve(listener)
	}()

	// Issue one HTTP request in another goroutine.
	reqDone := make(chan struct{})
	var resp *http.Response
	go func() {
		defer close(reqDone)
		resp, err = http.Get("http://" + listener.Addr().String())
	}()

	// Wait for the handler to start. Do not use a fixed sleep.
	<-started

	// Failure paths must also release the blocked handler.
	t.Cleanup(func() {
		close(release)
		<-reqDone
		<-serveDone
		if resp != nil {
			_ = resp.Body.Close()
		}
	})

	// Cancel the context before calling cleaner so Shutdown returns the context
	// error while the active connection is still in flight.
	ctx, cancel := context.WithCancel(logger.WithContext(context.Background()))
	cancel()

	server.cleaner(ctx)

	assert.Contains(t, logBuf.String(), "Error while shutting down")
	assert.Contains(t, logBuf.String(), "Shutdown complete")
}

// freeTCPPort reserves an ephemeral TCP port by opening a listener on
// 127.0.0.1:0, reading the assigned port, and then closing the listener.
// The caller uses the returned port to bind the real server. This pattern is
// inherently racy, but is acceptable for tests that bind immediately.
func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().(*net.TCPAddr)
	require.NoError(t, listener.Close())
	return uint16(addr.Port)
}

// TestNewServer_Lifecycle exercises the full [NewServer] wiring through a
// minimal fake lifecycle: it captures the registered starter, invokes it to
// obtain the runner and cleaner, starts the runner, makes one real HTTP
// request, and then triggers cleanup. It must restore [registerServer] and
// leave no running goroutine or open listener behind.
func TestNewServer_Lifecycle(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	port := freeTCPPort(t)

	// Capture the starter instead of running it immediately.
	originalRegister := registerServer
	t.Cleanup(func() { registerServer = originalRegister })

	var starter ctrl.Starter
	registerServer = func(s ctrl.Starter) { starter = s }

	config := &ServerConfig{
		Port:              port,
		ReadHeaderTimeout: 5,
		IdleTimeout:       60,
		MaxHeaderBytes:    4096,
		ShutdownOnError:   true,
	}

	router := NewServer(config).WithLogger(&logger)
	require.NotNil(t, starter, "NewServer must register exactly one starter")

	router.Handle("GET /", func(ctx *Context) {
		ctx.NewResponse(http.StatusOK).StringBody("ok")
	})

	lifecycleCtx, lifecycleCancel := context.WithCancel(
		logger.WithContext(context.Background()),
	)
	defer lifecycleCancel()

	runner, cleaner := starter(lifecycleCtx, lifecycleCtx)
	require.NotNil(t, runner)
	require.NotNil(t, cleaner)

	// Defensive cleanup so an earlier assertion failure still stops the
	// server. Calling the cleaner twice is harmless because
	// http.Server.Shutdown on an already-shutdown server returns nil.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		cleaner(ctx)
	})

	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		runner(lifecycleCtx, lifecycleCancel)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	var responseStatus int
	var responseBody string
	require.Eventually(t, func() bool {
		client := &http.Client{Timeout: 100 * time.Millisecond}
		resp, err := client.Get(url)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return false
		}
		responseStatus = resp.StatusCode
		responseBody = string(data)
		return resp.StatusCode == http.StatusOK
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, http.StatusOK, responseStatus)
	assert.Equal(t, "ok", responseBody)

	cleanupCtx, cleanupCancel := context.WithTimeout(
		logger.WithContext(context.Background()),
		time.Second,
	)
	defer cleanupCancel()
	cleaner(cleanupCtx)

	lifecycleCancel()

	select {
	case <-runnerDone:
		// success
	case <-time.After(time.Second):
		t.Fatal("HTTP server runner did not stop")
	}

	assert.Contains(t, logBuf.String(), "Start serving")
	assert.Contains(t, logBuf.String(), "Shutting down...")
	assert.Contains(t, logBuf.String(), "Shutdown complete")
	assert.NotContains(t, logBuf.String(), "Server closed with error")
}

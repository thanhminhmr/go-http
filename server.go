/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

// ServerConfig configures the [http.Server] registered by [NewServer]. Timeout
// values are in seconds. NewServer does not apply defaults or validate the
// configuration tags.
type ServerConfig struct {
	// Port is the TCP port to listen on all interfaces.
	Port uint16 `cfg:"port" validate:"required" default:"8080"`

	// ReadHeaderTimeout limits time spent reading request headers, in seconds.
	ReadHeaderTimeout int `cfg:"read_header_timeout" validate:"min=1,max=60" default:"5"`

	// IdleTimeout limits idle keep-alive time, in seconds.
	IdleTimeout int `cfg:"idle_timeout" validate:"min=1,max=3600" default:"60"`

	// MaxHeaderBytes limits request header size in bytes.
	MaxHeaderBytes int `cfg:"max_header_bytes" validate:"min=0,max=65536" default:"4096"`

	// ShutdownOnError cancels the application when serving fails unexpectedly.
	ShutdownOnError bool `cfg:"shutdown_on_error" default:"true"`
}

// registerServer is the seam through which [NewServer] registers its
// lifecycle starter. Production code uses [ctrl.Register]; tests may replace
// this variable to capture the starter without involving controller globals.
var registerServer = ctrl.Register

// NewServer creates a Router backed by a new [http.ServeMux] and registers its
// HTTP server with the [ctrl] lifecycle. The server wraps all requests with
// request/response logging and panic recovery.
//
// The server listens on ":<config.Port>" when the lifecycle starts and shuts
// down during cleanup. config must already have defaults applied and be
// validated before NewServer is called, and it should not be modified afterward.
// If serving fails unexpectedly, config.ShutdownOnError controls whether the
// application lifecycle is canceled.
func NewServer(config *ServerConfig) Router {
	serveMux := http.NewServeMux()
	registerServer(func(ctx, globalCtx context.Context) (ctrl.Runner, ctrl.Cleaner) {
		var server httpServer
		server = httpServer{
			config:   config,
			serveMux: serveMux,
			server: http.Server{
				Addr:              fmt.Sprintf(":%d", config.Port),
				Handler:           &server,
				ReadHeaderTimeout: time.Duration(config.ReadHeaderTimeout) * time.Second,
				IdleTimeout:       time.Duration(config.IdleTimeout) * time.Second,
				MaxHeaderBytes:    config.MaxHeaderBytes,
				BaseContext:       func(_ net.Listener) context.Context { return globalCtx },
			},
		}
		return server.runner, server.cleaner
	})
	return Router{serveMux: serveMux}
}

// httpServer is the outer net/http boundary around the package ServeMux. It owns
// the configured http.Server and adds per-request logging, response accounting,
// and panic recovery before dispatching to registered routes.
type httpServer struct {
	config   *ServerConfig
	serveMux *http.ServeMux
	server   http.Server
}

// ServeHTTP installs a request-scoped logger, records response status, bytes,
// and duration, recovers panics, and dispatches to serveMux. A panic before a
// final response is committed becomes 500 Internal Server Error; a panic after
// commitment preserves the already-committed response status.
func (s *httpServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	logger := zerolog.Ctx(request.Context()).With().
		Str("request_id", strconv.FormatUint(rand.Uint64(), 36)).Logger()
	logger.Info().Str("method", request.Method).Str("host", request.Host).
		Str("path", request.URL.Path).Msg("Request")
	start := time.Now()
	trackerWriter := &responseTracker{ResponseWriter: writer}
	defer func(start time.Time, wrappedWriter *responseTracker) {
		duration := time.Since(start)
		logger.Info().Int("status", wrappedWriter.Status).
			Int("bytes", wrappedWriter.BytesWritten).
			Dur("duration", duration).
			Msg("Response")
	}(start, trackerWriter)
	defer exception.Recover(func(recovered exception.Exception) {
		logger.Error().Any("recovered", recovered).Msg("Recovered from panic")
		if trackerWriter.Status == 0 {
			clear(trackerWriter.Header())
			trackerWriter.WriteHeader(http.StatusInternalServerError)
			return
		}
		panic(http.ErrAbortHandler)
	})
	s.serveMux.ServeHTTP(trackerWriter, request.WithContext(logger.WithContext(request.Context())))
}

// runner serves until http.Server stops. Unexpected serve errors are logged and
// cancel the application lifecycle when ShutdownOnError is enabled.
func (s *httpServer) runner(ctx context.Context, shutdown context.CancelFunc) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("address", s.server.Addr).Msg("Start serving")
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error().Err(err).Msg("Server closed with error")
		if s.config.ShutdownOnError {
			shutdown()
		}
	}
}

// cleaner gracefully shuts down the HTTP server with the cleanup context and
// logs any shutdown error.
func (s *httpServer) cleaner(ctx context.Context) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Shutting down...")
	if err := s.server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Error while shutting down")
	}
	logger.Info().Msg("Shutdown complete")
}

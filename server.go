/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/thanhminhmr/go-common/ctrl"
	"github.com/thanhminhmr/go-exception"
)

type ServerConfig struct {
	Port              uint16 `env:"HTTP_SERVER_PORT" validate:"required"`
	ReadHeaderTimeout int    `env:"HTTP_SERVER_READ_HEADER_TIMEOUT" validate:"min=0,max=60" default:"5"`
	IdleTimeout       int    `env:"HTTP_SERVER_IDLE_TIMEOUT" validate:"min=0,max=3600" default:"60"`
	MaxHeaderBytes    int    `env:"HTTP_SERVER_MAX_HEADER_BYTES" validate:"min=0,max=65536" default:"4096"`
	ShutdownOnError   bool   `env:"HTTP_SERVER_SHUTDOWN_ON_ERROR" default:"true"`
}

func NewServer(config *ServerConfig) *chi.Mux {
	// create route
	router := chi.NewRouter()
	// start the server
	ctrl.Register(func(ctx context.Context) (ctrl.Runner, ctrl.Cleaner) {
		// create the http server
		server := httpServer{
			config: config,
			router: router,
			server: http.Server{
				Addr:              string(strconv.AppendUint([]byte{':'}, uint64(config.Port), 10)),
				Handler:           router,
				ReadHeaderTimeout: time.Duration(config.ReadHeaderTimeout) * time.Second,
				IdleTimeout:       time.Duration(config.IdleTimeout) * time.Second,
				MaxHeaderBytes:    config.MaxHeaderBytes,
			},
		}
		// set a sane default middleware stack
		router.Use(requestLogger)
		// return the runner and the cleaner
		return server.runner, server.cleaner
	})
	// return the router and the starter func
	return router
}

type httpServer struct {
	config *ServerConfig
	router *chi.Mux
	server http.Server
}

func (s *httpServer) runner(ctx context.Context, shutdown context.CancelFunc) {
	logger := ctrl.Logger(ctx)
	// dump all routes
	logger.Info().Msg("Listing all routes...")
	if err := chi.Walk(s.router, func(method string, route string, handler Handler, middlewares ...Middleware) error {
		logger.Info().Str("method", method).Str("route", route).
			Object("handler", funcObject(handler)).
			Array("middlewares", funcObjects(middlewares)).
			Msg("Route")
		return nil
	}); err != nil {
		logger.Error().Err(err).Msg("Error walking routes")
		exception.Panic(err)
	}
	logger.Info().Msg("Listed all routes")
	// start the server
	logger.Info().Str("address", s.server.Addr).Msg("Start serving")
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error().Err(err).Msg("Server closed with error")
		if s.config.ShutdownOnError {
			shutdown()
		}
	}
}

func (s *httpServer) cleaner(ctx context.Context) {
	logger := ctrl.Logger(ctx)
	logger.Info().Msg("Shutting down...")
	if err := s.server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Error while shutting down")
	}
	logger.Info().Msg("Shutdown complete")
}

func requestLogger(next Handler) Handler {
	return HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		logger := ctrl.Logger(request.Context()).With().Str("request_id", rand.Text()).Logger()
		// log request and response
		logger.Info().Str("method", request.Method).Str("url", request.URL.String()).Msg("Request")
		start := time.Now()
		wrappedWriter := middleware.NewWrapResponseWriter(writer, request.ProtoMajor)
		defer func(start time.Time, wrappedWriter middleware.WrapResponseWriter) {
			duration := time.Since(start)
			logger.Info().Int("status", wrappedWriter.Status()).
				Int("bytes", wrappedWriter.BytesWritten()).
				Dur("duration", duration).
				Msg("Response")
		}(start, wrappedWriter)
		// recover any panic
		defer exception.Recover(func(recovered exception.Exception) {
			logger.Error().Any("recovered", recovered).Msg("Recovered from panic")
			// response with 500 Internal Server Error
			if request.Header.Get("Connection") != "Upgrade" {
				wrappedWriter.WriteHeader(http.StatusInternalServerError)
			}
		})
		// call the next handler
		next.ServeHTTP(wrappedWriter, request.WithContext(&logger))
	})
}

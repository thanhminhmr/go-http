/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/thanhminhmr/go-common/configuration"
	"github.com/thanhminhmr/go-exception"
	"go.uber.org/fx"
)

type ServerConfig struct {
	Port              uint16 `env:"HTTP_SERVER_PORT" validate:"required"`
	ReadHeaderTimeout uint32 `env:"HTTP_SERVER_READ_HEADER_TIMEOUT" validate:"min=0,max=60"`
	IdleTimeout       uint32 `env:"HTTP_SERVER_IDLE_TIMEOUT" validate:"min=0,max=3600"`
	MaxHeaderBytes    uint32 `env:"HTTP_SERVER_MAX_HEADER_BYTES" validate:"min=0,max=65536"`
	ShutdownOnError   bool   `env:"HTTP_SERVER_SHUTDOWN_ON_ERROR"`
}

func init() {
	configuration.SetDefault("HTTP_SERVER_READ_HEADER_TIMEOUT", "5")
	configuration.SetDefault("HTTP_SERVER_IDLE_TIMEOUT", "60")
	configuration.SetDefault("HTTP_SERVER_MAX_HEADER_BYTES", "4096")
	configuration.SetDefault("HTTP_SERVER_SHUTDOWN_ON_ERROR", "true")
}

func ifValue[Type any](condition bool, ifTrue, ifFalse Type) Type {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func NewServer(
	ctx context.Context,
	lifecycle fx.Lifecycle,
	shutdown fx.Shutdowner,
	config *ServerConfig,
) chi.Router {
	// create route
	router := chi.NewRouter()
	// create the http server
	server := httpServer{
		logger: zerolog.Ctx(ctx),
		router: router,
		server: http.Server{
			Addr:              fmt.Sprintf(":%d", config.Port),
			Handler:           router,
			ReadHeaderTimeout: time.Duration(config.ReadHeaderTimeout) * time.Second,
			IdleTimeout:       time.Duration(config.IdleTimeout) * time.Second,
			MaxHeaderBytes:    int(config.MaxHeaderBytes),
		},
		shutdown: ifValue(config.ShutdownOnError, shutdown, nil),
	}
	// set a sane default middleware stack
	router.Use(
		server.log,
		middleware.StripSlashes,
	)
	// add to lifecycle
	lifecycle.Append(fx.Hook{
		OnStart: server.onStart,
		OnStop:  server.onStop,
	})
	return router
}

type httpServer struct {
	logger   *zerolog.Logger
	router   *chi.Mux
	server   http.Server
	shutdown fx.Shutdowner
}

func (s *httpServer) onStart(context.Context) error {
	// dump all routes
	s.logger.Info().Msg("Listing all routes...")
	if err := chi.Walk(s.router, s.dumpRoutes); err != nil {
		s.logger.Error().Err(err).Msg("Error walking routes")
		return err
	}
	s.logger.Info().Msg("Listed all routes")
	// start the server
	go s.serve()
	return nil
}

func (s *httpServer) serve() {
	s.logger.Info().Str("addr", s.server.Addr).Msgf("Start serving")
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error().Err(err).Msg("Server closed with error")
		if s.shutdown != nil {
			if err := s.shutdown.Shutdown(); err != nil {
				s.logger.Error().Err(err).Msg("Error shutting down")
			}
		}
	}
}

func (s *httpServer) onStop(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down...")
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error().Err(err).Msg("Shutdown with error")
		return err
	}
	s.logger.Info().Msg("Shutdown complete")
	return nil
}

func (s *httpServer) dumpRoutes(
	method string,
	route string,
	handler http.Handler,
	middlewares ...func(http.Handler) http.Handler,
) error {
	s.logger.Info().
		Object("handler", funcObject(handler)).
		Array("middlewares", funcObjects(middlewares)).
		Msgf("Route: %s %s", method, route)
	return nil
}

func (s *httpServer) log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		logger := s.logger.With().Str("request_id", fmt.Sprintf("%016x", rand.Uint64())).Logger()
		// log request and response
		logger.Info().
			Str("method", request.Method).
			Stringer("url", request.URL).
			Msg("Request")
		start := time.Now()
		wrappedWriter := middleware.NewWrapResponseWriter(writer, request.ProtoMajor)
		defer func(start time.Time, wrappedWriter middleware.WrapResponseWriter) {
			duration := time.Since(start)
			logger.Info().
				Int("status", wrappedWriter.Status()).
				Int("bytes", wrappedWriter.BytesWritten()).
				Dur("duration", duration).
				Msg("Response")
		}(start, wrappedWriter)
		// recover any panic
		defer func() {
			if recovered := exception.Recover(recover()); recovered != nil {
				logger.Error().Err(recovered).Msg("Recovered from panic")
				// response with 500 Internal Server Error
				if request.Header.Get("Connection") != "Upgrade" {
					wrappedWriter.WriteHeader(http.StatusInternalServerError)
				}
			}
		}()
		// call the next handler
		next.ServeHTTP(wrappedWriter, request.WithContext(logger.WithContext(request.Context())))
	})
}

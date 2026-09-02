# go-httpserver

`github.com/thanhminhmr/go-httpserver` is a typed HTTP server layer built on the standard library `http.ServeMux`. It binds requests into Go structs, applies defaults and validation, provides composable middleware, and builds responses through a shared request `Context`.

The package keeps routing on the standard library instead of introducing a separate router API. See the package Godoc for the complete request-tag contract and API details.

## Example

```go
package main

import (
    "net/http"

    "github.com/rs/zerolog"
    "github.com/thanhminhmr/go-common/ctrl"
    "github.com/thanhminhmr/go-httpserver"
)

type GetUserRequest struct {
    ID      string `url:"id" validate:"required"`
    Verbose bool   `query:"verbose" default:"false"`
}

func main() {
    ctrl.Control(func(globalCtx context.Context) {
        ctrl.Register(ctrl.ShutdownOnSignal)

        config := &httpserver.ServerConfig{
            Port:              8080,
            ReadHeaderTimeout: 5,
            IdleTimeout:       60,
            MaxHeaderBytes:    4096,
            ShutdownOnError:   true,
        }

        router := httpserver.NewServer(config).WithLogger(zerolog.Ctx(globalCtx))
        router.Handle("GET /users/{id}", httpserver.RequestParser(
            func(ctx *httpserver.Context, request GetUserRequest) {
                ctx.NewResponse(http.StatusOK).JsonBody(map[string]any{
                    "id":      request.ID,
                    "verbose": request.Verbose,
                })
            },
        ))
    })
}

```

`NewServer` expects `ServerConfig` to have already had defaults applied and been validated. The example fills every field explicitly for that reason.

## Request binding

`RequestParser` and `MiddlewareParser` bind exported struct fields from `header`, `cookie`, `query`, ServeMux `url` wildcards, URL-encoded `form` bodies, `json` bodies, `multipart` bodies, or a raw `body`. `default` values are applied before binding and `validate` rules are checked afterward.

Use an empty source tag such as `query:""` or `header:""` when the handler needs the whole source. Form and JSON bodies are bounded and read with a timeout; multipart and raw bodies expose the live request stream and should be consumed by the handler that receives them.

The package Godoc is the authoritative reference for tag types, precedence, body media-type selection, JSON behavior, and error handling.

## Middleware and responses

`Router.Group` appends middleware without changing the parent router. Middleware calls `next` to continue, may return without calling `next` to short-circuit, and may inspect or replace the downstream response after `next` returns.

Handlers create responses with `Context.NewResponse`. The router writes the response only after the complete middleware and handler chain returns. If no response was created, the router returns 500 Internal Server Error.

## Go compatibility

ServeMux wildcard binding intentionally uses an unsafe mirror of unexported `net/http` request state to avoid reparsing `Request.Pattern` on every request. This makes the package sensitive to Go standard-library layout changes. When upgrading Go, the unsafe-layout regression test must remain passing.

## License

Mozilla Public License 2.0. See [`LICENSE.txt`](./LICENSE.txt).

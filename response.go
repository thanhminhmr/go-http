/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

type ServerResponse interface {
	Render(writer http.ResponseWriter) error
}

type ServerResponseFunc func(writer http.ResponseWriter) error

func (fn ServerResponseFunc) Render(writer http.ResponseWriter) error {
	return fn(writer)
}

type ServerErrorResponse struct {
	Status int
	Cause  error
}

func (e ServerErrorResponse) Render(writer http.ResponseWriter) error {
	header := writer.Header()
	header.Add("Content-Type", "text/plain; charset=utf-8")
	header.Add("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(e.Status)
	_, err := writer.Write([]byte(e.Cause.Error()))
	return err
}

func (e ServerErrorResponse) Error() string {
	return e.Cause.Error()
}

func (e ServerErrorResponse) MarshalZerologObject(event *zerolog.Event) {
	event.AnErr("cause", e.Cause).Int("Status", e.Status)
}

type ServerJsonResponse struct {
	Status   int
	Response any
}

func (r ServerJsonResponse) Render(writer http.ResponseWriter) error {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(r.Status)
	return json.NewEncoder(writer).Encode(r.Response)
}

type CookieServerResponse struct {
	Inner   ServerResponse
	Cookies []http.Cookie
}

func (c CookieServerResponse) Render(writer http.ResponseWriter) error {
	for _, cookie := range c.Cookies {
		if cookieString := cookie.String(); cookieString != "" {
			writer.Header().Add("Set-Cookie", cookieString)
		}
	}
	return c.Inner.Render(writer)
}

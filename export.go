/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import "net/http"

type Cookie = http.Cookie
type Handler = http.Handler
type HandlerFunc = http.HandlerFunc
type Header = http.Header
type Request = http.Request
type ResponseWriter = http.ResponseWriter

type KeyValue = map[string]string
type KeyValues = map[string][]string

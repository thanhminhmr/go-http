/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import "net/http"

// Middleware is a function that wraps an [http.Handler] to intercept or modify
// the handling of an HTTP request.
type Middleware = func(http.Handler) http.Handler

// KeyValue maps a string key to a single string value.
type KeyValue = map[string]string

// KeyValues maps a string key to a list of string values.
type KeyValues = map[string][]string

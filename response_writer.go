/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import "net/http"

// responseTracker wraps an [http.ResponseWriter] to record the first final
// Status code (2xx-5xx) and to count the bytes BytesWritten. All writes are delegated
// to the underlying ResponseWriter.
//
// Informational responses (1xx) are forwarded but do not commit a final Status.
// The first call to [http.ResponseWriter.Write] that occurs without a prior
// final [http.ResponseWriter.WriteHeader] call is treated as Status 200,
// matching the behavior of [net/http].
type responseTracker struct {
	http.ResponseWriter

	// Status returns the recorded status code, or 0 if [WriteHeader] has not been
	// called and no body has been written yet.
	Status int

	// BytesWritten returns the total number of body bytes written so far.
	BytesWritten int
}

func (t *responseTracker) Header() http.Header { return t.ResponseWriter.Header() }

func (t *responseTracker) WriteHeader(status int) {
	// Informational responses (1xx) do not commit a final status.
	if status >= 200 {
		// Preserve the first committed final status (2xx-5xx).
		if t.Status == 0 {
			t.Status = status
		}
	}
	t.ResponseWriter.WriteHeader(status)
}

func (t *responseTracker) Write(b []byte) (int, error) {
	if t.Status == 0 {
		t.Status = http.StatusOK
	}
	n, err := t.ResponseWriter.Write(b)
	t.BytesWritten += n
	return n, err
}

// Unwrap exposes the underlying [http.ResponseWriter] so callers can access
// additional interfaces via [http.ResponseController].
func (t *responseTracker) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

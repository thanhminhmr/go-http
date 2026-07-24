/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"testing"
)

// AnonInt is an exported non-struct type used to test that anonymous non-struct
// fields are rejected at registration.
type AnonInt int

// ============ createTags: non-struct request type ============

func TestPanic_NonStructRequestType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-struct request type, got none")
		}
	}()
	_ = RequestParser(func(ctx Context, _ int) *Response {
		return ctx.Response(http.StatusOK)
	})
}

// ============ checkRecursively: anonymous non-struct field ============

func TestPanic_AnonymousNonStructField(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for anonymous non-struct field, got none")
		}
	}()
	type Req struct {
		AnonInt
	}
	_ = RequestParser(captureHandler[Req])
}

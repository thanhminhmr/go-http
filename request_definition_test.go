/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for registration-time panics: non-struct request types and anonymous
// non-struct fields are rejected by createTags/checkRecursively.

// AnonInt is an exported non-struct type used to test that anonymous non-struct
// fields are rejected at registration.
type AnonInt int

// ============ createTags: non-struct request type ============

func TestPanic_NonStructRequestType(t *testing.T) {
	require.Panics(t, func() {
		_ = RequestParser(func(ctx *Context, _ int) { ctx.NewResponse(http.StatusOK) })
	})
}

// ============ checkRecursively: anonymous non-struct field ============

func TestPanic_AnonymousNonStructField(t *testing.T) {
	type Req struct{ AnonInt }
	require.Panics(t, func() { _ = RequestParser(captureHandler[Req]) })
}

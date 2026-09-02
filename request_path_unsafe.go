/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"unsafe"
)

// The types below mirror unexported net/http ServeMux/request state and are
// accessed through unsafe.Pointer. Their layout is coupled to the supported Go
// standard library. TestHTTPRequestUnsafeLayout is the guardrail: a Go upgrade
// must not be accepted until that test confirms these mirrors still match.

// httpSegment mirrors the unexported net/http ServeMux pattern segment used by
// getPathValues.
type httpSegment struct {
	s     string
	wild  bool
	multi bool
}

// httpPattern mirrors the unexported net/http ServeMux pattern representation
// needed to associate wildcard names with request matches.
type httpPattern struct {
	str      string
	method   string
	host     string
	segments []httpSegment
	loc      string
}

// httpRequest overlays the portion of http.Request needed to reach the matched
// ServeMux pattern and wildcard values.
//
// The leading _ [33]uintptr field is intentional, layout-dependent padding: it
// skips the 33 pointer-sized machine words that precede Request.pat in the Go
// 1.22–1.26 standard library's net/http.Request struct layout. The unsafe
// mirror only needs pat and matches, so the padding is sized to land on pat's
// offset.
//
// This constant is fragile: any change to net/http.Request's field set, order,
// or sizing — which the Go compatibility promise does NOT protect for
// unexported fields — will silently misalign this mirror. TestHTTPRequestUnsafeLayout
// in request_path_unsafe_test.go is the sole guardrail: it walks
// reflect.TypeFor[http.Request]() and asserts that the offsets/sizes of pat,
// matches, and the mirrored pattern/segment structs still match. A Go upgrade
// that breaks that test requires recomputing the [33]uintptr constant by
// inspecting the current net/http.Request layout (e.g. via go doc -src
// net/http.Request or by printing unsafe.Offsetof).
type httpRequest struct {
	_ [33]uintptr

	pat     *httpPattern // the pattern that matched
	matches []string     // values for the matching wildcards in pat
}

// getPathValues returns all named wildcard values captured by the ServeMux
// pattern that matched r. It returns nil when no matched pattern is available.
func getPathValues(r *http.Request) KeyValue {
	request := (*httpRequest)(unsafe.Pointer(r))
	if request.pat == nil {
		return nil
	}
	keyValue := make(KeyValue, len(request.pat.segments))
	matchIndex := 0
	for _, segment := range request.pat.segments {
		if segment.wild && segment.s != "" {
			keyValue[segment.s] = request.matches[matchIndex]
			matchIndex++
		}
	}
	return keyValue
}

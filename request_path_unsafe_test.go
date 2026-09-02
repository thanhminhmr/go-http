/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPRequestUnsafeLayout(t *testing.T) {
	actualRequest := reflect.TypeFor[http.Request]()
	mirrorRequest := reflect.TypeFor[httpRequest]()

	// These are the Request fields we actually dereference through
	// the unsafe mirror.
	for _, name := range []string{"pat", "matches"} {
		actualField, ok := actualRequest.FieldByName(name)
		if !ok {
			t.Fatalf("net/http.Request no longer has field %q", name)
		}

		mirrorField, ok := mirrorRequest.FieldByName(name)
		if !ok {
			t.Fatalf("httpRequest mirror no longer has field %q", name)
		}

		if actualField.Offset != mirrorField.Offset {
			t.Fatalf(
				"http.Request.%s offset changed: net/http=%d mirror=%d",
				name,
				actualField.Offset,
				mirrorField.Offset,
			)
		}

		if actualField.Type.Size() != mirrorField.Type.Size() ||
			actualField.Type.Align() != mirrorField.Type.Align() {
			t.Fatalf(
				"http.Request.%s layout changed: "+
					"net/http type=%v size=%d align=%d; "+
					"mirror type=%v size=%d align=%d",
				name,
				actualField.Type,
				actualField.Type.Size(),
				actualField.Type.Align(),
				mirrorField.Type,
				mirrorField.Type.Size(),
				mirrorField.Type.Align(),
			)
		}
	}

	// Verify the private *http.pattern that Request.pat points to.
	patField, _ := actualRequest.FieldByName("pat")
	actualPattern := patField.Type.Elem()

	assertStructLayout(
		t,
		"net/http.pattern",
		actualPattern,
		reflect.TypeFor[httpPattern](),
	)

	// Verify the private http.segment layout too.
	segmentsField, ok := actualPattern.FieldByName("segments")
	if !ok {
		t.Fatal("net/http.pattern no longer has field \"segments\"")
	}

	if segmentsField.Type.Kind() != reflect.Slice {
		t.Fatalf(
			"net/http.pattern.segments is no longer a slice: %v",
			segmentsField.Type,
		)
	}

	assertStructLayout(
		t,
		"net/http.segment",
		segmentsField.Type.Elem(),
		reflect.TypeFor[httpSegment](),
	)
}

func assertStructLayout(
	t *testing.T,
	name string,
	actual, mirror reflect.Type,
) {
	t.Helper()

	if actual.Kind() != reflect.Struct ||
		mirror.Kind() != reflect.Struct {
		t.Fatalf(
			"%s: expected structs: actual=%v mirror=%v",
			name,
			actual,
			mirror,
		)
	}

	if actual.Size() != mirror.Size() ||
		actual.Align() != mirror.Align() {
		t.Fatalf(
			"%s size/alignment changed: "+
				"actual size=%d align=%d; "+
				"mirror size=%d align=%d",
			name,
			actual.Size(),
			actual.Align(),
			mirror.Size(),
			mirror.Align(),
		)
	}

	if actual.NumField() != mirror.NumField() {
		t.Fatalf(
			"%s field count changed: actual=%d mirror=%d",
			name,
			actual.NumField(),
			mirror.NumField(),
		)
	}

	for i := 0; i < actual.NumField(); i++ {
		af := actual.Field(i)
		mf := mirror.Field(i)

		if af.Name != mf.Name {
			t.Fatalf(
				"%s field %d changed: actual=%q mirror=%q",
				name,
				i,
				af.Name,
				mf.Name,
			)
		}

		if af.Offset != mf.Offset {
			t.Fatalf(
				"%s.%s offset changed: actual=%d mirror=%d",
				name,
				af.Name,
				af.Offset,
				mf.Offset,
			)
		}

		// Exact Type equality won't work for unexported
		// net/http types such as http.segment, so compare
		// the properties that matter for unsafe access.
		if af.Type.Kind() != mf.Type.Kind() ||
			af.Type.Size() != mf.Type.Size() ||
			af.Type.Align() != mf.Type.Align() {
			t.Fatalf(
				"%s.%s type layout changed: actual=%v mirror=%v",
				name,
				af.Name,
				af.Type,
				mf.Type,
			)
		}
	}
}

// TestGetPathValues_UnmatchedRequest_ReturnsNil exercises the
// `request.pat == nil` branch in [getPathValues]: a request that has not been
// dispatched through a [http.ServeMux] has no matched pattern, so the result
// must be nil.
func TestGetPathValues_UnmatchedRequest_ReturnsNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	values := getPathValues(req)
	assert.Nil(t, values)
}

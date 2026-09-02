/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeResponseWriter is a purpose-built [http.ResponseWriter] for tests that
// need to observe every [http.ResponseWriter.WriteHeader] and
// [http.ResponseWriter.Write] call, including informational (1xx) responses.
// It does NOT depend on [httptest.ResponseRecorder], whose simplified header
// behavior is inadequate for verifying 1xx semantics.
type fakeResponseWriter struct {
	header   http.Header
	statuses []int
	writes   [][]byte
}

func newFakeResponseWriter() *fakeResponseWriter {
	return &fakeResponseWriter{header: make(http.Header)}
}

func (f *fakeResponseWriter) Header() http.Header {
	return f.header
}

func (f *fakeResponseWriter) WriteHeader(status int) {
	f.statuses = append(f.statuses, status)
}

func (f *fakeResponseWriter) Write(b []byte) (int, error) {
	f.writes = append(f.writes, b)
	return len(b), nil
}

func TestResponseWriterTracker_InitialState(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	assert.Equal(t, 0, tracker.Status)
	assert.Equal(t, 0, tracker.BytesWritten)
	assert.Same(t, fake, tracker.Unwrap())
}

func TestResponseWriterTracker_FirstWriteImplicit200(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	n, err := tracker.Write([]byte("hello"))

	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, http.StatusOK, tracker.Status)
	assert.Equal(t, 5, tracker.BytesWritten)
}

func TestResponseWriterTracker_MultipleWritesAccumulate(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	n1, _ := tracker.Write([]byte("foo"))
	n2, _ := tracker.Write([]byte("barbaz"))

	assert.Equal(t, 3, n1)
	assert.Equal(t, 6, n2)
	assert.Equal(t, 9, tracker.BytesWritten)
}

func TestResponseWriterTracker_RepeatedFinalStatus_PreservesFirst(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	tracker.WriteHeader(http.StatusOK)
	tracker.WriteHeader(http.StatusNotFound)

	// First committed 2xx-5xx status wins, matching net/http semantics.
	assert.Equal(t, http.StatusOK, tracker.Status)
	// Both calls are forwarded to the underlying writer.
	assert.Equal(t, []int{http.StatusOK, http.StatusNotFound}, fake.statuses)
}

func TestResponseWriterTracker_InformationalThenFinal_TracksFinal(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	tracker.WriteHeader(http.StatusEarlyHints) // 103
	tracker.WriteHeader(http.StatusOK)         // 200

	// 1xx is informational; the subsequent 200 is the first final status.
	assert.Equal(t, http.StatusOK, tracker.Status)
	assert.Equal(t, []int{http.StatusEarlyHints, http.StatusOK}, fake.statuses)
}

func TestResponseWriterTracker_MultipleInformationalThenBodyWrite_Tracks200(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	tracker.WriteHeader(http.StatusContinue)   // 100
	tracker.WriteHeader(http.StatusEarlyHints) // 103
	_, _ = tracker.Write([]byte("body"))

	// Multiple 1xx responses don't commit; first body write implies 200.
	assert.Equal(t, http.StatusOK, tracker.Status)
	assert.Equal(t, 4, tracker.BytesWritten)
}

func TestResponseWriterTracker_InformationalThenFinalError_TracksError(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	tracker.WriteHeader(http.StatusEarlyHints)          // 103
	tracker.WriteHeader(http.StatusInternalServerError) // 500

	// 1xx is informational; the subsequent 500 is the first final status.
	assert.Equal(t, http.StatusInternalServerError, tracker.Status)
}

func TestResponseWriterTracker_InformationalDoesNotCommitFinal(t *testing.T) {
	fake := newFakeResponseWriter()
	tracker := &responseTracker{ResponseWriter: fake}

	tracker.WriteHeader(http.StatusEarlyHints) // 103

	// 1xx is informational, not a final commit; status stays 0.
	// This is critical for panic recovery: server.go checks
	// wrappedWriter.Status == 0 to decide whether to write a 500.
	assert.Equal(t, 0, tracker.Status)
}

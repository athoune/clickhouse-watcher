// Package transport provides in-memory HTTP routing for standalone mode.
package transport

import (
	"net/http"
	"net/http/httptest"
)

// MemoryTransport implements http.RoundTripper by routing requests directly
// to an http.Handler without network I/O or Unix sockets.
// This is used in standalone mode where the daemon and client run in the
// same process.
type MemoryTransport struct {
	Handler http.Handler
}

// RoundTrip executes a single HTTP transaction by calling ServeHTTP on the
// wrapped handler and capturing the response.
func (t *MemoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.Handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

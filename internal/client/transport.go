package client

import (
	"net/http"
	"sync/atomic"
)

type transportValue struct{ transport http.RoundTripper }

var defaultTransport atomic.Pointer[transportValue]

// SetDefaultTransport sets the transport captured by subsequent New calls.
// A nil transport restores the standard net/http default.
func SetDefaultTransport(transport http.RoundTripper) {
	if transport == nil {
		defaultTransport.Store(nil)
		return
	}
	defaultTransport.Store(&transportValue{transport: transport})
}

func configuredTransport() http.RoundTripper {
	if current := defaultTransport.Load(); current != nil {
		return current.transport
	}
	return nil
}

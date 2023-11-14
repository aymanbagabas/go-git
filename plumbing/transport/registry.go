package transport

import (
	"fmt"
	"sync"
)

var (
	// registry contains the protocols registered.
	registry = map[string]Transport{}

	mtx sync.RWMutex
)

// Register adds or modifies an existing protocol in the registry.
func Register(scheme string, c Transport) {
	mtx.Lock()
	defer mtx.Unlock()
	registry[scheme] = c
}

// Unregister removes a protocol from the registry.
func Unregister(scheme string) {
	mtx.Lock()
	defer mtx.Unlock()
	delete(registry, scheme)
}

// Get returns the protocol registered with the given scheme.
// It returns nil if no protocol is registered.
func Get(scheme string) (Transport, bool) {
	mtx.RLock()
	defer mtx.RUnlock()
	t, ok := registry[scheme]
	return t, ok
}

// NewClient returns the appropriate client among of the set of known protocols:
// http://, https://, ssh:// and file://.
// See `InstallProtocol` to add or modify protocols.
func NewClient(endpoint *Endpoint) (Transport, error) {
	return getTransport(endpoint)
}

func getTransport(endpoint *Endpoint) (Transport, error) {
	f, ok := registry[endpoint.Protocol]
	if !ok {
		return nil, fmt.Errorf("unsupported scheme %q", endpoint.Protocol)
	}

	if f == nil {
		return nil, fmt.Errorf("malformed client for scheme %q, client is defined as nil", endpoint.Protocol)
	}
	return f, nil
}

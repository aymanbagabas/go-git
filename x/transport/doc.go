// Package transport implements the redesigned transport API for go-git.
//
// The new API separates transport capabilities from Git protocol adapters.
// It introduces a universal Transport.Open entrypoint, an optional
// Connectable capability for full-duplex transports, and an immutable
// Client with built-in transport factory registration.
//
// Git pack protocol operations (fetch, push, archive) and Git LFS operations
// are implemented as adapters on top of these transport primitives rather
// than being encoded directly into the transport interface.
package transport

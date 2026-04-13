package sideband

import "io"

// Type sideband type "side-band" or "side-band-64k"
type Type int8

const (
	// Sideband legacy sideband type up to 1000-byte messages
	Sideband Type = iota
	// Sideband64k sideband type up to 65519-byte messages
	Sideband64k Type = iota

	// MaxPackedSize for Sideband type
	MaxPackedSize = 1000
	// MaxPackedSize64k for Sideband64k type
	MaxPackedSize64k = 65520
)

// Channel sideband channel
type Channel byte

// WithPayload encode the payload as a message
func (ch Channel) WithPayload(payload []byte) []byte {
	return append([]byte{byte(ch)}, payload...)
}

const (
	// PackData packfile content
	PackData Channel = 1
	// ProgressMessage progress messages
	ProgressMessage Channel = 2
	// ErrorMessage fatal error message just before stream aborts
	ErrorMessage Channel = 3
)

// channelWriter is a writer that writes to a specific sideband channel.
type channelWriter struct {
	m  *Muxer
	ch Channel
}

var _ io.Writer = (*channelWriter)(nil)

// Write implements [io.Writer] for channelWriter.
func (w *channelWriter) Write(p []byte) (int, error) {
	return w.m.WriteChannel(w.ch, p)
}

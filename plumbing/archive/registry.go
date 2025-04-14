package archive

import "sync"

var (
	registry = map[string]Archiver{}
	mu 	 sync.RWMutex
)

// RegisterFormat registers a new archive format.
// It takes the archive extension and the [Archiver] implementation.
func RegisterFormat(ext string, a Archiver) {
	mu.Lock()
	registry[ext] = a
	mu.Unlock()
}

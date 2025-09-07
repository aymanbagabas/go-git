package transport

import "strings"

// Service represents a Git transport service.
// All services are prefixed with "git-".
type Service interface {
	// Command returns a new [Cmd] that represents the [Service] to be executed.
	Command(args ...string) *Cmd
	// String returns the string representation of the service.
	String() string
	// Name returns the name of the service without the "git-" prefix.
	Name() string
}

// GitService implements the [Service] interface for traditional Git services.
type GitService string

// Command returns a new [Cmd] that represents the [Service] to be executed.
func (s GitService) Command(args ...string) *Cmd {
	return Command(s.String(), args...)
}

// String returns the string representation of the service.
func (s GitService) String() string {
	return string(s)
}

// Name returns the name of the service without the "git-" prefix.
func (s GitService) Name() string {
	return strings.TrimPrefix(string(s), "git-")
}

// Git service names.
const (
	UploadPackService    GitService = "git-upload-pack"
	UploadArchiveService GitService = "git-upload-archive"
	ReceivePackService   GitService = "git-receive-pack"
)

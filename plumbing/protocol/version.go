package protocol

import "strconv"

// Version is the version of the git protocol.
type Version uint8

// Supported versions of the git protocol.
const (
	VersionV0 Version = iota
	VersionV1
	VersionV2
)

// String returns the string representation of the version.
func (v Version) String() string {
	return "version=" + strconv.Itoa(int(v))
}

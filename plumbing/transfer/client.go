package transfer

// Client is a Git client that can fetch and push to a Git server.
type Client struct{}

// type Client interface {
// 	// Close closes the connection.
// 	Close() error
//
// 	// Capabilities returns the list of capabilities supported by the server.
// 	Capabilities() *capability.List
//
// 	// Version returns the Git protocol version the server supports.
// 	Version() protocol.Version
//
// 	// GetRemoteRefs returns the references advertised by the remote.
// 	// Using protocol v0 or v1, this returns the references advertised by the
// 	// remote during the handshake. Using protocol v2, this runs the ls-refs
// 	// command on the remote.
// 	// This will error if the session is not already established using
// 	// Handshake.
// 	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
//
// 	// Fetch sends a fetch-pack request to the server.
// 	Fetch(ctx context.Context, req *FetchRequest) error
//
// 	// Push sends a send-pack request to the server.
// 	Push(ctx context.Context, req *PushRequest) error
// }

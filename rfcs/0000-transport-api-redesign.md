# RFC 0000: Transport API redesign

## Summary

This RFC describes go-git's redesigned transport API, implemented in `x/transport`. The design separates transport capabilities from Git protocol operations through a small set of transport-neutral primitives layered with pack-protocol-aware interfaces. The key types are a lean `Request` carrying only request semantics, a `Conn` type for raw full-duplex transport exchanges, an optional `Connectable` capability for transports that can open such exchanges, a `Transport` interface that performs pack protocol handshakes, a `Session` representing a connected Git pack protocol session, and a `Client` that resolves URL schemes to transport implementations. Transport-specific authentication replaces the old shared `AuthMethod` interface. Git pack operations (fetch, push) are methods on `Session` rather than on a separate adapter layer.

This redesign is intentionally breaking. Backward compatibility with `plumbing/transport` is not a constraint.

## Motivation

The current transport API in `plumbing/transport` is effective for classic Git pack operations, but it mixes together concerns that would benefit from clearer separation.

First, go-git's transport abstraction is currently pack-protocol-centric. The stable API is shaped around creating sessions for `git-upload-pack` and `git-receive-pack`. This makes the API awkward for transport use cases that are not themselves pack protocol operations.

Second, not all transports have the same capabilities. SSH, Git TCP, and file transports are naturally command-oriented and can support opening a raw stream for an arbitrary remote command. HTTP is fundamentally different: it is stateless and half-duplex, and should not pretend to support a reusable full-duplex stream. The transport API should model these differences directly instead of forcing all transports into one shape.

Third, there is growing demand for higher-level protocols layered on top of the same transport mechanisms. Examples include `git-upload-archive`, `git-lfs-authenticate` over SSH, `git-lfs-transfer` over SSH, and Git LFS Batch and basic transfer APIs over HTTP. These are not all pack protocol operations, but they reuse the same notions of URL selection, authentication, dialing, proxying, TLS policy, and command execution.

Fourth, users need a better extension story. The project already supports replacing built-in transport implementations, but the API surface is not yet ideal for implementing custom transports, remote-helper-like adapters, or application-specific protocols in a clean and reusable way.

The goals of this redesign are therefore:

- make the transport API composable and capability-aware
- preserve a universal pack protocol entrypoint that works for all built-in transports
- expose raw stream transport capabilities when those capabilities genuinely exist
- provide a clean foundation for SSH-based and HTTP-based Git LFS adapters
- support transport-specific authentication rather than a vague shared interface
- preserve the ability to register or replace built-in transports through a uniform factory model
- keep the public request shape small and avoid mixing transport policy into request semantics

## Detailed design

### Design principles

The proposed design follows these principles:

1. **Separate transport capabilities from each other.** Stream transports can open raw connections; HTTP cannot. The API models this directly.
2. **Make full-duplex streaming explicit.** Transports that can open a `Conn` implement `Connectable`; those that cannot do not.
3. **Keep the pack protocol interface small.** All built-in transports implement `Transport.Handshake`, returning a `Session` with pack capabilities.
4. **Use transport-specific authentication.** HTTP and SSH authenticate differently. The public API models that directly.
5. **Keep requests transport-neutral.** Git-specific conventions such as repository-as-first-argument are handled by transport implementations, not imposed on callers.
6. **Prefer standard library configuration types and patterns where they fit.**

### Implemented API surface

The design centers around six main concepts:

- `Request`: describes the operation to open
- `Conn`: a transport-neutral connection with read, write, and close
- `Connectable`: optional capability for raw full-duplex connections
- `Transport`: universal pack protocol interface implemented by all transports
- `Session`: a connected Git pack protocol session
- `Client`: URL scheme resolution and transport dispatch

### Request

`Request` is intentionally lean. It carries request semantics only.

```go
type Request struct {
    URL *url.URL

    Command  string
    Args     []string

    Protocol protocol.Version
}
```

Notes:

- `URL` is the target remote URL.
- `Command` is a transport-neutral command name such as `git-upload-pack`, `git-receive-pack`, `git-upload-archive`, `git-lfs-authenticate`, or `git-lfs-transfer`.
- `Args` carries additional arguments appended after the command and repository path on the wire.
- `Protocol` communicates Git protocol version preference where relevant.
- The repository path is **not** a field on `Request`. Transport implementations derive it from `URL.Path`, matching how canonical Git handles the relationship for all transport protocols.

### Conn

`Conn` is the transport-neutral connection type. It represents an open transport exchange with independent read, write, and close operations.

```go
type Conn interface {
    io.Closer
    Reader() io.Reader
    Writer() io.WriteCloser
}
```

`Writer().Close()` signals the end of writing (half-close) without closing the read side. `Close()` releases all resources.

For stream transports (SSH, Git TCP, file), `Reader()` and `Writer()` refer to the remote command's stdout and stdin respectively.

For HTTP, `Conn` models a single request/response exchange where `Writer()` buffers the request body and closing it triggers the HTTP round-trip.

### Connectable

`Connectable` is an optional capability implemented only by transports that can truly open a raw full-duplex connection.

```go
type Connectable interface {
    Connect(context.Context, *Request) (Conn, error)
}
```

This interface is implemented by:

- SSH transports
- Git TCP transports
- file transports

It is explicitly **not** implemented by HTTP transports. When code requires `Connect` on an HTTP transport, it receives `ErrConnectUnsupported`.

`Connectable` is valuable because it exposes a real capability boundary. Use `Connect` for non-pack protocols like `git-upload-archive`, `git-lfs-authenticate`, `git-lfs-transfer`, or custom commands.

### Transport

`Transport` is the universal pack protocol interface.

```go
type Transport interface {
    Handshake(ctx context.Context, req *Request) (Session, error)
}
```

All built-in transports implement `Transport`. `Handshake` opens a connection to the remote, performs the Git pack protocol handshake (reading advertised refs and capabilities), and returns a `Session`.

For stream transports, `Handshake` opens a `Conn` internally, sends the initial pack protocol request, reads the advertised refs, and wraps the result in a `Session`.

For HTTP, `Handshake` performs the `/info/refs?service=...` discovery request, detects smart vs dumb HTTP, parses the advertised refs, and returns an HTTP-backed `Session`.

### Session

`Session` is returned by `Transport.Handshake`. It represents a connected Git pack protocol session.

```go
type Session interface {
    Capabilities() *capability.List
    GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
    Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
    Push(ctx context.Context, st storage.Storer, req *PushRequest) error
    Close() error
}
```

`Session` is pack-protocol-aware by design. The implementation collapsed the RFC's original two-layer model (transport-neutral `Session` + separate `PackSession` adapter) into a single interface because all current transports speak the Git pack protocol. The transport-neutral exchange primitive is `Conn`.

`Fetch` and `Push` accept a `storage.Storer` so pack operations can read/write objects directly. This moves the storer dependency out of session construction and into the operations that actually need it.

### Client

The `Client` in `x/client` resolves URL schemes to transport implementations.

```go
type Options struct {
    File file.Options
    Git  xgit.Options
    SSH  xssh.Options
    HTTP xhttp.Options
}

type Client struct { ... }

func New(opts Options) *Client
func (c *Client) Handshake(ctx context.Context, req *Request) (Session, error)
func (c *Client) Connect(ctx context.Context, req *Request) (Conn, error)
func (c *Client) Transport(scheme string) (Transport, error)
func (c *Client) RegisterTransport(scheme string, tr Transport)
func (c *Client) Close() error
```

`New` creates a client that lazily constructs built-in transports for `file`, `git`, `ssh`, `http`, and `https` schemes from the provided `Options`.

`RegisterTransport` allows overriding the transport for any scheme, including built-in ones.

`Handshake` resolves the transport for the request URL scheme and performs a pack protocol handshake.

`Connect` resolves the transport and calls `Connectable.Connect`. Returns `ErrConnectUnsupported` if the transport does not implement `Connectable`.

`Close` releases resources held by the client.

### Authentication model

The redesign does **not** keep a single transport-neutral `AuthMethod` interface.

HTTP and SSH authentication are fundamentally different:

- HTTP authentication mutates or enriches an `*http.Request`
- SSH authentication produces or influences an `*ssh.ClientConfig`

Those are not two implementations of one meaningful core behavior.

#### HTTP authentication

```go
type Options struct {
    Client     *http.Client
    Authorizer func(*http.Request) error
    HTTPProxy  func(*http.Request) (*url.URL, error)
}
```

- `Client` is the underlying HTTP client. TLS configuration should be set on its `Transport`.
- `Authorizer` mutates outgoing requests to add auth headers. Basic auth, bearer auth, token headers, and LFS authorization all fit naturally.
- `HTTPProxy` returns the proxy URL for a given request. Defaults to `http.ProxyFromEnvironment` when no custom `Client` is provided.

#### SSH authentication

```go
type Options struct {
    ClientConfig func(context.Context, *Request) (*ssh.ClientConfig, error)
}
```

SSH auth is expressed as a provider of `*ssh.ClientConfig`, giving full control over auth methods, username, host key policy, algorithms, and agent usage.

### Built-in transport behavior

#### SSH

SSH implements both `Transport` and `Connectable`.

- `Connect` opens an SSH channel and executes the requested command
- `Handshake` connects, reads advertised refs and capabilities, returns a `Session`
- Command execution is derived from `Request.Command` and `URL.Path`
- Protocol negotiation hints (`GIT_PROTOCOL`) are derived from `Request.Protocol`
- Authentication comes from `Options.ClientConfig`

#### Git TCP

Git TCP implements both `Transport` and `Connectable`.

- `Connect` opens the raw TCP stream and sends the Git protocol request line
- `Handshake` connects, reads advertised refs, returns a `Session`
- Request encoding uses `Request.Command`, `URL.Path`, and `Request.Protocol`

#### File

File implements both `Transport` and `Connectable`.

- `Connect` spawns the local Git command and wraps its stdio pipes
- `Handshake` connects, reads advertised refs, returns a `Session`
- Supports serving from in-process `storage.Storer` via `MapLoader`

#### HTTP

HTTP implements `Transport` but not `Connectable`.

- `Handshake` performs GET `/info/refs?service=...`, detects smart vs dumb HTTP, parses refs and capabilities, returns a `Session`
- Smart HTTP `Fetch`/`Push` use buffered POST requests to the service endpoint
- Dumb HTTP `Fetch` walks object graph by fetching individual objects and pack files
- Dumb HTTP does not support `Push`
- Authorization uses `Options.Authorizer`
- Proxy behavior uses `Options.HTTPProxy` or the HTTP client's transport

### Service constants

```go
const (
    UploadPackService    = "git-upload-pack"
    UploadArchiveService = "git-upload-archive"
    ReceivePackService   = "git-receive-pack"
)
```

### Errors

The transport layer defines typed errors for capability and protocol boundaries:

```go
var (
    ErrConnectUnsupported  = errors.New("transport does not support raw connections")
    ErrCommandUnsupported  = errors.New("command is not supported by transport")
    ErrProtocolUnsupported = errors.New("protocol version is not supported")
    ErrRepositoryNotFound  = errors.New("repository not found")
    ErrEmptyRemoteRepository = errors.New("remote repository is empty")
    ErrAuthenticationRequired = errors.New("authentication required")
    ErrAuthorizationFailed = errors.New("authorization failed")
    // ...
)
```

HTTP errors include structured `Err` with status code, URL, and reason.

### Breaking changes

This redesign is intentionally breaking. The old `plumbing/transport` API (`NewSession`, `SupportedProtocols`, `AuthMethod`, `Endpoint`, `ProxyOptions`) is not preserved. The goal is a cleaner API that go-git should have.

### Non-goals

This RFC does not attempt to:

- fully specify the final Git LFS public API
- standardize every possible custom command protocol
- define transport multiplexing or long-lived pooled sessions
- define all timeout, retry, tracing, and observability hooks

Those concerns can be addressed in follow-up RFCs or implementation PRs once the core transport model is in place.

## Drawbacks

The collapsing of the pack adapter layer into `Transport.Handshake`/`Session` means the pack protocol is more tightly coupled to transport implementations than the original RFC envisioned. If a future transport needs to speak a non-pack protocol through `Handshake`, the interface would need to change. However, `Connectable.Connect` provides the escape hatch for non-pack use cases.

HTTP-backed sessions have different internal lifecycle from stream-backed sessions. Smart HTTP buffers writes and fires a POST, while stream transports have a persistent bidirectional connection. This difference is hidden behind the same `Session` interface, which works well for pack operations but callers should be aware of the underlying behavior.

## Rationale and alternatives

### Why this design

- `Connectable` exists only where a real full-duplex stream exists
- `Transport.Handshake` provides the universal pack protocol entrypoint
- `Session` captures pack protocol semantics directly, avoiding an unnecessary adapter layer
- `Conn` provides the transport-neutral exchange primitive for non-pack use cases
- Authentication is modeled transport-specifically instead of through a vague shared interface
- The `Client` provides scheme resolution and transport dispatch without complex factory configuration

### Alternative: Keep pack semantics in a separate adapter layer

The original RFC proposed `Transport.Open → Session` (transport-neutral) with a separate `PackClient.Handshake → PackSession` (pack-aware) adapter layer.

This was simplified during implementation because all current transports speak the Git pack protocol. The two-layer separation added conceptual overhead without practical benefit at this stage. If non-pack transports are added later, the `Connectable.Connect` path already handles that case.

### Alternative: Make all transports implement only `Connectable`

This would force HTTP to pretend to be a raw stream transport.

Rejected because HTTP is not a full-duplex reusable command transport, and the API should not misrepresent that.

### Alternative: Keep a universal `AuthMethod` interface

This would preserve the old conceptual model that one auth abstraction could span HTTP and SSH.

Rejected because HTTP request authorization and SSH client configuration are fundamentally different operations.

## Implementation status

The following packages are implemented and fully tested:

| Package | Description | Tests |
|---------|-------------|-------|
| `x/transport` | Core types: `Request`, `Conn`, `Connectable`, `Transport`, `Session`, errors, pack negotiation, fetch/push logic | ✓ |
| `x/transport/ssh` | SSH transport (both `Transport` and `Connectable`) | 15 upload-pack + 14 receive-pack + 3 unit |
| `x/transport/http` | HTTP transport (`Transport` only, smart + dumb) | 15 upload-pack + 14 receive-pack |
| `x/transport/git` | Git TCP transport (both `Transport` and `Connectable`) | ✓ |
| `x/transport/file` | File transport (both `Transport` and `Connectable`) | ✓ |
| `x/transport/test` | Shared test suites (`UploadPackSuite`, `ReceivePackSuite`) | — |
| `x/client` | URL scheme resolution and transport dispatch | 5 unit tests |

## Open implementation questions

- Whether `Client` should become immutable after construction (functional options) or remain mutable via `RegisterTransport`
- How much of `remote.go` and related callers should be rewired to the new API
- Whether a package-level default client facade is useful
- Whether `DialOptions` and `ProxyOptions` should be promoted to `Client` configuration
- Whether per-call overrides (`CallOptions`) are needed for auth, dial, and proxy
- How `git-upload-archive` should be exposed (likely via `Connectable.Connect`)
- Whether existing `plumbing/transport` endpoint parsing helpers should be preserved

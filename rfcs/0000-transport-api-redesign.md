# RFC 0000: Transport API redesign

## Summary

This RFC proposes redesigning go-git's transport API around a small set of transport-neutral primitives that clearly separate transport capabilities from Git protocol adapters. The new design introduces a universal `Transport.Open` entrypoint that all transports implement, a lower-level `Connectable` capability for transports that can open a raw full-duplex `io.ReadWriteCloser`, an immutable generic `Client` with a built-in transport factory registry, a lean `Request` type carrying only request semantics, and explicit per-call override options. Git pack protocol operations such as `git-upload-pack`, `git-receive-pack`, and `git-upload-archive` become adapters layered on top of the transport primitives instead of being encoded directly into the transport interface. Git LFS support follows the same model via separate SSH and HTTP adapters.

This redesign is intentionally breaking. Backward compatibility is not a constraint for this work.

## Motivation

The current transport API in `plumbing/transport` is effective for classic Git pack operations, but it mixes together concerns that would benefit from clearer separation.

First, go-git's transport abstraction is currently pack-protocol-centric. The stable API is shaped around creating sessions for `git-upload-pack` and `git-receive-pack`, and the experimental branch also tends to collapse transport capabilities back into pack-specific session methods such as `Fetch`, `Push`, and `GetRemoteRefs`. This makes the API awkward for transport use cases that are not themselves pack protocol operations.

Second, not all transports have the same capabilities. SSH, Git TCP, and file transports are naturally command-oriented and can support opening a raw stream for an arbitrary remote command. HTTP is fundamentally different: it is stateless and half-duplex, and should not pretend to support a reusable full-duplex stream. The transport API should model these differences directly instead of forcing all transports into one pack-shaped interface.

Third, there is growing demand for higher-level protocols layered on top of the same transport mechanisms. Examples include `git-upload-archive`, `git-lfs-authenticate` over SSH, `git-lfs-transfer` over SSH, and Git LFS Batch and basic transfer APIs over HTTP. These are not all pack protocol operations, but they reuse the same notions of URL selection, authentication, dialing, proxying, TLS policy, and command execution.

Fourth, users need a better extension story. The project already supports replacing built-in transport implementations, but the API surface is not yet ideal for implementing custom transports, remote-helper-like adapters, or application-specific protocols in a clean and reusable way.

The goals of this RFC are therefore:

- make the transport API composable and transport-neutral
- preserve a universal entrypoint that works for all built-in transports
- expose raw stream transport capabilities when those capabilities genuinely exist
- make pack protocol a consumer of transport primitives rather than the transport abstraction itself
- provide a clean foundation for SSH-based and HTTP-based Git LFS adapters
- support immutable client defaults with optional per-call overrides
- preserve the ability to register or replace built-in transports through a uniform factory model
- keep the public request shape small and avoid mixing transport policy into request semantics
- redefine authentication and proxying in a way that matches real transport behavior

## Detailed design

### Design principles

The proposed design follows these principles:

1. **Separate transport capabilities from Git protocol adapters.** The transport layer should know how to open an exchange for a request. It should not itself define Git fetch, push, refs, archive, or LFS semantics.
2. **Make full-duplex streaming explicit.** Some transports can open an `io.ReadWriteCloser`; some cannot.
3. **Keep the universal abstraction small.** All built-in transports should implement the same `Transport.Open` API.
4. **Use immutable client defaults.** Authentication, dialing, proxying, TLS, and HTTP policy should primarily live at client construction time, with optional per-call overrides.
5. **Keep requests transport-neutral.** Git-specific conventions such as repository-as-first-argument should be handled by adapters and transport implementations, not imposed on callers.
6. **Treat sessions as single-shot exchanges.** A session represents one opened exchange for one request.
7. **Prefer standard library configuration types and patterns where they fit.** The redesign should not introduce new public transport configuration types if Go's existing HTTP and dialing abstractions already express the configuration more idiomatically.
8. **Do not force fake transport-neutral abstractions.** If HTTP and SSH authenticate differently, the public API should model that directly instead of introducing a vague shared interface.

### Proposed API surface

The design centers around five main concepts:

- `Request`: describes the operation to open
- `Session`: a single-shot transport exchange
- `Transport`: universal transport interface implemented by all transports
- `Connectable`: optional capability for transports that can open a raw full-duplex stream
- `Client`: immutable defaults plus a scheme-to-factory registry

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
- The previous `Endpoint` shape is not embedded into the redesigned request type. The old extra endpoint fields were transport policy fields rather than request semantics and are handled elsewhere in this design.
- Existing URL parsing helpers, including scp-like normalization logic, may continue to exist as helper functions, but the request itself should carry a normalized `*url.URL`.
- `Command` is a transport-neutral command name such as `git-upload-pack`, `git-receive-pack`, `git-upload-archive`, `git-lfs-authenticate`, or `git-lfs-transfer`.
- `Args` carries command-specific arguments after the command name. For example, `git-lfs-authenticate <repo> <operation>` maps to `Args: []string{"download"}`.
- `Protocol` communicates Git protocol version preference where relevant.

The repository path is **not** a field on `Request`. In canonical Git, the repository path is always derived from the URL for every transport protocol:

| Transport | URL | Repository path |
|-----------|-----|----------------|
| SSH | `ssh://host/foo/bar.git` | `/foo/bar.git` (from URL path) |
| SSH SCP | `host:foo/bar.git` | `foo/bar.git` (normalized into URL path) |
| HTTP | `https://host/foo/bar.git` | `/foo/bar.git` (from URL path) |
| git:// | `git://host/foo/bar.git` | `/foo/bar.git` (from URL path) |
| file:// | `file:///srv/repo.git` | `/srv/repo.git` (from URL path) |

Adapters (pack, LFS) are responsible for extracting the repository path from `URL.Path` when constructing the transport command. This eliminates the risk of divergence between `URL` and a separate `Repository` field, and matches how canonical Git handles the relationship.

The split between `URL` and `Command`/`Args` is intentional:

- `URL` is the **where**
- `Command` and `Args` are the **what**

Those concerns are related but not redundant.

### Session

`Session` is the universal result of opening one transport exchange.

```go
type Session interface {
    io.Closer
    Reader() io.Reader
    Writer() io.WriteCloser
}
```

A session is intentionally minimal.

- It is **single-shot**: it represents one opened exchange for one `Request`.
- It is **transport-neutral**: it does not expose Git pack methods, refs, capabilities, or LFS concepts.
- It is **not inherently full-duplex**: implementations may back `Reader` and `Writer` with the same stream or with different halves of a stateless exchange.

#### Session lifecycle contract

All `Session` implementations must follow a **write-then-close-writer-then-read** lifecycle:

1. The caller writes the request payload to `Writer()`.
2. The caller closes `Writer()` to signal that the request is complete.
3. The caller reads the response from `Reader()`.
4. The caller calls `Close()` to release all resources.

This contract is explicit because the two session families have different internal behaviors:

For **stream-backed sessions** (SSH, Git TCP, file), `Reader()` and `Writer()` refer to the same underlying `io.ReadWriteCloser`. Closing the writer signals the write half of the stream. The read side remains available until `Close()`.

For **HTTP-backed sessions**, `Writer()` returns a pipe that buffers the request body. The HTTP round-trip is triggered when the caller closes `Writer()`. Only after the round-trip completes does `Reader()` become readable, returning the response body.

Adapters must not attempt to read from `Reader()` before closing `Writer()`. Violating this contract may deadlock on HTTP-backed sessions.

### Connectable

`Connectable` is an optional lower-level capability implemented only by transports that can truly open a raw full-duplex stream.

```go
type Connectable interface {
    Connect(context.Context, *Request) (io.ReadWriteCloser, error)
}
```

This interface is expected to be implemented by:

- SSH transports
- Git TCP transports
- file transports
- custom helper transports that can provide a full-duplex stream

It is explicitly **not** implemented by HTTP transports. When HTTP is used through an adapter that requires `Connect`, it should report a typed unsupported operation error.

`Connectable` is valuable because it exposes a real capability boundary. SSH, Git TCP, and file transports can produce a raw stream; HTTP cannot.

### Transport

`Transport` is the universal transport interface.

```go
type Transport interface {
    Open(context.Context, *Request) (Session, error)
}
```

All built-in transports implement `Transport`.

For transports that also implement `Connectable`, `Open` can be a thin adapter that wraps the connected `io.ReadWriteCloser` into a `Session`.

For HTTP, `Open` is the primary interface. It performs the one-shot stateless exchange and returns an HTTP-backed `Session`.

This RFC does **not** introduce a separate public `StatelessConnectable` concept at this stage. `Transport.Open` is sufficient for HTTP, and adding another public capability interface would expand API surface without enough payoff.

### Client

The design introduces a reusable, immutable `Client` with built-in transport factories.

```go
type Client struct {
    defaults ClientOptions
    registry Registry
}

type ClientOptions struct {
    HTTP  HTTPOptions
    SSH   SSHOptions
    Dial  DialOptions
    Proxy ProxyOptions
}

type Factory func(ClientOptions) Transport
```

A constructor such as the following is proposed:

```go
func NewClient(opts ...ClientOption) *Client
```

`NewClient` pre-registers built-in factories for:

- `http`
- `https`
- `ssh`
- `git`
- `file`

`Client` is **immutable after construction**. All configuration, including scheme registration, happens at construction time through functional options:

```go
func WithScheme(scheme string, factory Factory) ClientOption
func WithoutBuiltins() ClientOption
```

Examples:

```go
// Default client with all built-in transports
c := NewClient()

// Client with a custom transport for a custom scheme
c := NewClient(WithScheme("custom", myFactory))

// Client with only explicitly registered transports
c := NewClient(WithoutBuiltins(), WithScheme("ssh", mySSHFactory))
```

This keeps built-in transports ordinary. They are not special-cased after client construction; they are simply registered defaults that can be replaced.

Immutability means `Client` is safe for concurrent use without synchronization. Users who need a different registry construct a new `Client`. Per-call overrides via `CallOptions` handle the dynamic cases (different auth, different proxy) without mutating the client.

This aligns with the Go ecosystem consensus (`net/http.Client`, gRPC `ClientConn`, AWS SDK v2) where clients are configured at construction and reused.

#### Client.Close

```go
func (c *Client) Close() error
```

`Client` implements `io.Closer`. Calling `Close` releases resources held by the client, such as idle HTTP connections, cached transports, or connection pools.

`Close` must be called when the client is no longer needed. Even if the initial implementation is minimal, the method establishes the cleanup contract from day one. Retrofitting `io.Closer` later would be a breaking change in practice.

### Authentication model

The redesign should **not** keep a single transport-neutral `AuthMethod` interface.

HTTP and SSH authentication are fundamentally different:

- HTTP authentication mutates or enriches an `*http.Request`
- SSH authentication produces or influences an `*ssh.ClientConfig`

Those are not two implementations of one meaningful core behavior. A shared `AuthMethod` would either become vague or leak transport details into the core transport package.

Instead, authentication should be modeled as transport-specific configuration.

#### HTTP authentication

```go
type HTTPOptions struct {
    Client     *http.Client
    Authorizer func(*http.Request) error
}
```

Notes:

- `Client` is the underlying HTTP client used by HTTP-based transports and adapters.
- `Authorizer` is a small hook that can mutate the outgoing request.
- Basic auth, bearer auth, token headers, LFS authorization headers, and custom enterprise auth schemes all fit naturally into this model.

Examples of helper constructors that may exist outside the core transport package:

```go
func Basic(username, password string) func(*http.Request) error
func Bearer(token string) func(*http.Request) error
```

#### SSH authentication

```go
type SSHOptions struct {
    ClientConfig func(context.Context, *Request) (*ssh.ClientConfig, error)
}
```

Notes:

- SSH auth should be expressed as a provider of `*ssh.ClientConfig`, not as a custom auth interface.
- This gives full control over auth methods, username, host key policy, algorithms, timeout, and agent usage.
- It is more idiomatic and more flexible than trying to abstract SSH auth into a tiny custom interface.

Examples of helper constructors that may exist outside the core transport package:

```go
func FromAgent(...) func(context.Context, *Request) (*ssh.ClientConfig, error)
func PublicKeys(...) func(context.Context, *Request) (*ssh.ClientConfig, error)
```

### Dialing model

Stream transports should share a common dialing hook:

```go
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type DialOptions struct {
    DialContext DialContextFunc
}
```

Notes:

- The public hook should be a `DialContext` function shape rather than `net.Dialer` directly.
- `net.Dialer` remains the obvious default implementation, but the public API should accept the narrower and more flexible function form.
- This makes it easy to plug in standard dialers, SOCKS dialers, tracing dialers, test doubles, or wrappers.

### Proxy model

The redesign should support proxying as a higher-level policy that works across the transport APIs, but it should not reintroduce the old `ProxyOptions` bag of URL, username, and password fields.

There is no single standard library proxy abstraction that covers both HTTP and arbitrary stream dialing. The correct split is:

- for HTTP, proxying is configured through `*http.Client` / `*http.Transport`
- for stream transports such as SSH and Git TCP, proxying is configured through wrapped dialing behavior

To model this consistently, the redesign should introduce a proxy adapter concept:

```go
type ProxyOptions struct {
    // HTTPProxy returns the proxy URL for a given HTTP request.
    // If nil, http.ProxyFromEnvironment is used for HTTP transports.
    // Use http.ProxyURL(u) for a fixed proxy URL.
    HTTPProxy func(*http.Request) (*url.URL, error)

    // DialProxy wraps a DialContext function to route stream connections
    // (SSH, git://) through a proxy. If nil, connections are made directly.
    DialProxy func(DialContextFunc) DialContextFunc
}
```

This uses plain function types instead of an interface. HTTP and stream proxy are independent concerns — you rarely configure both — and the function signatures map directly to their standard library counterparts:

- `HTTPProxy` has the same signature as `http.Transport.Proxy`
- `DialProxy` wraps `DialContextFunc` the same way middleware wraps HTTP handlers

#### Proxy helper package

Helper constructors live in the `transport/proxy` package:

```go
package proxy

// FromURL returns ProxyOptions that route both HTTP and stream traffic
// through the given proxy URL.
func FromURL(u *url.URL) ProxyOptions {
    return ProxyOptions{
        HTTPProxy: http.ProxyURL(u),
        DialProxy: socksDialer(u), // wraps x/net/proxy internally
    }
}

// FromEnvironment returns ProxyOptions that honor standard proxy
// environment variables (HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, NO_PROXY).
func FromEnvironment() ProxyOptions {
    return ProxyOptions{
        HTTPProxy: http.ProxyFromEnvironment,
        DialProxy: envDialer(), // honors ALL_PROXY/NO_PROXY via x/net/proxy
    }
}
```

Expected behavior:

- for HTTP, use standard library primitives such as `http.ProxyURL` and `http.ProxyFromEnvironment`
- for stream dialing, use `golang.org/x/net/proxy` where applicable, especially for SOCKS5
- for HTTP CONNECT stream proxying, provide a dedicated adapter implementation if needed

This replaces the old `ProxyOptions{URL, Username, Password}` type. The migration from `ProxyOptions{URL: "socks5://host:port"}` becomes `proxy.FromURL(u)` — same behavior, better abstraction, no new interface types needed for the common case.

### Default and override policy

This redesign supports immutable client defaults plus optional per-call overrides.

A small per-call override shape is proposed:

```go
type CallOptions struct {
    HTTP  *HTTPOptions
    SSH   *SSHOptions
    Dial  *DialOptions
    Proxy *ProxyOptions
}
```

The client-facing API may therefore look like:

```go
func (c *Client) Open(ctx context.Context, req *Request, opts ...CallOption) (Session, error)
```

or another equivalent shape that preserves the same semantics.

The important point is that:

- request semantics live in `Request`
- connection policy lives in client defaults and per-call overrides
- the effective configuration for one call is resolved before creating or selecting the transport instance used to open the request

#### Default HTTP client

The client should have a default HTTP client underneath.

If the user does not supply one, the HTTP transport factory should create a sensible default `*http.Client` with a default `*http.Transport`.

#### Merge versus override

The default rule should be **replace, not deep merge**, especially for complex objects.

Recommended behavior:

- `HTTP.Client`: replace
- `HTTP.Authorizer`: compose in order `client default -> per-call override`, if both are present
- `SSH.ClientConfig provider`: replace
- `Dial.DialContext`: replace
- `Proxy.Proxy`: replace

The reasoning is:

- complex objects such as `*http.Client` and SSH config providers should not be implicitly deep-merged
- tiny hooks such as an HTTP request authorizer can be intentionally composed

### Why `Headers`, `Env`, `Metadata`, and a core `AuthMethod` are not part of `Request`

Earlier brainstorming considered putting `Headers`, `Env`, `Metadata`, and `AuthMethod` directly on `Request`.

That direction was rejected.

- `AuthMethod` is not transport-neutral and belongs in transport-specific options.
- `Metadata` is too vague and would quickly become an escape hatch for bypassing the design.
- `Headers` are specific to HTTP-oriented adapters.
- `Env` is specific to command-oriented transports such as SSH.

Those concepts still exist, but they should not all be promoted to first-class fields on the universal public `Request` type.

Instead:

- authentication lives in transport-specific client defaults and per-call overrides
- HTTP-specific adapters are free to construct the HTTP request headers they need internally
- command-oriented transports may translate `Request.Protocol` and adapter-specific semantics into environment variables such as `GIT_PROTOCOL` internally
- if later implementation experience shows that a tiny transport-neutral extension point is truly necessary, it can be introduced in a follow-up RFC with a much clearer justification

### Session implementations

The transport layer will likely need at least two internal session implementations.

#### Stream-backed session

For SSH, Git TCP, and file transports:

```go
type streamSession struct {
    rwc io.ReadWriteCloser
}

func (s *streamSession) Reader() io.Reader      { return s.rwc }
func (s *streamSession) Writer() io.WriteCloser { return nopWriteCloser{s.rwc} }
func (s *streamSession) Close() error           { return s.rwc.Close() }
```

The exact implementation may differ, but the key property is that one underlying stream backs both read and write sides.

#### HTTP-backed session

For HTTP transports:

```go
type httpSession struct {
    requestBody  io.WriteCloser
    responseBody io.ReadCloser
}

func (s *httpSession) Reader() io.Reader      { return s.responseBody }
func (s *httpSession) Writer() io.WriteCloser { return s.requestBody }
func (s *httpSession) Close() error           { ... }
```

The implementation details are transport-internal. The important property is that HTTP can participate in the universal `Open` API without pretending to be a reusable full-duplex connection.

### Built-in transport behavior

#### SSH

SSH implements both `Transport` and `Connectable`.

- `Connect` opens an SSH-backed `io.ReadWriteCloser`
- `Open` wraps that stream into a `Session`
- command execution is derived from `Request.Command`, `URL.Path` (as the repository path), and `Request.Args`
- protocol negotiation hints such as `GIT_PROTOCOL` are derived internally from `Request.Protocol`
- SSH authentication comes from `SSHOptions.ClientConfig`
- proxying and custom dialing come from the resolved `DialOptions` and `ProxyOptions`

SSH-specific stderr handling should remain a transport implementation detail. The common API should not grow an optional stderr capability solely for one transport.

When stderr output is meaningful, the SSH transport should prefer folding it into returned errors.

#### Git TCP

Git TCP implements both `Transport` and `Connectable`.

- `Connect` opens the raw TCP stream
- `Open` wraps it in a `Session`
- request encoding uses `Request.Command`, `URL.Path` (as the repository path), and `Request.Protocol`
- dialing and proxying come from the resolved `DialOptions` and `ProxyOptions`

The transport remains generic enough to support Git service commands, but higher-level validation lives in adapters.

#### File

File implements both `Transport` and `Connectable`.

It acts as a local stream provider for Git service commands and can support command-oriented adapters where the repository backend can provide the necessary semantics.

The exact scope of arbitrary file-backed commands is a design choice for implementation. At minimum, file transport should behave consistently with the generic transport interfaces and not remain hardcoded to pack-only methods.

#### HTTP

HTTP implements `Transport` but not `Connectable`.

- `Open` represents one stateless exchange
- requests may map to info/refs discovery, service RPC, or other higher-level HTTP protocols depending on the adapter using the transport
- the underlying HTTP client comes from `HTTPOptions.Client` or the built-in default
- request authorization comes from `HTTPOptions.Authorizer`
- proxy behavior is derived from either the provided HTTP client or the resolved `ProxyOptions`
- attempts to use HTTP through a stream-only adapter must fail with a typed unsupported error

This matches the transport reality and avoids pretending that HTTP can expose a raw reusable command stream.

### Pack adapter layer

The Git pack protocol becomes an adapter on top of `Client`, `Transport`, and `Session`.

Its job is to:

- build `Request` values for pack operations
- choose correct command names
- apply Git protocol version policy
- translate `Request.Protocol` into the correct transport-specific behavior
- parse adv-refs, capability data, and v2 response frames
- handle pack negotiation and send-pack/fetch-pack semantics
- enforce transport-specific restrictions

The adapter mirrors the current `Session`/`Connection` pattern from the existing transport API. The lifecycle is:

1. Create a `PackClient` from a generic `Client`
2. Call `Handshake` to open a pack session, which reads the advertised refs and capabilities from the server
3. Use `Capabilities()` and `GetRemoteRefs()` on the returned `PackSession` to inspect server state
4. Call `Fetch` or `Push` on the `PackSession` to drive the operation
5. Close the session

```go
type PackClient struct {
    client *Client
}

func (c *PackClient) Handshake(ctx context.Context, url *url.URL, service Service, opts ...CallOption) (*PackSession, error)
```

`Handshake` takes a URL and a `Service` enum (`UploadPackService` or `ReceivePackService`). The pack adapter builds the `*Request` internally — choosing the correct command name (`git-upload-pack` or `git-receive-pack`), deriving the repository path from `url.Path`, and applying protocol version policy. Callers never construct a `*Request` directly when using the pack adapter.

The adapter opens a transport `Session` via the generic `Client`, performs the initial ref advertisement exchange, and returns a `PackSession` that holds the connection state:

```go
type PackSession struct { ... }

func (s *PackSession) Capabilities() *capability.List
func (s *PackSession) Version() protocol.Version
func (s *PackSession) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
func (s *PackSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
func (s *PackSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error
func (s *PackSession) Close() error
```

This maps directly to the existing `Connection` interface:

| Current (`Connection`) | New (`PackSession`) |
|------------------------|---------------------|
| `Capabilities()` | `Capabilities()` |
| `Version()` | `Version()` |
| `GetRemoteRefs(ctx)` | `GetRemoteRefs(ctx)` |
| `Fetch(ctx, req)` | `Fetch(ctx, st, req)` |
| `Push(ctx, req)` | `Push(ctx, st, req)` |
| `Close()` | `Close()` |

`Fetch` and `Push` accept a `storage.Storer` so the pack adapter can read/write objects directly. This moves the storer dependency out of session construction (where it currently lives in `Transport.NewSession`) and into the operations that actually need it.

The key difference is that `PackSession` is built on top of the new `Transport.Open`/`Session` primitives rather than being the transport abstraction itself. The pack protocol is an adapter, not the transport layer.

Callers like `remote.go` use this the same way they use `Connection` today: handshake first, inspect capabilities (e.g., `delete-refs`, `allow-reachable-sha1-in-want`, `ofs-delta`), then call `Fetch` or `Push`.

The exact final shape may be integrated into existing plumbing packages, but the key design point is that pack protocol methods are **not part of `Transport` or `Session`**.

#### `git-upload-archive`

`git-upload-archive` does not perform a ref advertisement handshake. It is a separate operation that opens a transport session directly and speaks its own protocol. It should therefore live on `PackClient` as an independent method rather than going through `Handshake`/`PackSession`:

```go
func (c *PackClient) Archive(ctx context.Context, url *url.URL, req *ArchiveRequest, opts ...CallOption) (io.ReadCloser, error)
```

The command is always `git-upload-archive`, so the adapter builds the `*Request` internally. The caller only provides the remote URL and archive-specific parameters (tree-ish, format, etc.).

The pack adapter is responsible for transport-specific support policy:

- SSH, Git TCP, and file can open an appropriate request and speak the required protocol over the session
- HTTP must enforce protocol restrictions
- smart HTTP v0/v1 do not support `git-upload-archive`
- smart HTTP v2 may support it when enabled server-side

This keeps transport-neutral layers simple while still allowing protocol-specific validation and error reporting.

### Git LFS adapter layer

Git LFS should be implemented as a separate adapter family that reuses the same transport primitives and client policy.

It should not be merged into the pack adapter, because LFS is a distinct application protocol.

#### SSH-based LFS adapters

Two SSH-oriented adapters are in scope:

- `git-lfs-authenticate`
- `git-lfs-transfer`

Examples:

- `Request{URL: u, Command: "git-lfs-authenticate", Args: []string{"download"}}`
- `Request{URL: u, Command: "git-lfs-transfer", Args: ...}`

The repository path is derived from `u.Path` by the SSH transport when constructing the remote command.

These adapters use the universal transport API and rely on the SSH transport to map repository and arguments into the actual remote command invocation.

#### HTTP-based LFS adapters

Git LFS HTTP support should reuse the same universal `Transport.Open` and `Session` abstractions, but it should be implemented as its own adapter layer.

This provides reuse of:

- client defaults
- authentication policy
- dial policy
- proxy policy
- TLS policy
- HTTP client policy
- transport factory registry

without forcing LFS semantics into pack protocol abstractions.

LFS HTTP therefore does **not** need an entirely separate transport hierarchy. It needs a separate adapter built on top of the common transport layer.

### Errors

The transport redesign should introduce typed capability and support errors where possible.

Examples include:

```go
var ErrConnectUnsupported = errors.New("transport does not support raw connections")
var ErrCommandUnsupported = errors.New("command is not supported by transport")
var ErrProtocolUnsupported = errors.New("protocol version is not supported")
```

Adapters should prefer returning precise typed errors instead of generic failures when the requested operation is valid in principle but unsupported by a transport or protocol version.

#### Session errors

For partial exchange failures — e.g., SSH session opened successfully but the remote command exited non-zero, or HTTP returned a 401 mid-stream — the transport layer should introduce a structured `SessionError`:

```go
type SessionError struct {
    Op      string // e.g. "ssh", "http"
    URL     *url.URL
    Code    int    // HTTP status code, SSH exit code, or 0 if not applicable
    Stderr  string // truncated stderr from remote command, if available
    Err     error  // underlying error
}

func (e *SessionError) Error() string { ... }
func (e *SessionError) Unwrap() error { return e.Err }
```

Adapters need this to produce meaningful user-facing errors. For example, a pack adapter receiving a `SessionError` with `Code: 128` and `Stderr: "fatal: repository not found"` can translate that into a precise `ErrRepositoryNotFound` for the caller.

### Breaking changes

This RFC is intentionally about a breaking API redesign.

Backward compatibility is not a design constraint for this work. The goal is to define the cleaner API that go-git should have, and then implement it directly on top of the main branch rather than preserving the current transport shape through compatibility shims.

This means the old pack-centric transport API may be removed or rewritten in place during implementation.

### Non-goals

This RFC does not attempt to:

- fully specify the final Git LFS public API
- standardize every possible custom command protocol
- define transport multiplexing or long-lived pooled sessions
- define all timeout, retry, tracing, and observability hooks
- fully solve repository-store scoping or caching semantics for every transport backend

Those concerns can be addressed in follow-up RFCs or implementation PRs once the core transport model is in place.

## Drawbacks

This design introduces another abstraction layer between transport implementations and Git pack semantics. While this separation is intentional, it does increase conceptual surface area.

The new model also requires careful design around `Session` to ensure that the universal interface remains minimal and clear rather than becoming a dumping ground for transport-specific behavior.

HTTP-backed sessions may initially feel less natural than stream-backed sessions because they expose a request/response lifecycle through the same universal interface. This is a tradeoff accepted in exchange for keeping a single `Open` entrypoint for all transports.

The model also relies on a disciplined split between request semantics and connection policy. If future implementation work carelessly pushes more policy back into `Request`, the API will become muddled again.

## Rationale and alternatives

### Why this design

This design is the best fit for the problem because it aligns the public API with actual transport capabilities while keeping the request shape small.

- `Connectable` exists only where a real full-duplex stream exists.
- `Transport.Open` provides the universal entrypoint needed for all transports, including HTTP.
- `Session` is small enough to avoid encoding protocol-specific semantics.
- pack and LFS become adapters, which is where their protocol logic naturally belongs.
- authentication is modeled in a transport-specific way instead of through a vague shared interface.
- immutable client defaults with per-call overrides match common Go practice and keep configuration understandable.
- proxy configuration is expressed through standard Go HTTP configuration, dialers, and a thin higher-level adapter rather than a bespoke URL/username/password bag.
- built-in transport factories and user-registered factories share one mechanism, which improves extensibility.

### Alternative 1: Keep pack semantics in the transport API

This is close to the current model.

It was rejected because it keeps pack protocol at the wrong abstraction level and makes non-pack use cases awkward.

### Alternative 2: Make all transports implement only `Connectable`

This would force HTTP to pretend to be a raw stream transport or require awkward HTTP stream emulation.

It was rejected because HTTP is not a full-duplex reusable command transport, and the API should not misrepresent that.

### Alternative 3: Introduce a separate public `StatelessConnectable`

This would explicitly model HTTP's stateless nature as a sibling capability to `Connectable`.

It was rejected for now because `Transport.Open` already captures HTTP cleanly enough. A separate public stateless capability can be added later if implementation experience shows that it provides meaningful value.

### Alternative 4: Put repository path as a first-class field on `Request`

Earlier versions of this RFC included a `Repository string` field on `Request`.

It was removed because in canonical Git, the repository path is always derived from the URL for every transport protocol. Having both `URL.Path` and `Repository` as independent fields creates a divergence risk where the two values could disagree. Adapters are the right place to extract the repository path from `URL.Path` when constructing transport commands.

### Alternative 5: Put auth, headers, env, metadata, and other policy directly into `Request`

This was considered during early brainstorming.

It was rejected because it makes `Request` too complicated and mixes request semantics with transport policy and adapter-specific concerns. A lean request plus client defaults and per-call overrides is clearer.

### Alternative 6: Keep a universal `AuthMethod` interface

This would preserve the old conceptual model that one auth abstraction could span HTTP and SSH.

It was rejected because HTTP request authorization and SSH client configuration are fundamentally different operations.

### Alternative 7: Keep `ProxyOptions` as a dedicated URL/username/password bag

This would preserve a more go-git-specific proxy model.

It was rejected because proxy configuration is more idiomatically expressed through standard Go HTTP configuration, stream dialers, and a thin higher-level proxy adapter.

### Alternative 8: Make auth, dial, and proxying only per-client

This would maximize immutability and simplicity.

It was rejected because higher-level protocols and exceptional workflows often need per-call overrides, especially around authentication and custom HTTP or SSH behavior.

## Open implementation questions

The following questions are intentionally left to the implementation phase or follow-up design work:

- whether `HTTP.Authorizer` should always compose defaults and per-call overrides, or whether composition should be configurable
- whether `DialContextFunc` should remain a simple function type or be replaced with a richer interface later
- how much of the pack adapter API should remain in `plumbing/transport` versus moving into a new package
- whether file transport should support arbitrary commands immediately or initially focus on Git service commands only
- whether existing endpoint parsing helpers should remain public and, if so, how they should be named once `Request` carries a plain `*url.URL`

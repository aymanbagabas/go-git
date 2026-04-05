# Transport API redesign overview

This document explains how the transport API redesign described in `rfcs/0000-transport-api-redesign.md` replaces the current transport types on the main branch and how the implementation should be reshaped around the new model.

Unlike the RFC, this document is implementation-facing. It focuses on mapping old concepts to new ones, identifying the main code paths that will change, and outlining the broad rewrite plan for the main branch.

## Current main-branch model

The current transport design is centered around pack protocol sessions.

At the public transport layer:

- `plumbing/transport/transport.go:45-52` defines `Transport` as:
  - `NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)`
  - `SupportedProtocols() []protocol.Version`
- `plumbing/transport/transport.go:54-58` defines a transport-neutral `AuthMethod`
- `plumbing/transport/transport.go:60-70` defines `Endpoint` as `url.URL` plus transport policy fields such as TLS and proxy options
- `plumbing/transport/transport.go:72-102` defines `ProxyOptions` as a go-git-specific URL/username/password bag

At the lower-level pack transport layer:

- `plumbing/transport/common.go:137-145` defines `Session` as a handshake-oriented pack session
- `plumbing/transport/common.go:147-156` defines `Commander`
- `plumbing/transport/common.go:158-180` defines `Command`
- `plumbing/transport/common.go:193-210` defines `NewPackTransport` as an adapter from `Commander` to `Transport`

At the registry layer:

- `plumbing/transport/registry.go:16` registers scheme implementations with `Register(protocol string, c Transport)`

At the transport implementations:

- HTTP implements `Transport` directly and creates pack sessions in `plumbing/transport/http/common.go:224-232`
- SSH uses `transport.NewPackTransport(&runner{...})` in `plumbing/transport/ssh/common.go:36-39`
- Git and file follow the same pattern via the pack transport helper

At the porcelain/plumbing usage layer:

- `remote.go` and related call sites create a transport client from a URL, then call `NewSession(...)`, then drive pack fetch/push behavior on top of the returned pack session

In short, the main branch currently treats transport selection, authentication, proxying, pack negotiation, and command execution as one mostly pack-shaped API.

## New model at a glance

The RFC replaces that shape with a layered model:

- a lean request type describing the target URL and requested operation
- a universal `Transport.Open` entrypoint implemented by all transports
- an optional `Connectable` capability for true full-duplex transports
- a minimal, transport-neutral `Session`
- an immutable generic `Client` with built-in transport factories
- transport-specific auth configuration instead of one shared `AuthMethod`
- a higher-level proxy adapter instead of embedding proxy URL fields in endpoints
- pack and LFS as adapters above the transport layer instead of being the transport layer

## Old-to-new type mapping

### `transport.Transport`

Current:

- `NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)`
- `SupportedProtocols() []protocol.Version`

New:

- `Open(context.Context, *Request) (Session, error)`

Impact:

- `NewSession` disappears
- `SupportedProtocols` disappears from the transport interface
- pack protocol version support moves into the pack adapter layer rather than being declared as a transport-wide property

Reason:

The transport should open an exchange. It should not itself expose pack-session creation semantics.

### `transport.Session`

Current:

- handshake-oriented pack session interface in `plumbing/transport/common.go:137-145`

New:

- minimal transport-neutral session:
  - `io.Closer`
  - `Reader() io.Reader`
  - `Writer() io.WriteCloser`

Impact:

- handshake, refs, fetch, push, and pack semantics move out of `Session`
- the new `Session` becomes a thin exchange object usable by pack and non-pack adapters alike

Reason:

The current session type is really a pack protocol entrypoint. The redesign turns it into a transport exchange object.

### `transport.Commander` and `transport.Command`

Current:

- `Commander.Command(ctx, cmd string, ep *Endpoint, auth AuthMethod, params ...string) (Command, error)`
- `Command` models a started command with stdio pipes

New:

- `Connectable.Connect(ctx, req *Request) (io.ReadWriteCloser, error)`
- command-oriented transports derive their concrete execution internally from `Request`

Impact:

- the explicit `Command` interface disappears from the main transport abstraction
- SSH, Git TCP, file, and helper transports still perform command execution internally, but they surface it as a raw stream or `Session`

Reason:

`Command` was a useful seam, especially for SSH, but the new API wants the public transport primitive to be the stream or exchange rather than the process/session object itself.

### `transport.AuthMethod`

Current:

- common auth interface in `plumbing/transport/transport.go:54-58`

New:

- removed from the core transport package
- replaced by transport-specific configuration:
  - `HTTPOptions.Authorizer func(*http.Request) error`
  - `SSHOptions.ClientConfig func(context.Context, *Request) (*ssh.ClientConfig, error)`

Impact:

- all existing HTTP auth helpers and SSH auth helpers need to be re-expressed in transport-specific packages or helper constructors
- the client and call option model now carries transport-specific auth state

Reason:

HTTP request authorization and SSH client configuration are not one common concept.

### `transport.Endpoint`

Current:

- wraps `url.URL` and carries `InsecureSkipTLS`, `CaBundle`, and `Proxy`

New:

- removed from the public transport request model
- replaced by:
  - `Request.URL *url.URL`
  - client defaults and call overrides for TLS, HTTP client, dialing, and proxying

Impact:

- all code currently depending on `Endpoint` as both URL and transport policy container must be split
- parsing helpers may remain, but they should normalize into plain `*url.URL`

Reason:

`Endpoint` currently mixes request identity with connection policy.

### `transport.ProxyOptions`

Current:

- go-git-specific proxy URL/username/password type in `plumbing/transport/transport.go:72-102`

New:

- removed from the old endpoint-centered API
- replaced by a higher-level proxy adapter in client and call options

Proposed new shape:

```go
type ProxyOptions struct {
    HTTPProxy func(*http.Request) (*url.URL, error)
    DialProxy func(DialContextFunc) DialContextFunc
}
```

These are plain function types rather than an interface. `HTTPProxy` has the same signature as `http.Transport.Proxy`. `DialProxy` wraps `DialContextFunc` for stream transports (SSH, git://).

Helper constructors live in a dedicated `transport/proxy` package:

- `proxy.FromURL(u)` — routes both HTTP and stream traffic through the given URL
- `proxy.FromEnvironment()` — honors standard proxy environment variables

Impact:

- HTTP proxying uses standard library HTTP transport support
- stream transport proxying uses wrapped dialers and `x/net/proxy` where appropriate
- HTTP CONNECT support for stream transports may need a dedicated adapter

Reason:

Proxying is real, but the old URL/username/password struct is too narrow and transport-specific.

### `transport.Register`

Current:

- global registry of `Transport` implementations keyed by scheme

New:

- registry moves under the immutable generic client
- `NewClient(...)` pre-registers built-in factories
- scheme registration is construction-time only via `WithScheme(scheme, factory)` functional options
- `Client` is fully immutable after construction and safe for concurrent use

Impact:

- transport registration becomes client-scoped instead of purely global
- no runtime mutation of the registry; users who need a different registry construct a new `Client`
- decide separately whether a package-level default client facade is still useful

Reason:

Factory registration belongs naturally with the client that resolves and constructs transports. Immutability eliminates race conditions and matches Go ecosystem conventions (`net/http.Client`, gRPC, AWS SDK v2).

## Public API shape after the rewrite

The new public transport shape should revolve around these concepts:

```go
type Request struct {
    URL *url.URL

    Command  string
    Args     []string

    Protocol protocol.Version
}
```

The repository path is not a field on `Request`. Adapters derive it from `URL.Path`, matching how canonical Git handles the relationship for all transport protocols.

```go
type Session interface {
    io.Closer
    Reader() io.Reader
    Writer() io.WriteCloser
}
```

```go
type Connectable interface {
    Connect(context.Context, *Request) (io.ReadWriteCloser, error)
}
```

```go
type Transport interface {
    Open(context.Context, *Request) (Session, error)
}
```

```go
type ClientOptions struct {
    HTTP  HTTPOptions
    SSH   SSHOptions
    Dial  DialOptions
    Proxy ProxyOptions
}
```

```go
type CallOptions struct {
    HTTP  *HTTPOptions
    SSH   *SSHOptions
    Dial  *DialOptions
    Proxy *ProxyOptions
}
```

`Client` is immutable after construction and implements `io.Closer`:

```go
func NewClient(opts ...ClientOption) *Client
func (c *Client) Close() error
```

`Session` follows a **write-then-close-writer-then-read** lifecycle: callers write to `Writer()`, close `Writer()` to signal completion, then read from `Reader()`. This is explicit because HTTP-backed sessions trigger the round-trip when `Writer()` is closed.

## What changes in the main-branch implementation

### 1. Transport becomes exchange-oriented instead of pack-oriented

All transport implementations currently build or return pack sessions. That needs to be inverted.

After the rewrite:

- each transport factory builds a transport that knows how to open a `Session`
- pack protocol is implemented above `Open`
- `plumbing/transport/common.go` should stop being the central home of pack-specific transport contracts

Practical consequence:

- `NewPackTransport` as it exists today is either removed or rewritten as an adapter in the new pack layer rather than the core transport layer

### 2. HTTP stops pretending to be the same shape as SSH/git/file

Today, HTTP still satisfies the same `Transport.NewSession(...)` entrypoint even though its internal behavior is fundamentally stateless.

After the rewrite:

- HTTP implements `Transport.Open`
- HTTP does not implement `Connectable`
- HTTP-backed `Session` models one request/response exchange
- pack-over-HTTP and LFS-over-HTTP become adapters on top of this exchange model

Practical consequence:

- the current `plumbing/transport/http` package needs to be reorganized around exchange opening rather than pack-session creation

### 3. SSH, Git TCP, and file expose a raw stream capability

Today, SSH is modeled through `Command`/`Commander`, Git TCP through pack session creation, and file through pack-aware implementations.

After the rewrite:

- SSH, Git TCP, and file should implement `Connectable`
- their `Open` implementation can be a thin wrapper from `Connect` to `Session`
- the transport itself maps `Request.Command`, `URL.Path` (as the repository path), and `Request.Args` into its concrete execution model

Practical consequence:

- SSH command startup logic from `plumbing/transport/ssh/common.go` survives conceptually, but the public seam changes
- file transport should no longer be hardcoded to the old pack-only public shape

### 4. URL parsing and endpoint handling are split from connection policy

Today, `Endpoint` parsing produces both the URL and extra connection policy fields.

After the rewrite:

- URL parsing helpers should normalize into `*url.URL`
- TLS, HTTP client, dialer, and proxy live in client defaults and call overrides
- any scp-like parsing helpers may remain as public or internal helpers, but they should not return a policy-carrying endpoint object

Practical consequence:

- code paths that currently thread `*Endpoint` through the system must be rewritten to thread `*url.URL` plus resolved client/call options

### 5. Authentication is moved out of the core transport package

Today, auth is passed down as a common `AuthMethod`.

After the rewrite:

- HTTP auth becomes request authorization logic in `HTTPOptions`
- SSH auth becomes `*ssh.ClientConfig` production in `SSHOptions`
- there is no shared core auth interface

Practical consequence:

- `plumbing/transport/http` auth handling needs to be rewritten around `Authorizer`
- `plumbing/transport/ssh` auth handling needs to be rewritten around SSH config providers
- helper packages may remain, but they should stop pretending to implement one common auth contract

### 6. Proxying is elevated to a client policy concept

Today, proxy URL fields are embedded in endpoints and then interpreted by specific transports.

After the rewrite:

- proxying is resolved alongside dialing and HTTP client setup
- HTTP uses `http.Transport.Proxy`
- stream transports use wrapped `DialContext`
- a thin proxy adapter bridges the two worlds

Practical consequence:

- all existing proxy handling code needs to move closer to client option resolution and transport factory setup
- endpoint-level proxy propagation should disappear

### 7. Protocol support moves into adapters

Today, `Transport.SupportedProtocols()` advertises protocol support at the transport layer.

After the rewrite:

- protocol version is carried in `Request.Protocol`
- pack adapters decide which protocol versions are valid for a given operation and transport
- transport implementations only need to honor or reject the protocol hint as relevant

Practical consequence:

- `SupportedProtocols()` should disappear
- protocol capability checks move into pack adapter logic

## Replacement plan by package area

### `plumbing/transport/transport.go`

Replace or rewrite:

- remove `Transport.NewSession`
- remove `Transport.SupportedProtocols`
- remove core `AuthMethod`
- replace `Endpoint` usage with plain URL handling
- remove old `ProxyOptions`
- define or re-export the new `Request`, `Session`, `Connectable`, and `Transport`

### `plumbing/transport/common.go`

Split responsibilities:

- remove the current pack-session contract from the core transport abstraction
- move pack-specific logic behind a pack adapter package or sublayer
- preserve only generic exchange/stream helpers if they still make sense

### `plumbing/transport/registry.go`

Refactor into client-based registration:

- built-in registration should happen in `NewClient`
- client-level scheme overrides should replace the current purely global registration flow
- decide separately whether a package-level default client facade is still useful

### `plumbing/transport/http`

Rewrite around:

- `Transport.Open`
- HTTP-backed `Session`
- `HTTPOptions.Client`
- `HTTPOptions.Authorizer`
- proxy wiring through provided HTTP clients or resolved proxy adapter behavior

Move pack-specific HTTP behavior out of the transport core and into the pack adapter.

### `plumbing/transport/ssh`

Rewrite around:

- `Connectable.Connect`
- `Transport.Open`
- `SSHOptions.ClientConfig`
- `DialOptions` and `ProxyOptions`

The current SSH command construction logic remains conceptually relevant, but it now serves the new `Request`-driven transport API.

### `plumbing/transport/git`

Rewrite around:

- `Connectable.Connect`
- `Transport.Open`
- `DialOptions` and `ProxyOptions`
- request-based command encoding

### `plumbing/transport/file`

Rewrite around:

- `Connectable.Connect`
- `Transport.Open`
- local stream-backed `Session`

Then decide whether arbitrary commands are immediately supported or whether the first implementation only supports Git service commands through the new transport seam.

### `remote.go` and related callers

Refactor to:

- create or receive a new generic client
- build request objects for pack operations
- call pack adapter methods instead of manually opening transport sessions and driving handshake directly

This is one of the largest conceptual changes in the main branch.

## Pack adapter rewrite

The pack adapter is where current transport-facing pack logic should move.

Responsibilities include:

- building `Request` values for `git-upload-pack`, `git-receive-pack`, and `git-upload-archive`
- choosing transport-appropriate behavior for protocol v0/v1/v2
- translating protocol hints into transport-specific execution details
- reading and writing pack protocol frames over `Session`
- enforcing rules like “HTTP v0/v1 do not support archive”

This adapter replaces much of what today is spread across:

- pack-aware transport session implementations
- `Transport.SupportedProtocols()` checks
- direct `NewSession(...)` usage in callers

The pack layer mirrors the current `Session`/`Connection` lifecycle:

1. `PackClient.Handshake(ctx, url, service)` takes a URL and service enum; the adapter builds the `*Request` internally (choosing command name, deriving repo path from `url.Path`, applying protocol policy), opens a transport session via the generic `Client`, reads advertised refs and capabilities, and returns a `PackSession`
2. `PackSession.Capabilities()` and `PackSession.GetRemoteRefs(ctx)` expose server state for callers like `remote.go` to inspect
3. `PackSession.Fetch(ctx, storer, req)` or `PackSession.Push(ctx, storer, req)` drives the operation
4. `PackSession.Close()` releases resources

This maps directly to the existing `Connection` interface (`Capabilities()`, `Version()`, `GetRemoteRefs()`, `Fetch()`, `Push()`, `Close()`), but `PackSession` is built on top of the new `Transport.Open`/`Session` primitives rather than being the transport abstraction itself.

`git-upload-archive` does not perform a ref advertisement handshake. It lives on `PackClient` as an independent method: `PackClient.Archive(ctx, url, archiveReq)`. The command is always `git-upload-archive`, so the adapter builds the `*Request` internally.

`Handshake` reads the advertised refs and capabilities, which `remote.go` then inspects to determine server support (e.g., `delete-refs`, `allow-reachable-sha1-in-want`, `ofs-delta`) before constructing a fetch or push request.

This replaces the old session-based pack API where `Connection.GetRemoteRefs()` and `Connection.Capabilities()` were called separately after handshake.

## LFS adapter rewrite

The new transport layer also gives a clean place for LFS adapters.

### SSH-based LFS

Adapters for:

- `git-lfs-authenticate`
- `git-lfs-transfer`

can be built directly on the universal transport API by constructing `Request` values and using SSH transport behavior.

### HTTP-based LFS

Adapters for:

- Batch API
- basic upload/download
- verify

should reuse the same HTTP transport and client policy infrastructure, without being forced into pack abstractions.

## Implementation sequencing

Because this is a breaking redesign, the implementation can favor clarity over compatibility.

A reasonable implementation order is:

1. introduce the new core transport types and client/option model
2. implement client-scoped transport factory resolution
3. rewrite SSH, Git TCP, file, and HTTP transports to satisfy the new transport interfaces
4. build stream-backed and HTTP-backed session implementations
5. implement the pack adapter on top of `Transport.Open`
6. rewire `remote.go` and related pack call sites to use the pack adapter
7. remove the old `NewSession`, `SupportedProtocols`, `AuthMethod`, `Endpoint`, and `ProxyOptions` APIs
8. add SSH-based and HTTP-based LFS adapters on top of the new transport layer

## Major call-site changes to expect

The current shape:

- parse endpoint
- resolve auth
- create transport client
- call `NewSession(...)`
- handshake and drive pack operations on the returned session

becomes:

- parse URL
- resolve generic client plus call overrides
- call `PackClient.Handshake(ctx, url, service)` to get a `PackSession`
- inspect `PackSession.Capabilities()` and `PackSession.GetRemoteRefs()` to decide behavior
- call `PackSession.Fetch(ctx, storer, req)` or `PackSession.Push(ctx, storer, req)`
- `PackSession.Close()`

This is a deep rewrite, but it also creates a much cleaner separation of concerns.

## What this overview intentionally does not define

This document does not lock down:

- exact package names for the final pack and LFS adapters
- whether a package-level default client facade will remain
- exact helper APIs for HTTP auth and SSH auth constructors
- whether file transport initially supports only Git service commands or all command-shaped requests

Those decisions can be finalized during implementation PRs as long as they remain consistent with the RFC.

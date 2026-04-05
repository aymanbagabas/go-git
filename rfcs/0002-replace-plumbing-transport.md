# RFC 0002: Replacing plumbing/transport with x/transport

## Summary

This RFC proposes replacing the current alpha `plumbing/transport` API with `x/transport` and `x/client` before the transport layer ships as a stable public API. The replacement is already implemented and fully tested. This document compares the two designs, shows what the new API makes possible, and explains the migration plan.

## Motivation

The `plumbing/transport` API is still in alpha and has not been released as stable. Before it ships, we should address several structural problems in its design that would be painful to fix after a stable release.

### 1. Pack protocol is baked into the transport abstraction

The current `Transport` interface requires a `storage.Storer` and `AuthMethod` at session creation:

```go
type Transport interface {
    NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)
    SupportedProtocols() []protocol.Version
}
```

This couples every transport to pack protocol concerns. A transport should know how to open a connection — it should not need to know about object storage or authentication interfaces at the type level. This coupling makes it impossible to use the transport layer for anything other than pack operations.

### 2. Two parallel architectures with no shared abstraction

The current API has two completely separate code paths:

- **SSH, Git TCP, file** go through `Commander` → `Command` (with `StdinPipe`/`StdoutPipe`/`StderrPipe`/`Start`) → `PackSession` → `packConnection`
- **HTTP** bypasses `Commander` entirely and implements `Transport`, `Session`, and `Connection` directly using its own `requester` type that buffers writes and fires an HTTP POST

These two paths share the `Session` and `Connection` interfaces at the top, but have nothing in common underneath. There is no shared transport abstraction that models what they actually have in common — the ability to open an exchange for a given request. A custom transport author has to choose one of two internal architectures and implement a lot of plumbing either way.

The `Connection` interface also conflates transport capabilities. Both paths return a `Connection` with `Fetch`, `Push`, `GetRemoteRefs`, and `StatelessRPC` — but `StatelessRPC` is always `false` for stream transports and always `true` for HTTP. The interface pretends these are the same thing when they behave fundamentally differently.

### 3. Authentication is a shared interface with nothing shared

```go
type AuthMethod interface {
    fmt.Stringer
    Name() string
}
```

HTTP auth sets headers on `*http.Request`. SSH auth produces an `*ssh.ClientConfig`. These operations have nothing in common. The shared interface forces every transport to do runtime type assertions:

```go
auth, ok := rawAuth.(ssh.AuthMethod)
if !ok {
    return transport.ErrInvalidAuthMethod
}
```

This is a runtime failure that should be a compile-time error.

### 4. Endpoint mixes URL identity with connection policy

```go
type Endpoint struct {
    url.URL
    InsecureSkipTLS bool
    CaBundle        []byte
    Proxy           ProxyOptions
}
```

TLS policy, CA bundles, and proxy settings are embedded in the URL type. Two requests to the same URL with different TLS policies need different "endpoints". Proxy uses a custom type (`ProxyOptions{URL, Username, Password}`) instead of Go standard library patterns. TLS uses custom fields (`InsecureSkipTLS bool`, `CaBundle []byte`) instead of the standard `*tls.Config`. Configuration that belongs on the client or transport leaks into every request.

### 5. Proxy is implicit

The current SSH transport calls `proxy.Dial` by default, which silently reads `ALL_PROXY` and `NO_PROXY` environment variables. There is no way to:

- Opt out of environment proxy without providing a custom dialer
- Use different proxy configurations for different transports
- Compose proxy with other dialing behavior

Proxy behavior should be explicit and opt-in.

### 6. No path to non-pack protocols

`git-upload-archive`, `git-lfs-authenticate`, `git-lfs-transfer`, and other SSH-based protocols cannot be expressed through the current API. `Session.Handshake` always expects a pack protocol exchange and returns a `Connection` with `Fetch`/`Push`/`GetRemoteRefs`. There is no way to open a raw bidirectional stream for a different protocol.

This is the biggest limitation. Without raw stream support, go-git cannot implement Git LFS over SSH, archive export, or any custom protocol — even though the underlying transports (SSH, Git TCP) are fully capable of it.

## Detailed design

### New API overview

The new API separates concerns into distinct layers:

```
┌─────────────────────────────────────────────────┐
│  x/client.Client                                │
│  URL scheme resolution, functional options,     │
│  cross-cutting concerns (proxy, auth, dialer)   │
├─────────────────────────────────────────────────┤
│  x/transport.Transport                          │
│  Pack protocol: Handshake → Session             │
│  (all built-in transports implement this)       │
├─────────────────────────────────────────────────┤
│  x/transport.Connectable                        │
│  Raw connections: Connect → Conn                │
│  (SSH, Git TCP, file only — not HTTP)           │
├─────────────────────────────────────────────────┤
│  x/transport.Conn                               │
│  Transport-neutral: Reader, Writer, Close       │
└─────────────────────────────────────────────────┘
```

### Core types

```go
// Request carries only request semantics — no connection policy.
type Request struct {
    URL      *url.URL
    Command  string           // "git-upload-pack", "git-lfs-transfer", etc.
    Args     []string         // additional args after command and repo path
    Protocol protocol.Version
}

// Conn is a transport-neutral connection.
type Conn interface {
    io.Closer
    Reader() io.Reader
    Writer() io.WriteCloser
}

// Connectable is implemented by transports that can open raw full-duplex
// connections. SSH, Git TCP, and file implement this. HTTP does not.
type Connectable interface {
    Connect(context.Context, *Request) (Conn, error)
}

// Transport is the pack protocol interface. All built-in transports
// implement this.
type Transport interface {
    Handshake(ctx context.Context, req *Request) (Session, error)
}

// Session is a connected pack protocol session.
type Session interface {
    Capabilities() *capability.List
    GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
    Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
    Push(ctx context.Context, st storage.Storer, req *PushRequest) error
    Close() error
}
```

### Server-side functions

The transport package includes server-side implementations that run the Git protocol from the server's perspective:

```go
func UploadPack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, req *UploadPackRequest) error
func ReceivePack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, req *ReceivePackRequest) error
```

These are used by the file transport internally but are also available for building custom Git servers.

### HTTP transport options

The HTTP transport supports both smart and dumb protocols:

```go
type Options struct {
    Client     *http.Client
    Authorizer func(*http.Request) error
    HTTPProxy  func(*http.Request) (*url.URL, error)
    TLS        *tls.Config
    ForceDumb  bool // forces dumb HTTP protocol, bypassing smart detection
}
```

When `ForceDumb` is true, the transport skips the `?service=` query parameter in the `/info/refs` request and always treats the server as a dumb HTTP server. This provides backwards compatibility with `plumbing/transport/http.TransportOptions.UseDumb`.

### Client

```go
c := client.New(
    client.WithSSHAuth(keys),
    client.WithHTTPAuth(&xhttp.BasicAuth{Username: "u", Password: "p"}),
    client.WithProxyURL(proxyURL),
)

session, _ := c.Handshake(ctx, &transport.Request{
    URL:     remoteURL,
    Command: transport.UploadPackService,
})
```

The client is configured once with functional options and reused across operations. Auth, proxy, dialer, and transport overrides are all set at construction time. No mutable state after `New`.

Available options:

| Option | Purpose |
|--------|---------|
| `WithSSHAuth(SSHAuth)` | SSH authentication (keys, password, agent) |
| `WithHTTPAuth(HTTPAuth)` | HTTP authentication (basic, token) |
| `WithProxyURL(*url.URL)` | Explicit proxy for both HTTP and stream transports |
| `WithProxyEnvironment()` | Read proxy from `HTTP_PROXY`/`ALL_PROXY` environment |
| `WithDialer(DialContextFunc)` | Custom TCP dialer |
| `WithHTTPClient(*http.Client)` | Custom HTTP client |
| `WithInsecureSkipTLS()` | Skip TLS certificate verification |
| `WithCABundle([]byte)` | Custom CA certificate bundle |
| `WithLoader(Loader)` | Custom repository loader for file transport |
| `WithTransport(scheme, Transport)` | Register custom transport for a URL scheme |

### Authentication

Auth types are concrete structs with methods that directly match the transport option signatures. No shared interface, no runtime type assertions:

```go
// SSH — method value plugs directly into Options.ClientConfig
keys, _ := xssh.NewPublicKeysFromFile("git", "~/.ssh/id_ed25519", "")
tr := xssh.NewTransport(xssh.Options{ClientConfig: keys.ClientConfig})

// HTTP — method value plugs directly into Options.Authorizer
auth := &xhttp.BasicAuth{Username: "u", Password: "p"}
tr := xhttp.NewTransport(xhttp.Options{Authorizer: auth.Authorizer})
```

The client accepts small interfaces (`SSHAuth`, `HTTPAuth`) that the auth types already satisfy:

```go
c := client.New(
    client.WithSSHAuth(keys),                    // keys satisfies SSHAuth
    client.WithHTTPAuth(&xhttp.BasicAuth{...}),  // BasicAuth satisfies HTTPAuth
)
```

If the wrong auth type is passed, it fails at compile time — not at runtime.

## Side-by-side comparison

### Fetch

```go
// ─── Current ───
c, ep, _ := newClient(url, insecure, caBundle, proxyOpts)
s, _ := c.NewSession(storer, ep, auth)
conn, _ := s.Handshake(ctx, transport.UploadPackService)
refs, _ := conn.GetRemoteRefs(ctx)
conn.Fetch(ctx, &transport.FetchRequest{Wants: wants})
conn.Close()

// ─── New ───
session, _ := c.Handshake(ctx, &transport.Request{
    URL: remoteURL, Command: transport.UploadPackService,
})
refs, _ := session.GetRemoteRefs(ctx)
session.Fetch(ctx, storer, &transport.FetchRequest{Wants: wants})
session.Close()
```

Differences:
- No `Endpoint` — just `*url.URL`
- No `AuthMethod` parameter — auth lives on the client/transport
- Storer moves from session creation to `Fetch`/`Push` where it is actually used
- One fewer level of indirection (`NewSession` + `Handshake` collapses to `Handshake`)

### Push

```go
// ─── Current ───
c, ep, _ := newClient(url, insecure, caBundle, proxyOpts)
s, _ := c.NewSession(storer, ep, auth)
conn, _ := s.Handshake(ctx, transport.ReceivePackService)
refs, _ := conn.GetRemoteRefs(ctx)
conn.Push(ctx, &transport.PushRequest{Commands: cmds})
conn.Close()

// ─── New ───
session, _ := c.Handshake(ctx, &transport.Request{
    URL: remoteURL, Command: transport.ReceivePackService,
})
refs, _ := session.GetRemoteRefs(ctx)
session.Push(ctx, storer, &transport.PushRequest{Commands: cmds})
session.Close()
```

### Client construction

```go
// ─── Current ───
ep, _ := transport.NewEndpoint("ssh://host/repo.git")
ep.InsecureSkipTLS = true
ep.Proxy = transport.ProxyOptions{URL: "socks5://proxy:1080"}
c, _ := transport.Get(ep.Scheme)
s, _ := c.NewSession(storer, ep, &ssh.PublicKeys{User: "git", Signer: signer})

// ─── New ───
keys, _ := xssh.NewPublicKeysFromFile("git", "~/.ssh/id_ed25519", "")
proxyURL, _ := url.Parse("socks5://proxy:1080")
c := client.New(
    client.WithSSHAuth(keys),
    client.WithInsecureSkipTLS(),
    client.WithProxyURL(proxyURL),
)
```

## What the new API makes possible

### Raw connections

The `Connectable` interface enables protocols beyond pack operations. Any command can be run over SSH, Git TCP, or file transports:

```go
conn, err := c.Connect(ctx, &transport.Request{
    URL:     remoteURL,
    Command: "my-custom-tool",
    Args:    []string{"--flag", "value"},
})
```

This is not possible with the current API because `Session.Handshake` always expects a pack protocol service. `Commander.Command` exists internally but is not exposed through the public transport interface.

### git-upload-archive (proposed)

The architecture supports `git-upload-archive` through both raw connections and a higher-level API. The following types and functions are proposed but not yet finalized:

```go
// Archivable is an optional Session capability for git-upload-archive.
type Archivable interface {
    Archive(ctx context.Context, req *ArchiveRequest) (io.ReadCloser, error)
}

// ArchiveRequest describes a git-upload-archive request.
type ArchiveRequest struct {
    Args     []string          // git-archive arguments
    Progress sideband.Progress // optional progress output
}

// UploadArchive runs the server-side upload-archive service.
func UploadArchive(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, req *UploadArchiveRequest) error

// UploadArchiveRequest configures the server-side upload-archive service.
type UploadArchiveRequest struct {
    // AllowUnreachable disables the default security restriction that only
    // allows direct ref names. The repository-level config
    // uploadArchive.allowUnreachable overrides this value when set.
    AllowUnreachable bool
}
```

Stream transports (SSH, Git TCP, file) can implement `Archivable` through the existing `Conn` abstraction. HTTP transports would speak the same wire protocol by POSTing to `/git-upload-archive`. The server-side `UploadArchive` function supports tar, tar.gz, tgz, and zip formats with path filtering and prefix support.

Low-level raw connection usage is also possible:

```go
conn, err := c.Connect(ctx, &transport.Request{
    URL:     remoteURL,
    Command: transport.UploadArchiveService,
})
defer conn.Close()

rc, err := transport.Archive(ctx, conn.Writer(), conn.Reader(), &transport.ArchiveRequest{
    Args: []string{"--format=tar.gz", "HEAD"},
})
io.Copy(outputFile, rc)
```

### Git protocol v2

Protocol v2 changes the handshake — the server advertises capabilities without refs, and the client explicitly requests refs via `ls-refs`. The current API declares protocol support as a transport-wide property (`SupportedProtocols() []protocol.Version`), but v2 negotiation belongs in the pack session layer, not the transport.

```go
session, err := c.Handshake(ctx, &transport.Request{
    URL:      remoteURL,
    Command:  transport.UploadPackService,
    Protocol: protocol.V2,
})
```

`Request.Protocol` lets each transport set the right wire-level hint — `GIT_PROTOCOL=version=2` on SSH, protocol version in the Git TCP request line, `Git-Protocol` HTTP header — without the transport interface itself needing to know about protocol versions.

### Git LFS — SSH authenticate

`git-lfs-authenticate` is an SSH command that returns JSON with an HTTP endpoint and auth token. It is not a pack protocol operation.

```go
conn, err := c.Connect(ctx, &transport.Request{
    URL:     remoteURL,
    Command: "git-lfs-authenticate",
    Args:    []string{"download"},
})
if err != nil {
    return err
}
defer conn.Close()
conn.Writer().Close()

var lfsAuth struct {
    Href   string            `json:"href"`
    Header map[string]string `json:"header"`
}
json.NewDecoder(conn.Reader()).Decode(&lfsAuth)
```

### Git LFS — SSH transfer

`git-lfs-transfer` is a long-running SSH command that speaks the LFS transfer protocol over a bidirectional stream:

```go
conn, err := c.Connect(ctx, &transport.Request{
    URL:     remoteURL,
    Command: "git-lfs-transfer",
    Args:    []string{"download"},
})
if err != nil {
    return err
}
defer conn.Close()

w := conn.Writer()
fmt.Fprintf(w, "version 1\n")
// ... read/write LFS transfer packets over conn
```

## Comparison tables

### Separation of concerns

| Concern | Current API | New API |
|---------|-------------|---------|
| URL identity | `Endpoint` (mixed with TLS/proxy policy) | `*url.URL` in `Request` |
| TLS policy | `Endpoint.InsecureSkipTLS`, `Endpoint.CaBundle` | `http.Options.TLS` / `WithInsecureSkipTLS` / `WithCABundle` |
| Proxy | `Endpoint.Proxy` (custom `ProxyOptions` type) | `WithProxyURL` / `WithProxyEnvironment` |
| Auth | `AuthMethod` (shared interface, runtime casts) | Transport-specific types, compile-time safe |
| Protocol version | `Transport.SupportedProtocols()` | `Request.Protocol` (per request) |
| Storage | Bound at `NewSession` | Passed to `Fetch`/`Push` |
| Pack protocol | Baked into `Transport` | `Session` layer above `Conn` |
| Raw streams | Not possible | `Connectable.Connect` |
| Dumb HTTP | `TransportOptions.UseDumb` | `Options.ForceDumb` |

### Standard library alignment

| Concept | Current API | New API |
|---------|-------------|---------|
| HTTP client | Implicit | `*http.Client` via `WithHTTPClient` |
| TLS | `Endpoint.InsecureSkipTLS`, `Endpoint.CaBundle` | `*tls.Config` via `WithInsecureSkipTLS` / `WithCABundle` |
| HTTP proxy | `ProxyOptions{URL, Username, Password}` | `http.ProxyURL(u)` / `http.ProxyFromEnvironment` |
| Stream proxy | Implicit `proxy.Dial` | `proxy.FromURL(u, forward)` via `WithProxyURL` |
| SSH config | Custom `AuthMethod` interface | `*ssh.ClientConfig` (standard `x/crypto` type) |
| Dialer | Not configurable | `transport.DialContextFunc` via `WithDialer` |
| URL | `Endpoint` (custom type embedding `url.URL`) | `*url.URL` (standard library) |

### Capability modeling

The `Connectable` interface is a compile-time capability check:

```go
if conn, ok := tr.(transport.Connectable); ok {
    // SSH, Git TCP, file — can run arbitrary commands
    raw, _ := conn.Connect(ctx, req)
} else {
    // HTTP — pack protocol only
    return transport.ErrConnectUnsupported
}
```

The current API has no equivalent. All transports expose the same interface, and capability differences are only discovered at runtime through errors.

## Migration path

`plumbing/transport` is alpha and unreleased. There are no stability guarantees and no external consumers to worry about. Migration is a direct replacement.

### Phase 1: Parallel availability (current state)

Both APIs exist side by side. `plumbing/transport` is used by `remote.go` and related callers. `x/transport` and `x/client` are fully implemented and tested but not yet wired into the core.

### Phase 2: Rewire callers and remove plumbing/transport

Replace all `plumbing/transport` usage in `remote.go` and related callers:

```go
// Before
c, ep, _ := newClient(url, insecure, caBundle, proxyOpts)
s, _ := c.NewSession(storer, ep, auth)
conn, _ := s.Handshake(ctx, transport.UploadPackService)

// After
session, _ := r.client.Handshake(ctx, &transport.Request{
    URL:     remoteURL,
    Command: transport.UploadPackService,
})
```

`Remote` would hold a `*client.Client` instead of reconstructing transport, endpoint, and auth on every operation.

Since `plumbing/transport` has never been released as stable, it can be removed in the same change. No deprecation period is needed.

## Implementation status

All core code is implemented and tested:

| Package | Tests | Status |
|---------|-------|--------|
| `x/transport` | 31 tests (pack negotiation, fetch/push, version, loader, server info, update requests) | Passing |
| `x/transport/ssh` | 24 tests (2 suite runners × 19+24 suite methods, 19 auth, 3 transport) | Passing |
| `x/transport/http` | 20 tests (2 suite runners × 19+24 suite methods, 1 dumb suite × 6 active methods, 4 auth, 6 TLS, 7 common) | Passing |
| `x/transport/git` | 5 tests (2 suite runners, 3 transport) | Passing |
| `x/transport/file` | 11 tests (2 suite runners, 5 transport, 4 integration) | Passing |
| `x/client` | 19 tests (options, schemes, composition) | Passing |

### Test gaps to port from plumbing/transport

The following test areas from `plumbing/transport` have not yet been ported to `x/transport`. Some are structurally replaced (e.g. `Endpoint` and `Registry` tests have no equivalent because those concepts don't exist), but others represent real coverage gaps:

| Area | Missing tests | Notes |
|------|--------------|-------|
| **HTTP proxy** | `TestAdvertisedReferencesHTTP`, `TestAdvertisedReferencesHTTPS` | Full proxy integration test with MITM test proxy — needs porting |
| **HTTP redirect** | `TestAdvertisedReferencesRedirectPath`, `TestAdvertisedReferencesRedirectSchema` | Redirect handling is implemented but not tested |
| **SSH proxy** | `TestCommand` (SOCKS5 proxy test) | SOCKS5 proxy integration test — needs porting |
| **SSH config** | `TestOverrideConfig`, `TestOverrideConfigKeep`, `TestDefaultSSHConfig*`, `TestInvalidSocks5Proxy` | SSH config override behavior — partially covered by new auth tests |

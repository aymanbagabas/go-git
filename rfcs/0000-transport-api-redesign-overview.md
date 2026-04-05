# Transport API redesign — implementation overview

This document explains how the transport API redesign described in `rfcs/0000-transport-api-redesign.md` replaces the current transport types on the main branch and documents the implementation as built in `x/transport`.

Unlike the RFC, this document is implementation-facing. It focuses on mapping old concepts to new ones, documenting the actual package layout, and describing how each transport works.

## Current main-branch model (being replaced)

The current transport design is centered around pack protocol sessions.

At the public transport layer:

- `plumbing/transport/transport.go` defines `Transport` as:
  - `NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)`
  - `SupportedProtocols() []protocol.Version`
- `plumbing/transport/transport.go` defines a transport-neutral `AuthMethod`
- `plumbing/transport/transport.go` defines `Endpoint` as `url.URL` plus transport policy fields such as TLS and proxy options
- `plumbing/transport/transport.go` defines `ProxyOptions` as a go-git-specific URL/username/password bag

At the lower-level pack transport layer:

- `plumbing/transport/common.go` defines `Session` as a handshake-oriented pack session
- `plumbing/transport/common.go` defines `Commander` and `Command`
- `plumbing/transport/common.go` defines `NewPackTransport` as an adapter from `Commander` to `Transport`

At the registry layer:

- `plumbing/transport/registry.go` registers scheme implementations with `Register(protocol string, c Transport)`

## New model as implemented

The new `x/transport` package implements a layered model with clear separation between transport-level and pack-protocol-level concerns.

### Core types

| Type | Package | Purpose |
|------|---------|---------|
| `Request` | `x/transport` | Lean request: URL + command + args + protocol version |
| `Conn` | `x/transport` | Transport-neutral connection: `Reader()`, `Writer()`, `Close()` |
| `Connectable` | `x/transport` | Optional capability: `Connect(ctx, *Request) (Conn, error)` |
| `Transport` | `x/transport` | Pack protocol: `Handshake(ctx, *Request) (Session, error)` |
| `Session` | `x/transport` | Pack session: `Capabilities()`, `GetRemoteRefs()`, `Fetch()`, `Push()`, `Close()` |
| `FetchRequest` | `x/transport` | Parameters for fetch-pack |
| `PushRequest` | `x/transport` | Parameters for send-pack |
| `Client` | `x/client` | URL scheme resolution and transport dispatch |

### Design decision: collapsed pack adapter layer

The original RFC proposed two layers:

1. `Transport.Open(ctx, *Request) → Session` (transport-neutral exchange)
2. `PackClient.Handshake(ctx, url, service) → PackSession` (pack-aware adapter)

The implementation collapsed these into one:

1. `Transport.Handshake(ctx, *Request) → Session` (directly pack-aware)

This simplification was made because all current transports speak the Git pack protocol. The transport-neutral exchange primitive is `Conn` (from `Connectable.Connect`), which serves the non-pack use case.

If non-pack transports are needed in the future, `Connectable.Connect` already provides the escape hatch.

## Old-to-new type mapping

### `transport.Transport`

| Current | New |
|---------|-----|
| `NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)` | `Handshake(context.Context, *Request) (Session, error)` |
| `SupportedProtocols() []protocol.Version` | Removed — protocol version is in `Request.Protocol` |

### `transport.Session` (old) → `Session` (new)

| Current (old) | New |
|---------------|-----|
| `Handshake(ctx, service) (Connection, error)` | Absorbed into `Transport.Handshake` |
| `Connection.Capabilities()` | `Session.Capabilities()` |
| `Connection.GetRemoteRefs(ctx)` | `Session.GetRemoteRefs(ctx)` |
| `Connection.Fetch(ctx, req)` | `Session.Fetch(ctx, storer, req)` |
| `Connection.Push(ctx, req)` | `Session.Push(ctx, storer, req)` |
| `Connection.Close()` | `Session.Close()` |

Key change: `Fetch` and `Push` now accept `storage.Storer` directly, moving the storer dependency from session creation to the operations that need it.

### `transport.Commander` / `transport.Command`

| Current | New |
|---------|-----|
| `Commander.Command(ctx, cmd, ep, auth, params)` → `Command` | `Connectable.Connect(ctx, *Request)` → `Conn` |
| `Command` (started process with stdio pipes) | `Conn` (Reader/Writer/Close) |

### `transport.AuthMethod`

| Current | New |
|---------|-----|
| Common `AuthMethod` interface | Removed from core package |
| HTTP auth | `http.Options.Authorizer func(*http.Request) error` |
| SSH auth | `ssh.Options.ClientConfig func(context.Context, *Request) (*ssh.ClientConfig, error)` |

### `transport.Endpoint`

| Current | New |
|---------|-----|
| `*Endpoint` (URL + TLS + proxy) | `*url.URL` in `Request.URL` |
| `InsecureSkipTLS`, `CaBundle` | Configure on `http.Options.Client.Transport` |
| `Proxy` | `http.Options.HTTPProxy` |

### `transport.ProxyOptions`

| Current | New |
|---------|-----|
| `ProxyOptions{URL, Username, Password}` | `http.Options.HTTPProxy func(*http.Request) (*url.URL, error)` |

### `transport.Register`

| Current | New |
|---------|-----|
| Global `Register(scheme, transport)` | `Client.RegisterTransport(scheme, transport)` in `x/client` |
| Global `InstallProtocol` | Built-in schemes resolved lazily from `Options` |

## Package layout

```
x/
├── transport/
│   ├── doc.go              # Package documentation
│   ├── transport.go        # Conn, Connectable interfaces
│   ├── pack_session.go     # Transport, Session interfaces
│   ├── request.go          # Request type
│   ├── errors.go           # Error variables
│   ├── common.go           # DialContextFunc, FetchRequest, PushRequest, RemoteError
│   ├── service.go          # Service constants (UploadPackService, etc.)
│   ├── protocol.go         # GitProtocolEnv, DiscoverVersion
│   ├── version.go          # Protocol version utilities
│   ├── fetch.go            # FetchPack logic
│   ├── push.go             # SendPack logic
│   ├── negotiate.go        # Pack negotiation
│   ├── pack_stream.go      # Pack stream handling
│   ├── session_stream.go   # Stream-backed Session implementation
│   ├── upload_pack.go      # Upload-pack adapter
│   ├── receive_pack.go     # Receive-pack adapter
│   ├── loader.go           # MapLoader for in-process serving
│   ├── serve.go            # Server-side transport serving
│   ├── serverinfo.go       # Server info parsing
│   ├── file/               # File transport (Transport + Connectable)
│   │   ├── file.go
│   │   ├── handshake.go
│   │   ├── file_test.go
│   │   ├── file_integration_test.go
│   │   ├── upload_pack_test.go
│   │   └── receive_pack_test.go
│   ├── git/                # Git TCP transport (Transport + Connectable)
│   │   ├── git.go
│   │   ├── handshake.go
│   │   ├── git_test.go
│   │   └── pack_test.go
│   ├── ssh/                # SSH transport (Transport + Connectable)
│   │   ├── ssh.go
│   │   ├── handshake.go
│   │   ├── ssh_test.go
│   │   ├── pack_test.go
│   │   ├── knownhosts/
│   │   └── sshagent/
│   ├── http/               # HTTP transport (Transport only)
│   │   ├── http.go
│   │   ├── handshake.go
│   │   ├── common.go
│   │   ├── dumb.go
│   │   ├── http_test.go
│   │   ├── upload_pack_test.go
│   │   └── receive_pack_test.go
│   └── test/               # Shared test suites
│       ├── upload_pack.go
│       └── receive_pack.go
└── client/
    ├── client.go           # Client: scheme resolution + dispatch
    └── client_test.go
```

## Transport implementation details

### Stream transports (SSH, Git TCP, File)

All three implement both `Transport` and `Connectable`.

The `Connectable.Connect` path:
1. Opens a raw connection (SSH channel, TCP socket, local process pipes)
2. Sends the initial protocol request line (command + repository path)
3. Returns a `Conn` wrapping the connection's stdin/stdout

The `Transport.Handshake` path:
1. Calls `Connect` internally
2. Reads the advertised refs and capabilities from the stream
3. Wraps the connection state in a `Session` (via `NewStreamSession` helper)

The `Session` returned by stream transports holds the open connection. `Fetch` and `Push` continue using the same stream for the pack negotiation and data transfer phases.

### HTTP transport

HTTP implements only `Transport`.

The `Transport.Handshake` path:
1. GET `/info/refs?service=<command>` to discover refs
2. Detect smart vs dumb HTTP from `Content-Type` header
3. Parse advertised refs and capabilities
4. Return a `smartPackSession` or `dumbPackSession`

Smart HTTP `Fetch`/`Push`:
- Uses a buffered `httpRequester` that collects writes, then fires a POST to the service endpoint on first `Read` or `Close`
- Request/response is modeled as a single HTTP round-trip per operation

Dumb HTTP `Fetch`:
- Walks the object graph by fetching individual loose objects via `GET /objects/<hash>`
- Downloads pack index files and pack files as needed
- Does not support `Push`

## Test infrastructure

### Shared test suites (`x/transport/test/`)

Two reusable test suites exercise the full pack protocol:

- `UploadPackSuite`: 15 tests covering advertised refs, capabilities, fetch, partial fetch, multi-want, context cancellation, no-change detection
- `ReceivePackSuite`: 14 tests covering advertised refs, capabilities, send-pack on empty/non-empty repos, report-status, add/delete refs, context cancellation

Each transport wires these suites in its own `*_test.go` files with transport-specific setup (SSH server, HTTP server with `git http-backend`, Git daemon, local filesystem).

### HTTP test server

HTTP tests use `net/http/cgi` with `git http-backend`:

```go
server := &http.Server{
    Handler: &cgi.Handler{
        Path: filepath.Join(gitExecPath, "git-http-backend"),
        Env:  []string{"GIT_HTTP_EXPORT_ALL=true", "GIT_PROJECT_ROOT=" + base},
    },
}
```

### SSH test server

SSH tests use `gliderlabs/ssh` with an in-process handler that spawns Git subprocesses. The handler uses `wg.Add(2)` for stdout/stderr copies only — stdin runs in a separate goroutine to avoid deadlock.

## Call-site migration guide

The current shape:

```go
ep, _ := transport.NewEndpoint("ssh://host/repo.git")
session, _ := client.NewSession(storer, ep, auth)
conn, _ := session.Handshake(ctx, transport.UploadPackService)
refs, _ := conn.GetRemoteRefs(ctx)
conn.Fetch(ctx, fetchReq)
conn.Close()
```

Becomes:

```go
req := &transport.Request{
    URL:     &url.URL{Scheme: "ssh", Host: "host", Path: "/repo.git"},
    Command: transport.UploadPackService,
}
session, _ := tr.Handshake(ctx, req)
refs, _ := session.GetRemoteRefs(ctx)
session.Fetch(ctx, storer, fetchReq)
session.Close()
```

Or via the client:

```go
c := client.New(client.Options{
    SSH: ssh.Options{
        ClientConfig: mySSHConfigProvider,
    },
})
session, _ := c.Handshake(ctx, req)
// ...
```

## What remains to be done

### Completed

- [x] Core transport types (`Request`, `Conn`, `Connectable`, `Transport`, `Session`)
- [x] SSH transport with full test suite
- [x] Git TCP transport with tests
- [x] File transport with tests
- [x] HTTP transport (smart + dumb) with full test suite
- [x] Shared test suites (`UploadPackSuite`, `ReceivePackSuite`)
- [x] `x/client` with scheme resolution and dispatch
- [x] Pack negotiation and fetch/push logic

### Remaining

- [ ] Rewire `remote.go` and related callers to use `x/transport` / `x/client`
- [ ] Decide on `Client` immutability (functional options vs mutable `RegisterTransport`)
- [ ] Add `DialOptions` and `ProxyOptions` to `Client` configuration
- [ ] Add per-call overrides (`CallOptions`) if needed
- [ ] Implement `git-upload-archive` support via `Connectable.Connect`
- [ ] Build SSH-based LFS adapters (`git-lfs-authenticate`, `git-lfs-transfer`)
- [ ] Build HTTP-based LFS adapters (Batch API, basic transfer)
- [ ] Remove or deprecate old `plumbing/transport` API
- [ ] Migrate endpoint parsing helpers to produce `*url.URL`

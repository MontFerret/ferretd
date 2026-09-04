# ferretd

`ferretd` is the experimental long-running developer service for
[Ferret](https://github.com/MontFerret/ferret). It is intended to coordinate
language tooling, workspaces, execution sessions, and debug sessions for CLI,
Lab, and editor integrations.

The repository contains a local gRPC daemon with process-local Ferret source
workspaces and execution sessions, a supported Go client, an experimental
language server, and a single-session Debug Adapter Protocol (DAP) server over
stdio. The Ferret VM, compiler, runtime, debugger semantics, and language
semantics remain owned by the main Ferret project.

## Build

Requires Go 1.26.1 or newer.

```sh
make build
```

The binary is written to `bin/ferretd`. Release builds can override the default
development version:

```sh
make build VERSION=v0.1.0
```

## Release

Create and push a SemVer release tag from a clean working tree:

```sh
make release v0.1.0
```

The tag triggers the GoReleaser workflow, which builds the supported platform
archives and creates a draft GitHub release. Review its artifacts and release
notes before publishing it manually.

## Test

```sh
make test
make lint
make generate
make proto-lint
```

## Commands

```sh
./bin/ferretd --version
./bin/ferretd serve
./bin/ferretd serve --endpoint unix:///tmp/ferretd.sock
FERRETD_AUTH_TOKEN="$(openssl rand -base64 32)" \
  ./bin/ferretd serve \
  --endpoint tcp://127.0.0.1:0 \
  --auth-token-env FERRETD_AUTH_TOKEN
./bin/ferretd serve --log-level debug
./bin/ferretd lsp
./bin/ferretd dap
./bin/ferretd dap --log-level debug
```

`serve` starts the local daemon and waits for an interrupt or a `Shutdown` RPC.
It uses `$XDG_RUNTIME_DIR/ferret/ferretd.sock` on macOS and Linux, falling back
to the user cache directory, and `\\.\pipe\ferretd` on Windows. Explicit local
endpoints use `unix:///absolute/path` or `npipe:////./pipe/name`. These native
transports remain unauthenticated and are the defaults.

An integration that cannot use a native endpoint may opt into authenticated
loopback TCP. The server accepts only `tcp://127.0.0.1:0`, always lets the OS
assign the port, and requires `--auth-token-env` to name an environment variable
containing a nonempty bearer token. Every unary and streaming RPC, including
health RPCs, requires that credential. After listening, `serve` writes one
stable readiness diagnostic to stderr containing the actual endpoint:

```json
{"event":"ferretd.ready","endpoint":"tcp://127.0.0.1:49152","version":"...","message":"ferretd started"}
```

Clients must parse the reported nonzero port. Hostnames, other addresses,
configured nonzero listener ports, unauthenticated TCP, TLS, and remote access
are intentionally unsupported. The readiness handshake is not filtered by
`--log-level`, and the token is never included in diagnostics.

The `serve` and `dap` commands write newline-delimited JSON diagnostics to
stderr at `info` level by default. Their shared `--log-level` option accepts
`debug`, `info`, `warn`, or `error`.

The supported Go client discovers the default endpoint, performs API
compatibility negotiation, and exposes daemon, workspace, and execution
operations:

```go
c, err := client.Dial(ctx)
if err != nil {
	return err
}
defer c.Close()

info, err := c.Info(ctx)
workspace, err := c.Workspaces().Open(ctx, projectRoot)
session, err := c.Executions().CreateSession(ctx, client.CreateSessionRequest{
	WorkspaceID:  workspace.ID,
	RelativePath: "main.fql",
})
execution, err := c.Executions().CreateExecution(ctx, client.CreateExecutionRequest{
	SessionID:  session.ID,
	Parameters: map[string]any{"url": "https://example.com"},
	Options: client.ExecutionOptions{
		// runtimeRoot is an existing absolute directory and may be outside projectRoot.
		WorkingDirectory: runtimeRoot,
	},
})
watcher, err := c.Executions().WatchExecution(ctx, execution.ID)
running, err := c.Executions().RunExecution(ctx, execution.ID)
```

`WorkingDirectory` is optional. When omitted, the Ferret runtime session uses
the workspace-rooted filesystem associated with the compiled Session. When
supplied, it must resolve to an existing absolute local directory; it may be
outside the workspace and is retained canonically in Execution snapshots.

For the opt-in TCP transport, parse the endpoint from the `ferretd.ready` event
and pass the same token explicitly:

```go
endpoint, err := client.ParseEndpoint(reportedEndpoint)
c, err := client.Dial(
	ctx,
	client.WithEndpoint(endpoint),
	client.WithBearerToken(token),
)
```

The client rejects TCP endpoints without a bearer token and rejects bearer-token
configuration for native endpoints before opening a connection.

Opening a workspace recursively discovers lowercase `.fql` files, loads their
contents, and retains daemon-owned documents with Ferret syntax state and
diagnostics. No Ferret project manifest is required. While the workspace remains
open, it tracks eligible files and directories created, changed, deleted, or
renamed on disk. Initial discovery, dynamic tracking, and Session admission all
apply the same root boundary, nested-module, directory-exclusion, and symlink
rules.

Workspace state is in memory for the daemon process. Reopening the same cleaned
absolute root returns the same workspace ID, closing a client connection does
not close its workspaces, and `Close` is explicit and idempotent. The current
workspace RPC continues to expose identity and lifecycle operations rather than
documents or parser internals.

Each open workspace owns a Ferret engine with a read-write filesystem rooted at
the workspace directory. `CreateSession` refreshes and compiles the latest saved
contents of one eligible workspace-relative `.fql` document into an immutable
reusable plan. A missed creation notification is recovered during this refresh;
excluded or escaping paths remain rejected. Existing Sessions keep their
original source revision and normal and lazy debug Plans. Each `Execution` owns
isolated JSON-shaped parameter bindings and a fresh, one-shot Ferret runtime
session. Its optional working directory changes only that runtime session's
rooted filesystem; compilation and source containment remain workspace-owned.
`RunExecution` returns the `RUNNING` snapshot immediately; execution then
continues independently of the triggering RPC context. Clients can observe the
latest lifecycle event and subsequent events with `WatchExecution`, cancel an
active execution, and retrieve terminal output or failure details until they
explicitly close the resource. Closing a workspace cascades through its Sessions
and Executions. DAP debug Sessions are independent retained children whose
lifetime is coordinated by the protocol-neutral debug manager.

`lsp` starts the experimental language server over stdin and stdout. It opens
the local roots supplied by LSP initialization, uses their tracked workspace
documents as a baseline, and gives versioned editor overlays precedence while
documents are open. Analysis snapshots are coalesced and cached per URI.

`dap` starts a protocol-pure, single-session debug adapter over stdin and
stdout. It launches one local `.fql` program, opens its workspace in-process,
and delegates breakpoints, stepping, frame inspection, variables, and
evaluation to Ferret through separate transport-neutral execution and debug
managers. It does not connect to `ferretd serve` or expose debugging through
gRPC. Process diagnostics remain on stderr; `--log-level debug` enables concise
semantic DAP request, response, and event tracing without logging query text,
parameters, expressions, or evaluated values. See
[docs/dap.md](docs/dap.md) for launch arguments and supported requests.

## Current Status

The daemon exposes API v1.2 `DaemonService`, `WorkspaceService`, and
`ExecutionService` contracts over permission-restricted native local transports
or authenticated ephemeral IPv4 loopback TCP, plus the standard gRPC health
service. The checked-in Go code under `gen/` is generated from `proto/` with
pinned Buf and protobuf tools. The debug protobuf remains an ungenerated
placeholder.

Daemon workspaces retain deterministically discovered source files, source
contents, Ferret parse trees, and syntax diagnostics, and track eligible
filesystem changes while open. Session creation defensively discovers or
refreshes its selected target. The language server uses the shared workspace
manager for saved-source baselines and supports
opening, changing, and closing `.fql` editor overlays with full-document
synchronization. Its navigation and references are document-local.

Debug protobuf generation, debug gRPC/client APIs, incremental synchronization,
cross-file indexing, module resolution, workspace persistence, remote daemon
operation, and LSP-over-gRPC are not implemented. DAP remains single-session
stdio only.
Execution sessions do not add queues, durable replay, persistence, background
automatic recompilation, or REPL state.

See [the development architecture](docs/development/architecture.md) for the
implemented subsystem boundaries and [docs/lsp.md](docs/lsp.md) for experimental
editor setup.

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

Execution uses the
[Universal Runtime API](https://github.com/MontFerret/api). Each daemon or DAP
service graph transfers one shared native root from Ferret's upstream
`uapi.New` adapter to its own runtime acquisition owner. Each compiled Session
owns one selected runtime binding and its reusable plans; individual runs own
runtime sessions and live output handles. The execution manager borrows the
composition owner and releases Session bindings after their children and plans. Runs collect detached content and settle output and session
cleanup before publishing completion.

## Build

Requires Go 1.26.1 or newer.

```sh
make build
```

`make build`, `make lint`, and `make fmt` automatically install the pinned
golangci-lint release into `bin/` when it is missing. The first invocation
requires download access, curl, and a POSIX shell (Git Bash on Windows).
`make install-lint` optionally installs it ahead of time; existing installations
are reused. `make install-tools` also installs the protobuf tools needed for
generation.

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

`make lint` validates the golangci-lint configuration and checks handwritten Go
code, including tests, for correctness, formatting, and repository style.
`make fmt` applies the configured Go formatting and import grouping. See
[the development guide](docs/development/release.md#go-linting-and-formatting)
for rule coverage and intentional exceptions.

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
./bin/ferretd serve --runtime-config /absolute/path/runtimes.json
./bin/ferretd lsp
./bin/ferretd dap
./bin/ferretd dap --log-level debug
./bin/ferretd dap --runtime-config /absolute/path/runtimes.json
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

`WorkingDirectory` is optional. Native Sessions default to the workspace root;
an override must resolve to an existing absolute local directory and is retained
canonically. Wire Sessions inherit the hosted runtime's filesystem default when
omitted. A nonblank Wire override is forwarded exactly as a runtime-host path,
without daemon-side path normalization or existence checks; validation occurs on
the runtime host during session creation or use. Source loading always uses the
daemon workspace, independently of this execution filesystem.

Select a configured application runtime when creating a Session (API 1.3+):

```go
session, err := c.Executions().CreateSession(ctx, client.CreateSessionRequest{
    WorkspaceID:  workspace.ID,
    RelativePath: "query.fql",
    RuntimeName:  "application",
})
```

`CreateSession` and `GetSession` report the resolved `Session.RuntimeName`.
Executions and lazy debug compilation reuse that binding and the original source.
Connection loss fails operations on existing Sessions; creating a new Session
acquires afresh. Source names do not synchronize remote files or remap paths.

Protocol clients must remember the negotiated server version and reject every
explicit selector, including `local`, below API 1.3: an older protobuf server can
ignore the field. API 1.3+ responses must report a nonempty name, and explicit
selection must be acknowledged identically. Reject missing/mismatched names
without retrying creation; attempt bounded detached cleanup when the rejected
response contains a Session ID. The Go client uses a five-second cleanup timeout.
Older compatible 1.x servers still support unselected calls; their empty name
means unreported. Runtime lookup/acquisition failures expose `ErrRuntimeNotFound`
and `ErrRuntimeUnavailable`, separately from daemon availability, source failures,
and `CompilationError`. Compilation/preparation failures retain diagnostics when
available and otherwise a readable cause.

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

### Runtime definitions and discovery

Both `serve` and standalone `dap` accept the same optional `--runtime-config`
JSON file:

```json
{
  "defaultRuntime": "local",
  "runtimes": [
    {
      "name": "application",
      "label": "My application",
      "provider": "wire",
      "endpoint": "tcp://127.0.0.1:9000",
      "insecure": true,
      "connectTimeout": "5s"
    }
  ]
}
```

The built-in `local` target (`Built-in Ferret`, provider `native`) is always
implicit and cannot be replaced. Additional definitions must use provider `wire`.
Names are unique, case-sensitive, nonempty, and have no surrounding whitespace.
An omitted or empty label uses the name; an omitted connection timeout uses
`5s`. Supplied timeouts must be positive duration strings. Wire endpoints must
use `tcp://host:port`, with a hostname or IP and a numeric port in `1..65535`.
Bracketed IPv6 is supported; userinfo, paths, queries, and fragments are rejected.
Syntax is validated without connecting, so unreachable targets remain valid.

`insecure` is an optional boolean, defaulting to `false`. Omission or `false`
uses Wire's TLS defaults with system trust and server identity verification;
`true` explicitly selects plaintext. The development example opts into plaintext.
Loopback addresses do not imply plaintext, and TLS failure never retries with
weaker transport. Explicit null and nonboolean values are invalid.

Internally, each acquisition of a Wire definition creates a fresh client and
owned channel, even for the same target. Native bindings borrow one shared root.
The connection timeout bounds startup; bounded detached Wire rollback can take
additional time after cancellation. Cancelling startup after successful
acquisition does not close the binding. Callers settle outputs, sessions/debuggers,
and plans before releasing a binding; owner shutdown closes remaining roots
after child services settle. Definitive connection loss does not revive or replay
an existing binding; a new explicit acquisition performs a fresh handshake.

Without a file, the catalog contains only `local`. Omitting `defaultRuntime`
selects `local`; explicit blank or unknown defaults fail startup.
`serve` accepts Wire defaults without connecting at startup or during discovery;
`CreateSession` performs acquisition. Standalone `dap` still requires a native
default and rejects Wire defaults before runtime allocation. Unused Wire targets
never connect.

Explicit missing, unreadable, malformed, or invalid files fail startup. The
loader requires a JSON object and rejects unknown fields, null typed fields,
invalid field types, and trailing JSON values. Relative file paths use the
process working directory. Each service graph reads its file once and owns an
immutable catalog snapshot. Pass the same absolute path to both commands for
consistent definitions; DAP still constructs independent services and a runtime.
Configuration is not reloaded, discovered from workspaces, or persisted through
RPCs. The incoming daemon `--endpoint` and `--auth-token-env` options remain
separate from these outbound target definitions.

Use the supported Go client to discover metadata:

```go
catalog, err := c.ListRuntimes(ctx)
if err != nil {
	return err
}
fmt.Println(catalog.DefaultRuntime)
for _, target := range catalog.Runtimes {
	fmt.Println(target.Name, target.Label, target.Provider)
}
```

Discovery returns definitions ordered by name and one authoritative default
name. It does not dial endpoints, expose connection settings, or assert that a
target is reachable, executable, or debug-capable. A compatible older 1.x daemon
can still serve existing operations; unsupported discovery returns an error
matching `client.ErrUnsupportedOperation` without fabricating definitions.

Opening a workspace recursively discovers lowercase `.fql` files, loads their
contents, and retains daemon-owned documents with Ferret syntax state and
diagnostics. No Ferret project manifest is required. While the workspace remains
open, it tracks eligible files and directories created, changed, deleted, or
renamed on disk. Initial discovery and automatic dynamic tracking
apply the same root boundary, nested-module, directory-exclusion, and symlink
rules.

Workspace state is in memory for the daemon process. Reopening the same cleaned
absolute root returns the same workspace ID, closing a client connection does
not close its workspaces, and `Close` is explicit and idempotent. The current
workspace RPC continues to expose identity and lifecycle operations rather than
documents or parser internals.

`CreateSession` resolves its optional `RuntimeName` exactly through the startup
catalog; omission selects the daemon default. It first refreshes the latest saved contents of one explicitly selected workspace-relative `.fql`
document, acquires that runtime, and compiles there into an immutable reusable
plan. The Session retains its resolved name, binding, and source bytes until
close. A missed creation notification
is recovered during this refresh. Explicit selection also admits lowercase `.fql`
regular files beneath discovery-excluded directories, including `.tmp`, `testdata`,
and nested Go modules, without discovering neighboring excluded files.
Containment and nested-symlink restrictions remain. These admissions survive
watcher reconciliation and same-path recreation until workspace close.
Existing Sessions keep their original source revision and normal and lazy debug
Plans. Each `Execution` owns isolated JSON-shaped parameter bindings and a fresh,
one-shot runtime session. Native filesystem defaults use the workspace root;
Wire filesystem defaults belong to the hosted runtime. An optional working
directory changes only that runtime session's root. Source
containment and retained state remain workspace-owned.
`RunExecution` returns the `RUNNING` snapshot immediately; execution then
continues independently of the triggering RPC context. It remains running until
output collection and owned cleanup settle. Retained output preserves absent
versus present-empty content and available content alongside failures, using the
existing public content-type and data fields. Clients can observe the
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

The daemon exposes API v1.3 `DaemonService`, `WorkspaceService`, and
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

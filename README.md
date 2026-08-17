# ferretd

`ferretd` is the experimental long-running developer service for
[Ferret](https://github.com/MontFerret/ferret). It is intended to coordinate
language tooling, workspaces, execution sessions, and debug sessions for CLI,
Lab, and editor integrations.

The repository contains a local gRPC daemon with process-local Ferret source
workspaces and execution sessions, a supported Go client, and an experimental
language server with diagnostics, document navigation, symbols, hover,
completion, signature help, semantic tokens, and formatting. The Ferret VM,
compiler, runtime, and language semantics remain owned by the main Ferret
project.

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
./bin/ferretd lsp
```

`serve` starts the local daemon and waits for an interrupt or a `Shutdown` RPC.
It uses `$XDG_RUNTIME_DIR/ferret/ferretd.sock` on macOS and Linux, falling back
to the user cache directory, and `\\.\pipe\ferretd` on Windows. Explicit local
endpoints use `unix:///absolute/path` or `npipe:////./pipe/name`; TCP is not
supported. Daemon logs go to stderr.

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
})
watcher, err := c.Executions().WatchExecution(ctx, execution.ID)
running, err := c.Executions().RunExecution(ctx, execution.ID)
```

Opening a workspace recursively discovers lowercase `.fql` files, loads their
contents, and retains daemon-owned documents with Ferret syntax state and
diagnostics. No Ferret project manifest is required. The initial snapshot is
static; disk changes require closing and reopening the workspace.

Workspace state is in memory for the daemon process. Reopening the same cleaned
absolute root returns the same workspace ID, closing a client connection does
not close its workspaces, and `Close` is explicit and idempotent. The current
workspace RPC continues to expose identity and lifecycle operations rather than
documents or parser internals.

Each open workspace owns a Ferret engine with a read-write filesystem rooted at
the workspace directory. `CreateSession` compiles one retained workspace-relative
`.fql` document into an immutable reusable plan. Each `Execution` owns isolated
JSON-shaped parameter bindings and a fresh, one-shot Ferret runtime session.
`RunExecution` returns the `RUNNING` snapshot immediately; execution then
continues independently of the triggering RPC context. Clients can observe the
latest lifecycle event and subsequent events with `WatchExecution`, cancel an
active execution, and retrieve terminal output or failure details until they
explicitly close the resource. Closing a workspace cascades through its Sessions
and Executions.

`lsp` starts the experimental language server over stdin and stdout. It opens
the local roots supplied by LSP initialization, uses their static workspace
documents as a baseline, and gives versioned editor overlays precedence while
documents are open. Analysis snapshots are coalesced and cached per URI.

## Current Status

The daemon exposes API v1.1 `DaemonService`, `WorkspaceService`, and
`ExecutionService` contracts over a permission-restricted local transport, plus
the standard gRPC health service. The checked-in Go code under `gen/` is
generated from `proto/` with pinned Buf and protobuf tools. The debug protobuf
remains an ungenerated placeholder.

Daemon workspaces retain deterministically discovered source files, source
contents, Ferret parse trees, and syntax diagnostics. The language server uses
the shared workspace manager for static source baselines and supports opening,
changing, and closing `.fql` editor overlays with full-document synchronization.
Its navigation and references are document-local.

Debug sessions, DAP, filesystem watching, incremental synchronization,
cross-file indexing, module resolution, workspace persistence, remote daemon
operation, and LSP-over-gRPC are not implemented. Execution sessions do not add
queues, durable replay, persistence, automatic recompilation, or REPL state.

See [docs/architecture.md](docs/architecture.md) for the intended architecture
and [docs/lsp.md](docs/lsp.md) for experimental editor setup.

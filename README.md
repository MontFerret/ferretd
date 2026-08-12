# ferretd

`ferretd` is the experimental long-running developer service for
[Ferret](https://github.com/MontFerret/ferret). It is intended to coordinate
language tooling, workspaces, execution sessions, and debug sessions for CLI,
Lab, and editor integrations.

The repository contains a local gRPC daemon with process-local Ferret source
workspaces, a supported Go client, and an experimental language server that
publishes Ferret compiler diagnostics. The Ferret VM, compiler, runtime, and
language semantics remain owned by the main Ferret project.

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
compatibility negotiation, and exposes daemon and workspace operations:

```go
c, err := client.Dial(ctx)
if err != nil {
	return err
}
defer c.Close()

info, err := c.Info(ctx)
workspace, err := c.Workspaces().Open(ctx, projectRoot)
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

`lsp` continues to start the experimental language server over stdin and
stdout independently of the daemon.

## Current Status

The daemon exposes versioned `DaemonService` and `WorkspaceService` APIs over a
permission-restricted local transport, plus the standard gRPC health service.
The checked-in Go code under `gen/` is generated from `proto/` with pinned Buf
and protobuf tools. Execution and debug protobufs remain ungenerated
placeholders.

Daemon workspaces retain deterministically discovered source files, source
contents, Ferret parse trees, and syntax diagnostics. The separate language
server supports opening, changing, and closing `.fql` documents with
full-document synchronization and publishes parser and compiler diagnostics.
The language server does not yet consume daemon workspaces.

Execution sessions, debug sessions, DAP, filesystem watching, editor overlays,
module resolution, workspace persistence, remote daemon operation, and
LSP-over-gRPC are not implemented.

See [docs/architecture.md](docs/architecture.md) for the intended architecture
and [docs/lsp.md](docs/lsp.md) for experimental editor setup.

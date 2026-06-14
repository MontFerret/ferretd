# ferretd

`ferretd` is the experimental long-running developer service for
[Ferret](https://github.com/MontFerret/ferret). It is intended to coordinate
language tooling, workspaces, execution sessions, and debug sessions for CLI,
Lab, and editor integrations.

The repository contains the initial service boundaries and an experimental
language server that publishes Ferret compiler diagnostics. The Ferret VM,
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
```

## Commands

```sh
./bin/ferretd --version
./bin/ferretd serve
./bin/ferretd lsp
```

`serve` starts the placeholder daemon and waits for an interrupt. `lsp` starts
the experimental language server over stdin and stdout.

## Current Status

The language server supports opening, changing, and closing `.fql` documents
with full-document synchronization. It publishes parser and compiler
diagnostics for open documents. Other language features, DAP, gRPC serving,
protobuf generation, execution sessions, debug sessions, and module resolution
are not implemented yet.

The placeholder protobuf contracts under `proto/` establish package names and
service versions. Protobuf and gRPC tooling will be introduced in a follow-up
change before generated clients or servers are needed.

See [docs/architecture.md](docs/architecture.md) for the intended architecture
and [docs/lsp.md](docs/lsp.md) for experimental editor setup.

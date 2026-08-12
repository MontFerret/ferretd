# Architecture

`ferretd` is intended to become a long-running coordinator for Ferret developer
tooling:

```text
ferretd
├── LSP adapter
├── gRPC daemon API
│   ├── DaemonService
│   ├── WorkspaceService
│   └── standard health service
├── language service
├── workspace manager
├── execution session manager
└── debug session manager
```

## Responsibilities

The daemon owns local listener, gRPC, service-health, and graceful-shutdown
lifecycle. It listens on a permission-restricted Unix socket on macOS/Linux or
a current-user named pipe on Windows. The workspace manager owns
concurrency-safe, process-local workspace identity and state. The
protocol-neutral language service provides language features to protocol
adapters and other integrations. Execution and debug session managers remain
placeholders for future Ferret-backed sessions.

LSP and future DAP support are protocol adapters. They should translate
protocol messages and delegate to shared language, workspace, execution, and
debug services. Protocol adapters must not own core language or debugging
behavior.

The versioned gRPC API currently exposes daemon information, API-major
compatibility negotiation, shutdown, and idempotent workspace open/get/list/
close operations. `internal/grpc` translates these wire contracts and delegates
workspace behavior to `internal/workspace`; it does not own workspace state.
The supported top-level `client` package hides generated protobuf clients and
centralizes discovery, dialing, negotiation, and error classification.

Buf generates only the implemented daemon and workspace v1 contracts into
`gen/`. Execution and debug protobuf source files remain ungenerated
placeholders until those capabilities are implemented.

## Boundaries

`ferretd` does not replace the Ferret VM. Parsing, compilation, runtime
semantics, VM execution, and core debugging behavior remain owned by the main
Ferret project. This repository coordinates those capabilities for long-running
developer workflows.

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
concurrency-safe, process-local workspace identity, source discovery, files,
daemon document snapshots, and retained Ferret syntax state. The
protocol-neutral language service owns versioned editor overlays, per-document
analysis scheduling and caching, and Ferret-backed language features for
protocol adapters and other integrations. Execution and debug session managers
remain placeholders for future Ferret-backed sessions.

Workspace opening is synchronous. It scans lowercase `.fql` regular files under
the root, prunes a small fixed set of VCS and dependency directories, skips
symlinks below the root, and retains per-document source, parse state, and
syntax diagnostics. A malformed or unreadable document does not prevent other
documents from loading. The workspace snapshot remains unchanged until explicit
close. Editor overlays exist separately in the language service and take
precedence for the same URI; watching, incremental parsing, and automatic reload
remain future work.

LSP and future DAP support are protocol adapters. They should translate
protocol messages and delegate to shared language, workspace, execution, and
debug services. Protocol adapters must not own core language or debugging
behavior.

The versioned gRPC API currently exposes daemon information, API-major
compatibility negotiation, shutdown, and idempotent workspace open/get/list/
close operations. `internal/grpc` translates these wire contracts and delegates
workspace behavior to `internal/workspace`; it does not own workspace state.
The supported top-level `client` package hides generated protobuf clients and
centralizes discovery, dialing, negotiation, and error classification. The v1
wire contract continues to expose workspace identity and lifecycle only; syntax
trees and document state stay inside the daemon for shared future services.

The stdio LSP server constructs a shared workspace manager and language service.
Initialization roots populate static workspace baselines, while client-supplied
open-document text is retained as an overlay. The LSP adapter owns protocol
translation, lifecycle ordering, request cancellation, diagnostic publication,
and stdio framing rather than language semantics.

Buf generates only the implemented daemon and workspace v1 contracts into
`gen/`. Execution and debug protobuf source files remain ungenerated
placeholders until those capabilities are implemented.

## Boundaries

`ferretd` does not replace the Ferret VM. Parsing, compilation, runtime
semantics, VM execution, and core debugging behavior remain owned by the main
Ferret project. This repository coordinates those capabilities for long-running
developer workflows.

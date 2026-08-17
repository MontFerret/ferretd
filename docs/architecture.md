# Architecture

`ferretd` is intended to become a long-running coordinator for Ferret developer
tooling:

```text
ferretd
├── LSP adapter
├── gRPC daemon API
│   ├── DaemonService
│   ├── WorkspaceService
│   ├── ExecutionService
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
daemon document snapshots, retained Ferret syntax state, and one rooted
read-write Ferret engine per open workspace. The
protocol-neutral language service owns versioned editor overlays, per-document
analysis scheduling and caching, and Ferret-backed language features for
protocol adapters and other integrations. The execution manager owns compiled
Plan Sessions, isolated one-shot Executions, cancellation, lifecycle events,
terminal results, and the workspace-to-session-to-execution cleanup cascade.
The debug session manager remains a placeholder.

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
compatibility negotiation, shutdown, idempotent workspace lifecycle, execution
Session and Execution lifecycle, and a bounded server-streaming execution
watch. `internal/grpc` translates these wire contracts and delegates behavior
to `internal/workspace` and `internal/exec`; it does not own domain state. The
supported top-level `client` package hides generated protobuf clients and
centralizes discovery, dialing, negotiation, streaming, and error
classification. Syntax trees and document state stay inside the daemon.

The stdio LSP server constructs a shared workspace manager and language service.
Initialization roots populate static workspace baselines, while client-supplied
open-document text is retained as an overlay. The LSP adapter owns protocol
translation, lifecycle ordering, request cancellation, diagnostic publication,
and stdio framing rather than language semantics.

Buf generates the implemented daemon, workspace, and execution v1 contracts
into `gen/`. The debug protobuf source remains ungenerated until that capability
is implemented.

## Boundaries

`ferretd` does not replace the Ferret VM. Parsing, compilation, runtime
semantics, VM execution, and core debugging behavior remain owned by the main
Ferret project. Execution Sessions own compiled Ferret Plans and Executions own
fresh Ferret runtime Sessions; this repository coordinates their lifecycle for
long-running developer workflows.

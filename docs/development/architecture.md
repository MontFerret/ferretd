# Development Architecture

`ferretd` is a long-running coordinator for Ferret developer tooling. It owns
process and protocol integration, retained workspace and session state, and
source-position projection. Ferret continues to own FQL parsing, compilation,
runtime semantics, VM execution, and core debugging behavior.

This document describes the implemented repository shape. User-visible status
and limitations are summarized in the root `README.md`; current source and tests
remain authoritative when implementation and documentation differ.

## System shape

```text
editor or language client
    -> ferretd lsp over stdio
    -> internal/lsp
    -> internal/language
        -> editor overlay or internal/workspace baseline
        -> Ferret analysis and formatting
        -> internal/source and internal/diagnostic projection

ferretd serve
    -> internal/daemon coordinates lifecycle
        -> internal/transport local listener
        -> internal/grpc
            -> internal/workspace
            -> internal/exec
            -> Universal Runtime API
                -> internal/ferretapi -> native Ferret

local daemon client
    -> client
    -> internal/transport dial
    -> local Unix socket or Windows named pipe
    -> internal/grpc services

debug adapter client
    -> ferretd dap over stdio
    -> internal/dap
    -> internal/workspace + internal/exec + internal/debug
    -> Universal Runtime API
        -> internal/ferretapi -> native Ferret debugger
```

The `lsp` and `dap` commands compose in-process services independently of
`ferretd serve`. The daemon exposes workspace and execution behavior over gRPC;
debugging is not currently a daemon gRPC capability. Daemon and DAP composition
each construct one native Ferret engine, pass it to `internal/ferretapi`, and
own the resulting Universal runtime through final cleanup.

## Dependency direction

Commands compose services and adapters. Protocol adapters translate requests,
responses, events, coordinates, and errors, then delegate to protocol-neutral
packages. Execution and debug domain packages consume the Universal Runtime API;
`internal/ferretapi` alone translates that contract to native Ferret runtime
objects.

```text
cmd/ferretd serve -> internal/daemon -> internal/transport + internal/grpc
                                       -> internal/workspace + internal/exec
cmd/ferretd lsp   -> internal/lsp -> internal/language -> internal/workspace
cmd/ferretd dap   -> internal/dap -> internal/workspace + internal/exec
                                    + internal/debug

domain services  -> internal/source + internal/diagnostic + internal/lifecycle
execution/debug -> github.com/MontFerret/api -> internal/ferretapi -> Ferret
```

Protocol types do not flow into domain services. Mutable manager or session
internals do not flow into adapters. Cross-package ownership is represented by
explicit constructors, immutable values, copies, leases, and close hooks rather
than access to protected state.

## Package map

* `cmd/ferretd` owns Cobra commands, process context and signals, version output,
  stderr reporting, and top-level service composition.
* `internal/daemon` coordinates the local listener, gRPC health state, workspace
  and execution services, and graceful shutdown.
* `internal/transport` owns local endpoint parsing, defaults, listening, dialing,
  and platform security behavior.
* `internal/grpc` translates the versioned daemon, workspace, and execution wire
  contracts without owning domain state.
* `client` is the supported public Go client. It hides generated clients and
  centralizes endpoint discovery, dialing, negotiation, conversion, streaming,
  and public error classification.
* `internal/workspace` owns process-local workspace identity, dynamic discovery,
  retained documents and Ferret syntax state, refresh, and close coordination.
* `internal/exec` borrows a composition-owned `api.Runtime` and owns compiled
  Sessions, common per-run execution state, `Parameters`, normalized
  `RuntimeOptions`, copied `api.Output` and `RuntimeFailure` results, one-shot
  Executions, watches, cancellation, lazy debug Plans, and debugger-runtime
  leases.
* `internal/ferretapi` provisionally adapts a caller-constructed native Ferret
  engine and its Plan, Session, debugger Session, source, option, output, and
  diagnostic contracts to the Universal API. Composition may construct the
  native engine, but no other package translates between the runtime APIs.
* `internal/debug` owns retained DebugSessions, commands, paused-state
  inspection, event streams, and cleanup.
* `internal/dap` adapts workspace, execution, and debug services to one stdio DAP
  session and owns all DAP handles and client-coordinate conversion.
* `internal/language` owns editor overlays, source snapshot resolution, analysis
  coalescing and caching, diagnostics, formatting, and language features.
* `internal/lsp` adapts the language service to LSP lifecycle, capabilities,
  request cancellation, notifications, and stdio framing.
* `internal/source` owns local file URIs and UTF-8-byte to UTF-16 position
  mapping. `internal/diagnostic` owns shared diagnostic projections.
* `internal/lifecycle` supplies focused synchronization primitives used by
  managers with matching close semantics.
* `proto/ferretd` contains versioned protobuf sources. `gen/` contains generated
  daemon, workspace, and execution Go code.

## State and resource hierarchy

Each daemon or DAP service graph constructs one native Ferret engine, wraps it
as one shared Universal runtime, and closes only that wrapper after its child
services. An open workspace owns retained file and document snapshots, but no
execution engine.
An execution Session is a child of that workspace and owns an immutable
`api.Plan` compiled by the shared runtime. Each ordinary Execution is a one-shot
child of a Session and creates a fresh `api.Session`.

A Session can also lazily own one matching debug Plan. `internal/exec` builds an
execution runtime that owns one Universal debugger Session. A DebugRuntime
exposes that session's debugger capability and owns the debug-Plan lease through
the common runtime's one-time session close attempt. `internal/debug` layers
DebugSession identity, commands, presentation scopes, events, and state on that
DebugRuntime while consuming Universal debugger and source values directly.
Only `internal/ferretapi` translates those values to or from native Ferret
debugger types. Ordinary Executions and DebugSessions remain sibling resources
with distinct observable state machines. Closing a parent stops new runtime
creation, settles both child kinds, releases leases, and only then closes its
Plans. Composition cleanup then clears workspaces and closes the shared runtime
exactly once last.

The language service consumes workspace documents as saved-source baselines but
owns editor overlays separately. Opening or changing an editor document does
not mutate daemon workspace discovery or execution source.

## Implemented and future boundaries

Implemented protocol entry points are local gRPC for daemon/workspace/execution,
LSP over stdio, and single-session DAP over stdio. The debug protobuf source is
a placeholder and is intentionally excluded from generation. There is no debug
gRPC service or public debug client.

Workspace discovery loads the initial saved-source model synchronously, then a
workspace-owned watcher tracks eligible create, change, delete, and rename
operations until close. Session creation also reconciles its selected path so a
missed creation notification cannot prevent execution. Durable state and
eviction are not implemented.

The language service supports full-document editor overlays and document-local
language intelligence. Incremental synchronization, module resolution,
cross-file indexing, rename, code actions, workspace symbols, and dynamic
workspace-folder changes are not implemented.

These absences are boundaries, not extension points to fill during unrelated
work. Adding a capability requires changing its owning domain package first,
then the appropriate adapter and public documentation.

## Subsystem guides

* [Daemon and protocols](daemon.md)
* [Workspace model](workspace.md)
* [Language tooling](language.md)
* [Execution model](execution.md)
* [Debugging model](debugging.md)
* [Build and release](release.md)

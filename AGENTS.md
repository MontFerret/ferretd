# AGENTS.md

This file is the canonical operating guide for coding agents working in this repository. It describes the current experimental `ferretd` implementation, not an aspirational implementation of the architecture. If repository documentation conflicts with this file, prefer `Makefile`, `go.mod`, `.github/workflows/ci.yml`, and the current tests for commands, toolchains, CI behavior, and executable contracts.

## Repo snapshot

* Module path: `github.com/MontFerret/ferretd`
* Go version in `go.mod`: Go 1.26.1
* `ferretd` is the experimental long-running developer service for Ferret.
* The current executable provides `serve`, `lsp`, `dap`, and version/help behavior.
* The current language server combines static workspace baselines with versioned editor overlays and provides Ferret-backed diagnostics, document symbols, hover, document-local navigation and references, completion, signature help, semantic tokens, and formatting over LSP stdio.
* `serve` exposes daemon information, API negotiation, graceful shutdown, workspace lifecycle, and execution sessions over a local gRPC transport.
* Opening a workspace synchronously discovers `.fql` files, retains daemon-owned source, syntax state, and document diagnostics, and constructs one rooted read-write Ferret engine until explicit close.
* The execution manager owns compiled Sessions and one-shot Executions, the debug manager owns retained DebugSessions, and `dap` is a single-session stdio adapter over both protocol-neutral models.
* Buf generates the implemented daemon/workspace/execution protobuf and gRPC packages under `gen/`. Debug protobufs remain ungenerated placeholders.
* Ferret remains the owner of FQL parsing, compilation, runtime semantics, VM execution, and core debugging behavior.

Do not infer implemented behavior from architecture diagrams, placeholder service definitions, future-facing type names, or historical discussion. Current source, tests, dependency contracts, and build configuration are authoritative.

## Architectural mental model

The implemented language-tooling flow is:

```text
editor or language client
    -> ferretd lsp over stdin/stdout
    -> internal/lsp protocol adapter
    -> internal/language protocol-neutral language service
        -> versioned editor overlay or static workspace snapshot
        -> coalesced per-snapshot Ferret analysis or canonical formatting
        -> internal/source UTF-8-byte-to-UTF-16 mapping
    -> LSP responses and diagnostics notifications
```

The current daemon flow is:

```text
ferretd serve
    -> internal/daemon
    -> internal/transport local listener
    -> internal/grpc DaemonService, WorkspaceService, ExecutionService, and health
    -> protocol-neutral workspace and execution managers
    -> root-confined source discovery, Ferret compilation, and one-shot execution
    -> signal, RPC, or context-triggered graceful shutdown
```

The intended longer-term architecture is:

```text
CLI, Lab, and editor integrations
    -> LSP, DAP, or versioned gRPC adapters
    -> protocol-neutral language, workspace, execution, and debug services
    -> Ferret-owned compiler, runtime, VM, and debugger capabilities
```

Agents should reason about changes by ownership boundary:

* `cmd/ferretd` owns process startup, command parsing, process-facing output, signal handling, and top-level service wiring.
* `internal/daemon` owns long-running service lifecycle and coordination.
* `internal/grpc` owns gRPC translation and health behavior, not daemon domain state.
* `internal/transport` owns default endpoint discovery and Unix-socket/named-pipe I/O.
* `internal/lsp` owns LSP translation and transport behavior, not language semantics.
* `internal/language` owns protocol-neutral open-document state and language-service behavior built on Ferret.
* `internal/source` owns protocol-neutral source locations, file-URI conversion, and source-position conversion.
* `internal/workspace` owns workspace state as that capability is implemented.
* `internal/exec` owns compiled execution Sessions, ordinary Executions, lazy debug Plans, and leased immutable debug targets.
* `internal/debug` owns retained debug-session coordination, debugger commands, inspection, events, and debug child cleanup.
* `internal/dap` owns DAP translation, stdio framing, and protocol handle allocation, not debugger semantics.
* `client` owns the supported Go client, compatibility negotiation, and public error classification.
* `proto/ferretd` owns versioned daemon RPC source contracts; `gen/` is checked-in generated output.
* The Ferret dependency owns parsing, compilation, diagnostics primitives, runtime semantics, VM execution, and core debugging machinery.

Protocol adapters should translate and delegate. They must not become alternate owners of language, execution, workspace, or debugging behavior.

## Canonical invariants

* `ferretd` coordinates Ferret capabilities; it does not replace or fork Ferret's compiler, runtime, VM, or language semantics.
* The language service is protocol-neutral. LSP-specific request and response types stay in `internal/lsp`.
* LSP stdout is protocol-only. Do not print logs, progress messages, diagnostics, or ordinary CLI text to stdout while serving LSP.
* The current language server uses full-document synchronization. Incremental text edits are rejected.
* Open document contents are supplied by the client, kept as versioned in-memory overlays, and removed on close. An overlay takes precedence over the static workspace snapshot for the same URI; closing it falls back to that snapshot when one exists.
* Initialization roots synchronously populate static workspace baselines for unopened documents. The language service does not watch disk, reload roots in the background, or mutate workspace discovery in response to editor lifecycle events.
* A document change must advance the stored version. Stale or same-version changes are rejected.
* Closing an unknown document is safe and idempotent.
* Current document URIs must be local `file` URIs. Unsupported schemes, non-local hosts, queries, fragments, and empty paths remain errors.
* Ferret compiler spans and protocol-neutral source spans use zero-based, half-open UTF-8 byte offsets. Protocol-neutral positions and LSP positions use zero-based lines and UTF-16 code units with half-open ranges.
* Source mapping must clamp invalid offsets safely and preserve CR, LF, CRLF, Unicode, and astral-character behavior.
* Shared service state must remain safe for concurrent callers. Do not bypass or leak mutable state protected by service synchronization.
* Context cancellation must remain effective at process and service boundaries. Long-running operations must not outlive their owning context without an explicit lifecycle reason.
* Daemon shutdown must remain safe after cancellation and `Stop` must remain idempotent.
* The daemon remains local-only: Unix sockets or Windows named pipes, permission-restricted to the current user, with no TCP, TLS, or remote mode.
* API major mismatch is rejected; minor versions are additive. Workspace, Session, and Execution state survive client disconnects but not daemon restarts.
* Workspace roots are existing absolute directories, cleaned without resolving symlinks; repeated opens converge and closes are idempotent.
* Workspace loading is synchronous with static discovery. It retains lowercase `.fql` regular files recursively and skips nested symlinks. `CreateSession` refreshes only its already-discovered target; close/reopen is required to discover creates, deletes, and renames.
* `.git`, `.hg`, `.svn`, `node_modules`, and `vendor` directories are pruned during discovery. There is no ignore-file or project-manifest contract.
* Workspace documents retain source and Ferret syntax state only. A refresh atomically advances changed retained state, while compilation into immutable Plans and one-shot runtime state belong to execution Sessions and Executions; editor overlays and filesystem watching remain separate capabilities.
* Document load and syntax diagnostics do not fail an otherwise coherent workspace; fatal root/discovery failures do not leave manager entries.
* A Session owns one immutable compiled Ferret Plan. An Execution owns one fresh runtime Session, one run attempt, isolated parameters, terminal output or failure, and bounded lifecycle observation.
* A Session lazily compiles and caches one matching debug Plan. `internal/exec` leases immutable debug targets while independent retained DebugSessions in `internal/debug` own exactly one Ferret debugger session, asynchronous commands, paused-state inspection, terminal retention, and bounded lifecycle observation.
* DAP is single-session stdio only. DAP stdout is protocol-only, DAP owns all integer handles, and every handle becomes stale when execution runs or becomes terminal.
* Debug placeholder protobuf declarations do not imply generated clients, servers, or service behavior.
* Current package names, protobuf package names, `go_package` values, service versions, CLI behavior, and LSP wire behavior are compatibility-sensitive.
* Preserve existing behavior unless the task explicitly changes it. Do not implement future architecture as a side effect of an unrelated change.

## Package and source map

Begin with the package that owns the requested behavior. Do not move logic into a protocol adapter or top-level command merely because that call site is convenient.

### Command entry point

* `cmd/ferretd`
    * Owns the `ferretd` process, Cobra command tree, version output, help behavior, signal-aware root context, and composition of top-level services.
    * `serve` starts the local gRPC daemon and supports explicit `--endpoint` selection.
    * `lsp` starts the language server over stdin and stdout.
    * `dap` starts the single-session debug adapter over stdin and stdout.
    * Keep command parsing and process-facing error context here.
    * Preserve protocol-pure stdout for `lsp` and `dap`.
    * Do not move language, workspace, execution, debug, or transport-independent lifecycle behavior into `main`.

### Daemon coordination

* `internal/daemon`
    * Owns construction and lifecycle coordination for the services that make up `ferretd`.
    * Keep orchestration thin and delegate behavior to the owning service.
    * `Start` listens and serves until context cancellation, RPC shutdown, explicit stop, or transport failure.
    * `Stop` marks health unavailable, closes execution resources, clears workspace engines, drains or force-stops RPCs, closes the listener, and remains idempotent.

### Protocol-neutral language service

* `internal/language`
    * Owns versioned editor overlays, static workspace fallback, snapshot resolution, per-snapshot analysis scheduling and caching, and protocol-neutral language features built on Ferret.
    * The service stores document values rather than exposing mutable internal references, and an overlay always takes precedence over a matching workspace snapshot until close.
    * Keep Ferret analysis, formatting, diagnostic extraction, and protocol-neutral feature projection here when they are independent of a wire protocol.
    * Preserve stable error identity through `errors.Is` when adding context to document lifecycle errors.
    * Do not introduce LSP types into this package.
    * Do not reimplement parser, compiler, or diagnostic semantics already owned by Ferret.

### LSP adapter

* `internal/lsp`
    * Owns Language Server Protocol capability advertisement, lifecycle handlers, request/notification translation, and LSP diagnostic projection.
    * Keep the adapter thin and delegate document symbols, hover, definition, references, completion, signature help, semantic tokens, formatting, diagnostics, and document state to `internal/language`.
    * Preserve full-document synchronization until incremental changes are intentionally implemented through the protocol-neutral service.
    * Serialize document lifecycle handling where required to keep notification order and shared document state coherent.
    * Publish diagnostics with the current document version when available and clear diagnostics when a document closes.
    * LSP callbacks and notifications are wire-facing behavior; test both translated payloads and delegated state changes.
    * Never write non-protocol output to LSP stdout.

### Source locations and mapping

* `internal/source`
    * Owns protocol-neutral `URI`, `Position`, `Range`, and `Span` concepts used by services and adapters.
    * Owns conversion between local filesystem paths and escaped absolute file URIs.
    * Owns conversion from Ferret's UTF-8 byte-indexed spans to zero-based UTF-16 positions.
    * URI handling is platform-sensitive. Preserve Unix and Windows path behavior, escaping, localhost handling, and rejection of unsupported URI forms.
    * Position mapping is correctness-sensitive. Cover newlines, CRLF, Unicode, astral characters, empty text, and invalid offsets.
    * Do not place LSP-specific types in this package; adapters should convert protocol-neutral values at the boundary.

### Workspace, execution, and debug services

* `internal/workspace`
    * Owns concurrency-safe, process-local workspace identity, lifecycle, discovery, files, daemon documents, source snapshots, retained Ferret syntax state, and one rooted read-write Ferret engine per open workspace.
    * Canonicalizes with `filepath.Abs` at the public client boundary and `filepath.Clean` at the service boundary; it deliberately does not resolve symlinks.
    * Coordinates duplicate in-flight opens without holding manager locks across I/O or parsing and publishes only successfully loaded workspaces.
    * Returns copies of files, sources, and diagnostics; retained parser state remains shared daemon-owned state that visitors must treat as read-only. State remains independent of connections, lists and documents are sorted deterministically, and repeated open/close operations are convergent.
    * Uses Ferret source/parser/diagnostic APIs for syntax state and exposes the compile boundary consumed by `internal/exec`. It does not own Ferret semantic behavior.
* `internal/exec`
    * Owns concurrency-safe compiled Sessions, one-shot Execution coordination, cancellation, terminal state, bounded execution watchers, lazy debug Plans, immutable debug-target leases, and parent-child cleanup hooks.
    * Ferret remains the owner of compilation, runtime semantics, and VM execution.
    * Sessions retain an eager normal Plan and a lazy matching debug Plan; each Execution creates and closes a fresh Ferret runtime Session and runs it at most once.
* `internal/debug`
    * Owns retained DebugSessions, breakpoints, debugger commands, paused-state inspection, terminal retention, bounded debug watches, and execution-Session child cleanup.
    * Acquires only leased immutable debug targets from `internal/exec`; it does not receive mutable execution Session internals or own Plan compilation.
    * Each DebugSession owns one retained Ferret debugger session while Ferret remains the owner of debugger semantics and VM execution.
* `internal/dap`
    * Owns the single-session DAP initialization, launch, configuration, request/event translation, client coordinate conversion, integer handles, and serialized stdio writes.
    * Delegates workspace and execution Session behavior to `internal/workspace` and `internal/exec`, and all debugger resources and operations to `internal/debug`.
    * Must not reimplement Ferret debugger behavior or add debug gRPC/client APIs.

### Protobuf contracts

* `proto/ferretd/workspace/v1`
    * Contains the implemented `WorkspaceService` v1 source contract.
* `proto/ferretd/daemon/v1`
    * Contains the implemented `DaemonService` v1 source contract and compatibility detail.
* `proto/ferretd/execution/v1`
    * Contains the implemented `ExecutionService` v1 source contract.
* `proto/ferretd/debug/v1`
    * Contains the placeholder `DebugService` v1 source contract.

Buf v2 configuration at the repository root pins generation through `make generate`; checked-in output belongs under `gen/` and must never be edited manually. `make proto-lint` validates all source contracts, while generation intentionally targets daemon/workspace/execution v1. Keep debug ungenerated until a debug gRPC service is explicitly implemented.

### Documentation, tooling, and release scripts

* `README.md`
    * Describes current product status and common user-facing commands.
* `docs/architecture.md`
    * Describes intended responsibility boundaries. Treat future-facing statements as design direction, not evidence of implementation.
* `docs/lsp.md`
    * Describes the experimental editor integration and currently supported LSP behavior.
* `Makefile`
    * Is the source of truth for routine build, test, format, lint, vet, compile, and release entry points.
* `.github/workflows/ci.yml`
    * Is the source of truth for CI setup and executed validation.
* `scripts/versions.sh`
    * Derives the development/build version and Ferret dependency version used by the linker flags.
* `scripts/release.sh`
    * Creates and pushes a release tag from a clean working tree. Run release operations only when explicitly requested.

## Implemented and planned behavior

Keep current behavior distinct from planned architecture in code, tests, documentation, and final summaries.

Currently implemented:

* Cobra-based `serve`, `lsp`, `dap`, help, and version behavior;
* signal-aware process cancellation;
* local Unix-socket and Windows named-pipe gRPC serving;
* API v1.1 negotiation, daemon information, health, and graceful shutdown;
* supported Go client discovery, dialing, negotiation, and error classification;
* concurrency-safe, process-local workspace open/get/list/close behavior;
* deterministic root-confined `.fql` discovery with fixed directory exclusions and nested-symlink avoidance;
* daemon-owned file and document snapshots with source contents, revision, Ferret parse state, syntax/load diagnostics, and per-Session refresh of already-discovered targets;
* one rooted read-write Ferret engine per open workspace;
* immutable compiled Sessions and isolated one-shot Executions with JSON-shaped parameters and encoded output;
* asynchronous run, cancellation, terminal retention, and latest-plus-future bounded lifecycle watches;
* workspace-to-Session-to-Execution close cascades and daemon-owned cleanup;
* lazy per-Session debug Plans and independent retained DebugSessions;
* asynchronous start, continue, pause, step-in, step-over, step-out, termination, frame scopes, variables, evaluation, and bounded debug watches;
* single-session DAP over stdio with local launch, source breakpoints, client coordinate conversion, and paused-state handle invalidation;
* LSP over stdio;
* open, full-document change, and close notifications;
* initialization-root workspace baselines for unopened documents and versioned in-memory editor overlays with overlay precedence;
* coalesced and cached Ferret analysis per resolved source snapshot;
* Ferret parser/compiler diagnostics, document symbols, hover, document-local definitions and references, completion, signature help, full semantic tokens, and full-document formatting;
* UTF-8 byte-span to UTF-16 range conversion;
* pinned protobuf generation for daemon/workspace/execution v1;
* placeholder debug protobuf source contract.

Not currently implemented:

* debug protobuf generation, gRPC service behavior, or public Go client APIs;
* durable workspace persistence or eviction;
* module resolution and cross-file indexing;
* filesystem watching, background reload, create/delete/rename discovery, and incremental workspace parsing;
* incremental document synchronization;
* rename, code actions, workspace symbols, cross-file definitions or references, range formatting, or dynamic workspace-folder changes.

Do not claim planned capabilities are supported. When implementing one, update the relevant current-status documentation and remove only the corresponding obsolete limitation.

## Where to start by task

* Change CLI commands, flags, version text, signals, or process-facing errors:
    * inspect `cmd/ferretd` and its tests
    * preserve LSP stdout purity and Cobra error/usage behavior
* Change daemon startup or shutdown:
    * inspect `internal/daemon` and `cmd/ferretd`
    * define ownership, cancellation, partial-startup cleanup, shutdown order, and idempotency before adding concurrency
* Change open-document state or version semantics:
    * inspect `internal/language`
    * preserve synchronization and copy boundaries
    * test cancellation, missing documents, stale versions, and close behavior
* Change diagnostics:
    * inspect `internal/language/diagnostics.go`
    * inspect the current Ferret diagnostic contract before changing conversion
    * verify primary ranges, related information, codes, hints, notes, empty documents, and unexpected errors
* Add or change an LSP feature:
    * put protocol-neutral behavior in the owning shared service first
    * translate it in `internal/lsp`
    * test capability advertisement, payload conversion, lifecycle ordering, and service effects
* Change URI or position behavior:
    * inspect `internal/source`
    * test platform path rules, escaping, Unicode, UTF-16 width, newline forms, and clamping
* Add workspace behavior:
    * begin in `internal/workspace`
    * preserve the distinct filesystem-file and daemon-document models
    * preserve synchronous static discovery, root confinement, deterministic ordering, per-Session existing-target refresh, and recoverable document diagnostics unless the task explicitly changes them
    * define workspace identity, ownership, lifecycle, and concurrency before exposing new behavior through an adapter
* Add or change execution sessions:
    * begin in `internal/exec`
    * use Ferret's public embedding/runtime contracts rather than duplicating execution semantics
    * define cancellation, result ownership, cleanup, and session lifetime explicitly
* Add debug sessions or DAP:
    * begin with protocol-neutral retained debugger coordination in `internal/debug`
    * keep lazy debug Plan compilation and target leases in `internal/exec`
    * keep DAP translation in `internal/dap`
    * consume Ferret debugging capabilities rather than redefining them
* Change a protobuf contract or implement gRPC:
    * begin with the versioned `.proto` source
    * treat field numbers and service/message names as compatibility-sensitive
    * introduce and document reproducible generation before adding generated code
    * add server/client translation and wire-level tests
* Change user-visible CLI, LSP, daemon, or configuration behavior:
    * update `README.md`, `docs/lsp.md`, or `docs/architecture.md` where applicable
    * keep current-status statements synchronized with implementation

## Ferret ownership boundary

`ferretd` consumes `github.com/MontFerret/ferret/v2`; it does not own Ferret language or runtime behavior.

* Make syntax, parser, compiler, diagnostics-primitive, runtime-value, VM, core debugger, standard-library, or embedding-contract changes in Ferret when that repository owns the requested behavior.
* Keep daemon-specific document state, session coordination, transport adaptation, and source-position projection in `ferretd`.
* Do not copy Ferret internals into this repository to avoid a dependency change.
* Do not use a protocol-specific workaround to redefine a Ferret semantic error or source span.
* When a task requires coordinated Ferret and `ferretd` changes, identify the cross-repository contract and validate the exact dependency version used here.
* Do not assume behavior from another Ferret version, branch, or unreleased local checkout unless `go.mod` or the task explicitly selects it.

## Compatibility-sensitive surfaces

The repository contains no intended public Go package today; all service packages are under `internal`. That does not make process and wire behavior freely changeable.

Treat observable behavior as intentional until the task establishes otherwise. Do not change public, language-visible, wire-visible, CLI-visible, persistence-visible, or integration-visible behavior as collateral cleanup.

Treat these as compatibility-sensitive:

* CLI command names, arguments, exit behavior, version text, stdout/stderr separation, and cancellation behavior;
* LSP initialization capabilities, synchronization mode, lifecycle semantics, diagnostic payloads, document versions, and protocol purity;
* protobuf package names, `go_package` paths, service names, RPC names, message names, field numbers, field types, and version directories;
* source URI acceptance and source-position conversion observed by clients;
* error identity relied upon by package tests or future adapters.

For compatibility-sensitive changes:

* make the behavior change explicit in the task summary;
* add focused tests at the observable boundary;
* preserve old behavior unless incompatibility is required;
* document intentional incompatibility;
* update user-facing documentation when support or configuration changes;
* avoid exporting Go APIs merely to share implementation across internal packages.

Do not infer desired behavior from historical discussions, abandoned designs, stale comments, or future-looking architecture when current source and tests establish a different contract.

## Protocol and adapter rules

* Keep adapters thin: validate and translate protocol values, delegate to protocol-neutral services, and translate results back.
* Do not let LSP, DAP, or gRPC types leak into shared service packages.
* Keep framing, protocol handles, client capability state, wire identifiers, and other protocol-specific state in the owning adapter.
* Keep reusable language, workspace, execution, and debugging behavior below adapters even when one protocol is currently its only caller. Do not move domain behavior upward for call-site convenience.
* Avoid duplicated semantics. When one subsystem owns a rule, adapters and consumers should call through that owner rather than reproduce it independently.
* Do not expose implementation details across boundaries merely to avoid making the proper change in the owning layer.
* Do not place transport lifecycle, serialization, or notification behavior in domain services.
* Validate unsupported protocol forms explicitly. Do not silently reinterpret incremental changes as full-document changes.
* Keep output channels pure. In particular, LSP stdout contains framed protocol messages only.
* Preserve request and notification ordering where state transitions depend on it.
* Avoid holding service locks while performing transport callbacks, blocking I/O, or potentially long Ferret operations unless a documented invariant requires it.
* Map errors at boundaries without destroying underlying identity needed by `errors.Is` or `errors.As`.
* Test translation separately from the underlying service when practical.

## Context, lifecycle, and concurrency rules

* Accept `context.Context` at operation boundaries that can block, be canceled, perform I/O, perform potentially long-running work, or participate in a caller-owned lifecycle.
* Check or propagate cancellation early enough to avoid committing state after cancellation.
* Do not store contexts in long-lived structs. Store explicit lifecycle state and cancellation functions when ownership requires them.
* Do not replace an available caller context with `context.Background()` without a concrete protocol or lifecycle reason.
* Long-running work must not outlive its owning context without an explicit lifecycle reason.
* Every goroutine must have a clear owner, termination condition, and cleanup path.
* Avoid goroutine leaks on normal completion, errors, cancellation, partial startup, and repeated shutdown.
* Identify which mutex protects each field or cohesive state group. Keep lock scope narrow and make protected state obvious from the type layout or a focused invariant comment.
* Prefer one cohesive lock-owned state representation when fields participate in the same lifecycle transition. Do not mix mutexes, atomics, channels, and once-guards for the same state without a concrete ordering or performance reason.
* Give lifecycle transitions one authoritative representation. Derived flags and events must not become competing sources of truth.
* Scrutinize repeated hand-written lifecycle or synchronization protocols because fixes otherwise need to be reproduced. Share a mechanism only when its semantics and ownership genuinely match across domains.
* Never trade obvious domain ownership for a clever concurrency abstraction. Execution Sessions and DebugSessions may share a coordination mechanism without becoming one generic session model.
* Do not call unknown, external, blocking, or potentially re-entrant code while holding a lock unless the ordering requirement is explicit and tested.
* Return copies or immutable views when callers must not mutate synchronized internal state.
* Preserve required ordering between state changes and externally visible events, including document changes and diagnostic publication.
* Test cancellation, idempotent cleanup, stale state, and concurrent access with deterministic lifecycle coordination where the behavior is meaningful.
* Use the race detector for changes that add or materially alter shared mutable state or goroutine coordination.
* Concurrency comments should explain ownership, invariants, and non-obvious ordering, not narrate individual statements.

## Error-handling rules

* Use standard `errors` and `%w` wrapping so callers can inspect error identity.
* Add context at subsystem and process boundaries without repeating the entire call chain.
* Error strings should be lowercase sentence fragments unless they contain a proper name or protocol-defined text.
* Keep sentinel errors for stable conditions callers need to classify.
* Use typed errors when they express a meaningful structured contract.
* Do not compare error strings in production code when `errors.Is`, `errors.As`, or a typed error can express the contract.
* Distinguish cancellation, invalid input, missing state, stale state, dependency failures, transport failures, runtime or domain failures, and internal invariant violations where callers need different behavior.
* Do not collapse expected user or domain failures and internal invariant violations into the same conceptual error class.
* Do not log and return the same error at every layer. The owning process or transport boundary should decide how to report it.
* Never write errors to protocol stdout.

## Go design and API ownership

Use [Effective Go](https://go.dev/doc/effective_go), [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), and standard-library conventions as the general baseline. The repository-specific ownership, lifecycle, protocol, and formatting rules in this guide take precedence where they make a deliberate choice.

### Semantic types

* Introduce a named type when it can own meaningful semantics, invariants, behavior, validation, conversion, lifecycle, or API safety. Otherwise prefer the underlying type.
* APIs for an established semantic type should normally accept or return that type instead of bypassing it with the underlying primitive. For example, URI parsing, validation, and conversion should normally flow through the semantic URI type when one exists.
* Do not add a wrapper merely for naming, and do not leave a meaningful domain type disconnected from all intrinsic behavior while free functions continue to operate on primitive representations.

### Dependencies and nil semantics

* Required peer-service dependencies must be explicit. Domain-service constructors must not interpret a nil required dependency as a request to construct a hidden default.
* Construct required services once at a clear composition root and pass them into dependents. Tests should construct the same required dependency graph explicitly.
* Optional callbacks, options, or dependencies may have defaults only when their optional nature and default behavior are intentional and documented.
* Avoid service locators, hidden globals, implicit initialization, and invisible dependency construction when explicit construction is practical.
* Require non-nil `context.Context` values at operation boundaries. Do not silently replace a nil caller context with `context.Background()`.
* Do not normally make nil receivers valid domain objects or map them to lifecycle states such as closed. Use nil semantics only when nil is genuinely part of the documented contract.

### Resources, enums, and options

* Make resource ownership and cleanup visible in APIs. A type that owns a closable resource should normally provide the lifecycle operation rather than expose a DTO-like field that callers must discover and close manually.
* State whether resources are owned, borrowed, leased, or transferred when the distinction affects cleanup. Release partially acquired resources on every failure path.
* Keep cleanup correct on success, errors, cancellation, early returns, partial initialization, and repeated close or shutdown when idempotency is part of the contract.
* Do not eagerly materialize, retain, copy, or promote expensive resources without a concrete need. Make ownership transitions explicit when values escape their current execution or ownership scope.
* Unless zero has a natural and safe domain meaning, reserve it as the unspecified or invalid value for enum-like types and begin meaningful values after it. Keep sibling packages consistent and document intentional meaningful-zero exceptions.
* Keep option validation, trimming, and defaulting close to the option-owning type or constructor. Do not repeat normalization across managers, services, and adapters.

## Go type and file structure rules

These rules are mandatory unless the task explicitly requires otherwise.

* Do not define multiple method-bearing structs in the same `.go` file.
* Prefer declaring a method-bearing struct as a standalone `type Name struct { ... }`.
* A method-bearing struct should usually live in its own file, named after the primary type or responsibility whenever practical, for example:
    * `service.go` for `Service`;
    * `server.go` for `Server`;
    * `manager.go` for `Manager`;
    * `session.go` or `sessions.go` for session ownership.
* Grouped `type ( ... )` declarations are allowed for interfaces, passive data-only structs, and small related value types from one narrow concern.
* A grouped declaration may contain exactly one method-bearing struct only when it is the file's sole behavioral type and the other types are passive helpers from the same concern.
* Do not use grouped declarations to hide multiple substantial behavioral types.
* If a helper gains methods and would create another method-bearing struct in the file, extract it into its own file.
* Methods should live with their struct unless a strong concern-based split makes the result clearer.
* Do not place a new method-bearing struct in an existing file merely because the code compiles.
* A file centered on a behavioral type should remain centered on that type.

Allowed:

```go
type (
	Diagnostic struct {
		Message  string
		Severity DiagnosticSeverity
	}
	
	RelatedInformation struct {
		Message string
	}
)
```

Avoid:

```go
type (
	Service struct {
		// ...
	}
	serverState struct {
		// ...
	}
)
```

## Function and method ownership rules

* Prefer a method when behavior belongs intrinsically to a semantic type, depends on its invariants, is a natural query or transformation of its value, or manages resources that value owns.
* Prefer a package-level function when behavior constructs a value, combines unrelated values, performs a package-wide conversion, or has no natural receiver.
* Do not turn every helper into a method for stylistic uniformity. Conversely, do not introduce a meaningful domain type and leave its intrinsic behavior in free functions that accept primitive representations.
* A file centered on a method-bearing type should contain that type, its methods, and its constructors.
* Constructors are the normally allowed package-level functions in a type-centered file.
* Do not mix unrelated package-level helpers into a type-centered file.
* Keep protocol conversion helpers near the adapter concern they serve.
* Keep Ferret-to-protocol-neutral conversion in the language or source layer, not in process setup.
* Prefer the narrowest ownership level that keeps behavior testable and avoids duplicate semantics.

## Package, file, and abstraction design

* Do not use `helpers.go`, `utils.go`, or similarly generic files as long-term containers for unrelated functionality. A small helper file is acceptable while its contents remain one cohesive concern.
* As a concern grows, organize files around responsibilities a reader can predict, such as lifecycle, conversion, snapshots, parameters, identifiers, or protocol state.
* Keep package boundaries domain-oriented. Do not create a package solely to shorten files, remove a few repeated lines, or create an abstract layer.
* Prefer cohesive private implementation types and files over package fragmentation. Keep symbols unexported until another package has a real need for them.
* Prefer concrete types until multiple meaningful implementations, an actual substitution boundary, a focused consumer-side contract, or a concrete test seam that materially improves the design justifies an interface. Interfaces are usually most useful at the point of consumption.
* Prefer deletion and simplification over new abstractions. Similar-looking code is not sufficient reason to share an implementation.
* Extract shared behavior when the duplicated code represents the same concept with the same semantics, ownership, and lifecycle. Keep domain-specific types domain-specific.
* Give duplicated synchronization and lifecycle state machines extra scrutiny, but preserve clear ownership when extracting a shared mechanism. Do not create a generic session manager merely because execution and debug coordination have structural similarities.
* Do not introduce interfaces, wrappers, managers, factories, generic types, or layers for aesthetic symmetry, a few repeated lines, easier mocking alone, shorter files, or hypothetical future requirements.
* Avoid speculative generalization for future adapters, services, protocols, or lifecycle models. An abstraction must make this repository easier to reason about and earn its complexity.
* Avoid both oversized responsibilities and fragmentation across excessive helpers, files, interfaces, or packages. Behavioral ownership should be predictable from the organization.

## Comment rules

* Do not add comments to every function or method by default.
* Exported declarations should have useful doc comments, even in `internal` packages, when they define package-facing contracts.
* Comment unexported code only when it carries non-obvious behavior, invariants, side effects, ownership, synchronization, lifecycle, cleanup, recovery, protocol, or compatibility constraints.
* Explain why the code exists, what must remain true, what the contract guarantees, how ownership works, or why ordering matters.
* Do not restate names or signatures.
* Keep future plans out of code comments unless the comment describes a deliberate current boundary.
* Update or remove comments when implementation makes them obsolete.
* Prefer dense, meaningful comments over comment wallpaper.

Preferred:

```go
// OffsetToPosition converts a Ferret UTF-8 byte offset to a zero-based UTF-16
// position suitable for protocol adapters.
func (m *Mapper) OffsetToPosition(offset int) Position
```

Avoid:

```go
// OffsetToPosition converts an offset to a position.
func (m *Mapper) OffsetToPosition(offset int) Position
```

## Go control-flow spacing rules

These rules apply to handwritten Go code. Blank lines should separate logical units and make control-transfer boundaries easy to scan.

### Immediate producer and check

A declaration, call, lookup, parse, or assertion may remain directly adjacent to the `if` that immediately checks or consumes its result.

Preferred:

```go
document, ok := s.documents[uri]
if !ok {
	return ErrDocumentNotOpen
}
```

Preferred:

```go
path, err := source.URIToPath(uri)
if err != nil {
	return fmt.Errorf("resolve document URI: %w", err)
}
```

If that producer-and-check unit follows another logical unit, separate it from the preceding work:

```go
validateRequest(request)

document, ok := s.documents[uri]
if !ok {
	return ErrDocumentNotOpen
}
```

### Consecutive control flow

Separate independent `if` statements with a blank line.

```go
if err := ctx.Err(); err != nil {
	return err
}

if len(changes) == 0 {
	return ErrNoTextChanges
}
```

Add a blank line after completed control flow before continuing with an independent statement.

### Return and break separation

When another statement precedes `return` or `break` in the same block, start the control transfer as a new logical group.

Preferred:

```go
result := buildResult()

return result
```

Preferred:

```go
if ready {
	state = stateRunning

	break
}
```

No blank line is required when `return` is already the first statement in its block:

```go
if err != nil {
	return err
}
```

Do not add artificial leading blank lines or surround every return mechanically. The rule separates a control transfer from preceding work in the same block.

## Local type declarations

Local types declared inside functions are allowed when they are small, passive, method-free, used only by that function, and make the local algorithm easier to understand.

Prefer a package-level unexported type when the type:

* represents a meaningful domain, lifecycle, protocol, or algorithmic concept;
* is used across a substantial function or by nearby helpers;
* would make control flow easier to scan when declared separately;
* may reasonably gain methods;
* is likely to be reused;
* clarifies ownership at package scope.

Do not promote a tiny throwaway struct merely for consistency, and do not hide a meaningful state or protocol concept inside a long function merely to avoid another declaration.

Choose between local and package scope based on readability, conceptual ownership, and expected evolution.

## Naming and API style

* Follow standard Go initialism and package naming conventions.
* Keep package names short, lowercase, and responsibility-oriented.
* Name protocol-neutral concepts independently from a specific transport.
* Use `New` when a package has one primary construction path; use a qualified constructor when multiple meanings would otherwise be ambiguous.
* Keep receiver names short and consistent within a type.
* Avoid stutter between package and exported names.
* Treat newly introduced protobuf and CLI names as long-lived contracts.

## Test style and placement

* Add or update tests for every behavior change.
* Put tests beside the package that owns the behavior.
* Test observable contracts rather than mirroring implementation details.
* Prefer focused table-driven tests when several inputs exercise the same contract.
* Use `t.Helper()` in reusable test helpers.
* Use `t.Cleanup` for restoring globals, closing resources, canceling contexts, or stopping goroutines.
* Avoid sleeps as synchronization. Use channels, contexts, deadlines, or observable state.
* Keep timeouts bounded and generous enough for CI while still detecting leaks or deadlocks.
* Cover relevant positive, negative, boundary, invalid-input, cancellation, cleanup, repeated-operation, idempotency, stale-state, error-identity, and concurrency cases.
* Assertions must verify meaningful observable behavior strongly enough that plausible regressions fail.
* Include integration coverage when behavior crosses meaningful package or external boundaries instead of relying exclusively on direct method tests.
* Avoid redundant tests with little behavioral value and brittle tests unnecessarily coupled to implementation details.
* Avoid network-dependent tests unless the task explicitly requires integration coverage and the repository provides a deterministic fixture.

Place coverage according to ownership:

* CLI behavior belongs in `cmd/ferretd` tests.
* Daemon lifecycle belongs in `internal/daemon` tests.
* Document state, versioning, diagnostics, and Ferret conversion belong in `internal/language` tests.
* LSP capability and payload translation belong in `internal/lsp` tests.
* URI and position behavior belongs in `internal/source` tests.
* Workspace, execution, and debug behavior belongs in the corresponding manager package plus adapter-level integration tests.
* Protobuf or gRPC work requires generation reproducibility and wire/translation tests, not only direct method tests.

For bug fixes, add a regression test that fails without the fix whenever practical. For concurrency changes, add deterministic lifecycle tests and run the race detector on affected packages.

A passing test suite is evidence of correctness, not evidence that the design is good.

## Development practice expectations

### Core principles

* Preserve correctness and protocol compatibility first.
* Preserve existing observable behavior unless the task explicitly requires changing it.
* Preserve ownership boundaries and lifecycle invariants.
* Prefer the smallest local change that fully solves the task.
* Prefer straightforward, idiomatic Go over clever implementation.
* Reuse an existing repository pattern only after verifying that it follows this guide and fits the same semantics, ownership, and lifecycle. Existing technical debt is not precedent.
* Avoid abstraction, indirection, and generalization without a concrete need.
* Avoid speculative implementation of planned architecture.
* Do not optimize by intuition alone; measure performance-sensitive work.
* Keep behavior, state ownership, dependencies, cancellation, cleanup, and resource lifetimes obvious.
* Leave already-correct code alone.
* Do not treat the first compiling implementation as complete.

### Required workflow for non-trivial changes

1. Identify the subsystem, package, type, or layer that owns the behavior.
2. Identify the observable contract, invariants, lifecycle, resource ownership, error semantics, and compatibility surface being preserved or changed.
3. Read the current source and tests before relying on architecture prose, historical discussion, or assumptions.
4. Choose the smallest coherent design that fits the current ownership boundaries.
5. Determine whether the change is concurrency-, lifecycle-, compatibility-, or performance-sensitive.
6. Establish and retain a focused pre-change benchmark baseline when performance is significant.
7. Add or update correctness tests for the observable behavior.
8. Implement the focused change without collateral cleanup.
9. Run the narrowest validation that directly exercises the change.
10. Broaden validation according to risk with integration, race, lint, build, generation, or repository-wide checks as appropriate.
11. Perform the mandatory final self-review described below.
12. Correct issues introduced by the task and appropriate directly adjacent findings under the scope rules below.
13. Re-run every validation invalidated by review-driven changes.
14. Re-run relevant benchmark comparisons when corrections affect benchmarked code.
15. Inspect the complete final diff as one coherent change.
16. Report implementation, preserved invariants, tests, measurements, review, and unresolved limitations accurately.

Do not perform opportunistic refactors, dependency upgrades, formatting churn, API redesign, package reshuffling, abstraction creation, generated-file changes, documentation rewrites, or implementation of future features unrelated to the task.

## Mandatory final self-review

Every coding task must end with a design and style self-review before it is considered finished. After implementation and initial validation, review all changed and directly adjacent code against this guide as though reviewing another engineer's pull request; for non-trivial work, also inspect the complete diff as a coherent change. The review must evaluate the implementation and design, not merely confirm that compilation, tests, or lint pass.

Review the final change for:

### Correctness

* Verify every requested behavior, explicit non-goal, and existing behavior that must remain unchanged.
* Look for missing cases, regressions, invalid assumptions, boundary conditions, failure paths, and partial state changes.
* Check error identity and context, cancellation, resource cleanup on every path, and close or shutdown idempotency where required.
* Check lifecycle transitions, stale state, version ordering, and externally visible event ordering where applicable.
* Check concurrency behavior, lock scope, goroutine termination, re-entrancy, and callback ordering where applicable.
* Verify protocol conversions, URI handling, source ranges, Unicode behavior, and output-channel purity when touched.
* Verify public and externally observable behavior matches the intended contract.
* Ensure tests would detect plausible regressions rather than merely repeat implementation structure.

### Code clarity and cleanliness

* Look for unnecessary complexity, duplicated behavior or semantics, excessive nesting, awkward control flow, misleading names, oversized functions, and difficult-to-follow execution paths.
* Look for hidden state transitions, hidden ownership, unnecessary mutation, unnecessary indirection, and unrelated responsibilities.
* Remove temporary code, debugging output, dead branches, obsolete helpers, and comments describing abandoned approaches.
* Keep the primary execution path easy to follow and prefer straightforward code whose behavior can be understood locally.
* Simplify only when the result is clearly equivalent and easier to reason about; do not perform optional stylistic rewrites.

### Go design and API quality

* Check API consistency, naming, semantic-type grounding, method-versus-function ownership, constructor behavior, dependency construction, nil semantics, resource lifetimes, enum zero values, and option normalization.
* Check context propagation, error wrapping, synchronization, lock scope, goroutine ownership, lifecycle visibility, and cleanup behavior.
* Look for semantic types bypassed by primitive APIs, free functions containing type-owned behavior, methods with unnatural receivers, required dependencies hidden behind nil defaults, ambiguous resource ownership, repeated normalization, and competing lifecycle representations.
* Verify compliance with the type/file, method ownership, comment, and control-flow rules in this guide.

### Abstraction quality

* Review every new interface, wrapper, helper, manager, factory, generic type, and layer for a concrete current need.
* Verify an abstraction represents a real concept, clarifies ownership, reduces meaningful duplication, and simplifies reasoning.
* Prefer direct concrete code when it is clearer; remove abstractions that do not earn their complexity.
* Do not generalize concepts whose semantics, ownership, or lifecycle differ merely because their implementations look similar.

### Architecture and code organization

* Verify behavior remains in the owning package and dependency direction remains clear.
* Keep protocol types in adapters and protocol-neutral behavior in shared services.
* Keep Ferret-owned semantics in Ferret.
* Avoid exposing new APIs or wire contracts unless the task requires them.
* Check that packages, files, types, methods, functions, helpers, and options each have coherent and predictable responsibilities.
* Look for files or types doing too much, package-level helpers mixed into type-centered files, generic dumping grounds, meaningful concepts hidden locally, excessive extraction, forwarding-only layers, and package fragmentation.
* Keep tightly related behavior cohesive and helpers at the narrowest appropriate ownership level.
* Distinguish implemented behavior from placeholder or planned architecture.

### Comments and documentation

* Re-read comments and documentation directly exposed by the change and correct stale statements within scope.
* Verify comments describe current contracts, invariants, ownership, ordering, and lifecycle rather than abandoned approaches or speculative future architecture.
* Remove obsolete comments instead of adding narration to compensate for confusing code.
* Evaluate user-visible, integration-facing, and public behavior for required documentation synchronization without widening into unrelated cleanup.

### Tests

* Review positive, negative, boundary, invalid-input, cancellation, cleanup, repeated-operation, idempotency, stale-state, error-classification, concurrency, and integration coverage as relevant.
* Check assertions for meaningful observable behavior rather than implementation structure or error strings when identity is the contract.
* Look for weak or redundant coverage, brittle internal coupling, flaky timing, sleeps used for synchronization, leaked goroutines or resources, and mutable global state.
* Ensure deterministic lifecycle coverage for concurrency and appropriate boundary coverage for behavior spanning layers.

### Performance

* For performance-significant changes, inspect allocations, repeated work, copying, conversions, materialization, synchronization, lock contention, blocking work, memory retention, and hot-path overhead.
* Compare final benchmark results against the pre-change baseline under comparable conditions and investigate meaningful regressions.
* Do not rationalize a regression because correctness tests pass or trade clear correctness and maintainability for speculative micro-optimization.

When review finds a problem in the task's change, fix it, add or improve coverage where necessary, and re-run affected validation. Do not leave correctness, lifecycle, protocol, ownership, architecture, or significant test-coverage issues introduced by the task unresolved merely because initial tests passed.

### Scope discipline for review findings

Classify review findings before changing adjacent code:

1. Fix every meaningful deviation introduced by the task.
2. Fix a pre-existing deviation only when the correction is small, local, low-risk, clearly understood, directly within the affected area, and relevant to correctness, ownership, lifecycle, architecture, or maintainability.
3. Report broader architectural cleanup or unrelated technical debt explicitly and leave the implementation unchanged for a separate task.

Do not copy an existing pattern merely because it already exists when that pattern conflicts with this guide. Equally, do not use self-review to justify unrelated cleanup, speculative refactoring, broad package reshuffling, dependency upgrades, or implementation of future features. Distinguish concrete problems from optional preferences and leave clear, correct, idiomatic, appropriately organized code alone. If correcting a discovered issue would materially expand the task, preserve current behavior and call it out in the completion report.

### Final diff inspection

Immediately before finishing, inspect the complete final diff and verify that:

* every changed line belongs to the requested task or a necessary supporting change;
* unrelated user changes remain intact;
* no debugging, temporary, dead, or abandoned implementation remains;
* no accidental behavior, public API, protocol, compatibility, dependency, generated-file, or documentation changes slipped in;
* generated files changed only because their source inputs required regeneration;
* tests describe intended behavior;
* comments describe current contracts and invariants;
* package, file, type, method, and function responsibilities and ownership remain coherent;
* cancellation, concurrency, cleanup, and resource lifetimes remain correct;
* formatting churn and unrelated whitespace changes are absent;
* the result is the smallest coherent change that fully solves the task.

If final inspection causes another edit, repeat the affected validation afterward.

## Significant changes and benchmarks

A change is significant when it could reasonably affect:

* request or diagnostic latency;
* repeated source mapping or compilation cost;
* allocation patterns for open documents or protocol payloads;
* memory usage, retention, caching, pooling, or materialization cost;
* lock contention or concurrency throughput;
* resource cleanup or lifetime;
* daemon startup, shutdown, or long-running memory behavior;
* execution or debug-session throughput.

When uncertain whether a change affects a hot path, treat it as performance-significant and measure it.

For significant changes:

* identify or add a focused benchmark;
* run it before the implementation and save a baseline;
* run the same benchmark after the implementation;
* compare `ns/op`, `B/op`, and `allocs/op` where applicable;
* investigate meaningful regressions;
* report the command and comparison accurately.

Inspect significant changes for accidental allocations, unnecessary copying, repeated conversions or computation, avoidable materialization or synchronization, lock contention, blocking work, hot-path overhead, and resources retained longer than necessary. Do not trade clear correctness or maintainability for speculative micro-optimization.

Documentation-only, test-only, pure rename, formatting-only, and narrow non-hot-path refactoring changes are normally not significant. If benchmark tooling or the environment is unavailable, state that explicitly rather than claiming benchmark validation.

## Command matrix

Run commands from the repository root.

* Download dependencies: `make install`
* Compile the binary: `make compile`
* Run all Go tests: `go test ./...` or `make test`
* Run vet: `go vet ./...` or `make vet`
* Run static analysis and revive: `make lint`
* Format Go code: `make fmt`
* Run the broad local build gate: `make build`
* Install lint and formatting tools: `make install-tools`
* Generate daemon/workspace/execution protobuf code: `make generate`
* Lint all protobuf sources: `make proto-lint`
* Verify checked-in generated code: `make check-generate`

`make build` runs vet, lint, tests, and compilation. It therefore requires `staticcheck`, `revive`, and the normal Go toolchain in addition to project dependencies.

`make generate` uses the pinned Buf CLI and pinned Go protobuf plugins declared by the repository. It intentionally generates only implemented daemon/workspace/execution contracts. Always inspect generated diffs and run `make check-generate` when contracts or generation configuration change.

`make fmt` mutates Go files. Use it only when formatting changes are within task scope, and inspect the resulting diff for unrelated churn.

The release target creates and pushes a tag. Run `make release <version>` only when the user explicitly requests a release and the release preconditions have been verified.

## Validation expectations

Run the narrowest validation that proves the changed behavior, then broaden according to risk.

* Handwritten Go changes: format the affected code, inspect formatting churn, and keep unrelated files untouched.
* Package-local Go changes: run `go test` for the affected package first.
* Cross-package language or LSP changes: run affected package tests, then `go test ./...`.
* Shared-state or goroutine changes: run every affected domain and adapter package with `-race`, then broader tests as appropriate; do not assume the current CI race-package list covers a newly affected package.
* CLI or daemon lifecycle changes: run the relevant package tests and compile the binary.
* Lint-sensitive or exported-contract changes: run `make lint` when the required tools are available.
* Broad or release-facing changes: finish with `make build` when the environment supports the toolchain.
* After focused validation, run `go test ./...` for Go changes when practical; explain material environment or scope limitations when it is omitted.
* Documentation-only changes: validate exact scope, referenced commands and paths, Markdown structure, whitespace, and the complete diff. Do not run unrelated code tests merely to create validation theater.

Do not run unrelated expensive validation merely to create evidence. Conversely, do not stop at a narrow unit test when the change affects behavior across packages or external boundaries.

After review-driven code changes, re-run every command whose result may have been invalidated.

If validation cannot be completed because of tooling, environment, permissions, or external dependencies, report the limitation explicitly.

When finishing a non-trivial change, report:

* owning subsystem;
* files changed;
* behavior changed and important behavior or invariants preserved;
* tests added or updated;
* validation commands actually run;
* race-detector validation, if applicable;
* benchmarks, commands, and baseline comparison, if applicable;
* final self-review completion and meaningful corrections;
* noteworthy pre-existing design or style issues intentionally left outside scope;
* remaining concerns, limitations, or environmental and tooling failures.

Never claim tests, lint, builds, race detection, benchmarks, generation, or review succeeded unless the work was actually completed. Accuracy of the completion report is part of engineering quality.

## Editing and change-discipline rules

* Preserve unrelated dirty or untracked files.
* Keep the diff focused on the requested behavior.
* Do not modify unrelated code merely to make it conform stylistically or use an implementation task as an excuse to clean up the surrounding repository.
* Do not update dependencies unless the task requires a dependency change.
* Do not edit files under `gen/` manually; update protobuf sources/configuration and regenerate.
* Inspect generated diffs whenever generation is required.
* Do not add protocol, session, workspace, or debug abstractions for hypothetical future use.
* Avoid changing CLI, LSP, or protobuf contracts as collateral cleanup.
* Keep documentation statements precise about current versus planned support.
* If a necessary cleanup directly supports correctness, lifecycle safety, or maintainability of the requested change, keep it narrow and explain it.

## Documentation synchronization

Update repository documentation when a change affects user-visible or integration-facing behavior.

* Update `README.md` for commands, supported capabilities, requirements, or current status.
* Update `docs/lsp.md` for editor setup, LSP capabilities, synchronization, or language features.
* Update `docs/architecture.md` for ownership boundaries or intended service relationships.
* Keep implemented behavior and future plans clearly separated.
* Do not document the debug placeholder protobuf service as a running endpoint.
* Keep examples consistent with actual command names, arguments, and transport behavior.

Documentation synchronization is part of a behavior change when existing documentation would otherwise become incorrect. It does not authorize unrelated documentation cleanup.

## Decision bias when uncertain

When uncertain:

* verify current source and tests before relying on architecture prose;
* preserve existing observable behavior;
* keep protocol adapters thin;
* keep Ferret semantics in Ferret;
* prefer the smaller local change;
* make cancellation, ownership, and lifecycle explicit;
* add a focused test;
* measure before optimizing;
* treat CLI, LSP, and protobuf changes as compatibility-sensitive;
* leave already-correct code alone.

## Definition of done

A non-trivial coding task is complete only when:

* ownership, contracts, invariants, lifecycle, and compatibility were understood;
* the requested behavior is implemented and relevant existing behavior is preserved;
* meaningful tests and risk-appropriate validation have passed;
* race detection and performance measurement were completed when applicable;
* the implementation underwent mandatory design and style self-review;
* problems introduced by the task were corrected;
* affected validation and benchmarks were repeated after corrections;
* the complete final diff was inspected as one coherent change;
* the final change is focused, comprehensible, and appropriately designed;
* results, limitations, and unresolved follow-up work are reported accurately.

Compiling is not completion. Passing tests is not completion. The standard is a correct, clean, appropriately designed, well-tested, deliberately reviewed change.

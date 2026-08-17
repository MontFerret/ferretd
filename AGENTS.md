# AGENTS.md

This file is the canonical operating guide for coding agents working in this repository. It describes the current experimental `ferretd` implementation, not an aspirational implementation of the architecture. If repository documentation conflicts with this file, prefer `Makefile`, `go.mod`, `.github/workflows/ci.yml`, and the current tests for commands, toolchains, CI behavior, and executable contracts.

## Repo snapshot

* Module path: `github.com/MontFerret/ferretd`
* Go version in `go.mod`: Go 1.26.1
* `ferretd` is the experimental long-running developer service for Ferret.
* The current executable provides `serve`, `lsp`, and version/help behavior.
* The current language server tracks open `.fql` documents and publishes Ferret parser and compiler diagnostics over LSP stdio.
* `serve` exposes daemon information, API negotiation, graceful shutdown, workspace lifecycle, and execution sessions over a local gRPC transport.
* Opening a workspace synchronously discovers `.fql` files, retains daemon-owned source, syntax state, and document diagnostics, and constructs one rooted read-write Ferret engine until explicit close.
* The workspace and execution-session managers are implemented; the debug-session package remains a boundary placeholder.
* Buf generates the implemented daemon/workspace/execution protobuf and gRPC packages under `gen/`. Debug protobufs remain ungenerated placeholders.
* Ferret remains the owner of FQL parsing, compilation, runtime semantics, VM execution, and core debugging behavior.

Do not infer implemented behavior from architecture diagrams, placeholder service definitions, future-facing type names, or historical discussion. Current source, tests, dependency contracts, and build configuration are authoritative.

## Architectural mental model

The implemented language-tooling flow is:

```text
editor or language client
    -> ferretd lsp over stdin/stdout
    -> internal/lsp protocol adapter
    -> internal/language protocol-neutral document service
    -> Ferret compiler and diagnostics
    -> internal/source rune-to-UTF-16 mapping
    -> LSP diagnostics notification
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
    -> LSP, future DAP, or versioned gRPC adapters
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
* `internal/exec` owns execution-session coordination as that capability is implemented.
* `internal/debug` owns debug-session coordination as that capability is implemented.
* `client` owns the supported Go client, compatibility negotiation, and public error classification.
* `proto/ferretd` owns versioned daemon RPC source contracts; `gen/` is checked-in generated output.
* The Ferret dependency owns parsing, compilation, diagnostics primitives, runtime semantics, VM execution, and core debugging machinery.

Protocol adapters should translate and delegate. They must not become alternate owners of language, execution, workspace, or debugging behavior.

## Canonical invariants

* `ferretd` coordinates Ferret capabilities; it does not replace or fork Ferret's compiler, runtime, VM, or language semantics.
* The language service is protocol-neutral. LSP-specific request and response types stay in `internal/lsp`.
* LSP stdout is protocol-only. Do not print logs, progress messages, diagnostics, or ordinary CLI text to stdout while serving LSP.
* The current language server uses full-document synchronization. Incremental text edits are rejected.
* Open document contents are supplied by the client, kept in memory, and removed on close. The current language service does not read document contents from disk.
* A document change must advance the stored version. Stale or same-version changes are rejected.
* Closing an unknown document is safe and idempotent.
* Current document URIs must be local `file` URIs. Unsupported schemes, non-local hosts, queries, fragments, and empty paths remain errors.
* Ferret compiler spans use rune offsets. Protocol-neutral positions and LSP positions use zero-based lines and UTF-16 code units with half-open ranges.
* Source mapping must clamp invalid offsets safely and preserve CR, LF, CRLF, Unicode, and astral-character behavior.
* Shared service state must remain safe for concurrent callers. Do not bypass or leak mutable state protected by service synchronization.
* Context cancellation must remain effective at process and service boundaries. Long-running operations must not outlive their owning context without an explicit lifecycle reason.
* Daemon shutdown must remain safe after cancellation and `Stop` must remain idempotent.
* The daemon remains local-only: Unix sockets or Windows named pipes, permission-restricted to the current user, with no TCP, TLS, or remote mode.
* API major mismatch is rejected; minor versions are additive. Workspace, Session, and Execution state survive client disconnects but not daemon restarts.
* Workspace roots are existing absolute directories, cleaned without resolving symlinks; repeated opens converge and closes are idempotent.
* Workspace loading is synchronous and static. It retains lowercase `.fql` regular files recursively, skips nested symlinks, and requires close/reopen to observe disk changes.
* `.git`, `.hg`, `.svn`, `node_modules`, and `vendor` directories are pruned during discovery. There is no ignore-file or project-manifest contract.
* Workspace documents retain source and Ferret syntax state only. Compilation into immutable Plans and one-shot runtime state belong to execution Sessions and Executions; editor overlays and filesystem watching remain separate capabilities.
* Document load and syntax diagnostics do not fail an otherwise coherent workspace; fatal root/discovery failures do not leave manager entries.
* A Session owns one immutable compiled Ferret Plan. An Execution owns one fresh runtime Session, one run attempt, isolated parameters, terminal output or failure, and bounded lifecycle observation.
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
    * Keep command parsing and process-facing error context here.
    * Preserve protocol-pure stdout for `lsp`.
    * Do not move language, workspace, execution, debug, or transport-independent lifecycle behavior into `main`.

### Daemon coordination

* `internal/daemon`
    * Owns construction and lifecycle coordination for the services that make up `ferretd`.
    * Keep orchestration thin and delegate behavior to the owning service.
    * `Start` listens and serves until context cancellation, RPC shutdown, explicit stop, or transport failure.
    * `Stop` marks health unavailable, closes execution resources, clears workspace engines, drains or force-stops RPCs, closes the listener, and remains idempotent.

### Protocol-neutral language service

* `internal/language`
    * Owns open-document snapshots, document version rules, protocol-neutral diagnostics, and calls into the Ferret compiler.
    * The service stores document values rather than exposing mutable internal references.
    * Keep Ferret diagnostic extraction and conversion here when it is independent of a wire protocol.
    * Preserve stable error identity through `errors.Is` when adding context to document lifecycle errors.
    * Do not introduce LSP types into this package.
    * Do not reimplement parser, compiler, or diagnostic semantics already owned by Ferret.

### LSP adapter

* `internal/lsp`
    * Owns Language Server Protocol capability advertisement, lifecycle handlers, request/notification translation, and LSP diagnostic projection.
    * Keep the adapter thin and delegate document behavior to `internal/language`.
    * Preserve full-document synchronization until incremental changes are intentionally implemented through the protocol-neutral service.
    * Serialize document lifecycle handling where required to keep notification order and shared document state coherent.
    * Publish diagnostics with the current document version when available and clear diagnostics when a document closes.
    * LSP callbacks and notifications are wire-facing behavior; test both translated payloads and delegated state changes.
    * Never write non-protocol output to LSP stdout.

### Source locations and mapping

* `internal/source`
    * Owns protocol-neutral `URI`, `Position`, `Range`, and `Span` concepts used by services and adapters.
    * Owns conversion between local filesystem paths and escaped absolute file URIs.
    * Owns conversion from Ferret's rune-indexed spans to zero-based UTF-16 positions.
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
    * Owns concurrency-safe compiled Session and one-shot Execution coordination, cancellation, terminal state, bounded watchers, and parent-child cleanup.
    * Ferret remains the owner of compilation, runtime semantics, and VM execution.
    * Sessions retain immutable Plans; each Execution creates and closes a fresh Ferret runtime Session and runs it at most once.
* `internal/debug`
    * Is the future owner of debug-session coordination and protocol-neutral daemon state.
    * Ferret remains the owner of core debugging and execution behavior.
    * The current session manager is a boundary placeholder and does not implement DAP or debugging sessions.

Do not add speculative abstractions to placeholder packages. Add state, interfaces, or lifecycle machinery only when an implemented capability requires them.

### Protobuf contracts

* `proto/ferretd/workspace/v1`
    * Contains the implemented `WorkspaceService` v1 source contract.
* `proto/ferretd/daemon/v1`
    * Contains the implemented `DaemonService` v1 source contract and compatibility detail.
* `proto/ferretd/execution/v1`
    * Contains the implemented `ExecutionService` v1 source contract.
* `proto/ferretd/debug/v1`
    * Contains the placeholder `DebugService` v1 source contract.

Buf v2 configuration at the repository root pins generation through `make generate`; checked-in output belongs under `gen/` and must never be edited manually. `make proto-lint` validates all source contracts, while generation intentionally targets daemon/workspace/execution v1. Keep debug ungenerated until its service is implemented.

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

* Cobra-based `serve`, `lsp`, help, and version behavior;
* signal-aware process cancellation;
* local Unix-socket and Windows named-pipe gRPC serving;
* API v1.1 negotiation, daemon information, health, and graceful shutdown;
* supported Go client discovery, dialing, negotiation, and error classification;
* concurrency-safe, process-local workspace open/get/list/close behavior;
* deterministic root-confined `.fql` discovery with fixed directory exclusions and nested-symlink avoidance;
* daemon-owned file and document snapshots with source contents, revision, Ferret parse state, and syntax/load diagnostics;
* one rooted read-write Ferret engine per open workspace;
* immutable compiled Sessions and isolated one-shot Executions with JSON-shaped parameters and encoded output;
* asynchronous run, cancellation, terminal retention, and latest-plus-future bounded lifecycle watches;
* workspace-to-Session-to-Execution close cascades and daemon-owned cleanup;
* LSP over stdio;
* open, full-document change, and close notifications;
* in-memory open-document snapshots and version checks;
* Ferret parser/compiler diagnostics for open documents;
* source-span to UTF-16 range conversion;
* pinned protobuf generation for daemon/workspace/execution v1;
* placeholder debug protobuf source contract.

Not currently implemented:

* DAP transport;
* debug-session behavior;
* durable workspace persistence or eviction;
* module resolution;
* LSP document loading from daemon workspaces or disk;
* filesystem watching, editor overlays, automatic reload, and incremental workspace parsing;
* incremental document synchronization;
* completion, hover, formatting, navigation, semantic tokens, or code actions.

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
    * preserve synchronous static loading, root confinement, deterministic ordering, and recoverable document diagnostics unless the task explicitly changes them
    * define workspace identity, ownership, lifecycle, and concurrency before exposing new behavior through an adapter
* Add or change execution sessions:
    * begin in `internal/exec`
    * use Ferret's public embedding/runtime contracts rather than duplicating execution semantics
    * define cancellation, result ownership, cleanup, and session lifetime explicitly
* Add debug sessions or DAP:
    * begin with protocol-neutral coordination in `internal/debug`
    * keep protocol translation in a dedicated adapter
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
* update user-facing documentation when support or configuration changes;
* avoid exporting Go APIs merely to share implementation across internal packages.

## Protocol and adapter rules

* Keep adapters thin: validate and translate protocol values, delegate to protocol-neutral services, and translate results back.
* Do not let LSP, future DAP, or gRPC types leak into shared service packages.
* Do not place transport lifecycle, serialization, or notification behavior in domain services.
* Validate unsupported protocol forms explicitly. Do not silently reinterpret incremental changes as full-document changes.
* Keep output channels pure. In particular, LSP stdout contains framed protocol messages only.
* Preserve request and notification ordering where state transitions depend on it.
* Avoid holding service locks while performing transport callbacks, blocking I/O, or potentially long Ferret operations unless a documented invariant requires it.
* Map errors at boundaries without destroying underlying identity needed by `errors.Is` or `errors.As`.
* Test translation separately from the underlying service when practical.

## Context, lifecycle, and concurrency rules

* Accept `context.Context` at operation boundaries that can block, be canceled, perform I/O, or participate in a caller-owned lifecycle.
* Check or propagate cancellation early enough to avoid committing state after cancellation.
* Do not store contexts in long-lived structs. Store explicit lifecycle state and cancellation functions when ownership requires them.
* Do not replace an available caller context with `context.Background()` without a concrete protocol or lifecycle reason.
* Every goroutine must have a clear owner, termination condition, and cleanup path.
* Avoid goroutine leaks on normal completion, errors, cancellation, partial startup, and repeated shutdown.
* Keep lock scope narrow and make the protected state obvious.
* Do not call unknown or external code while holding a lock unless the ordering requirement is explicit and tested.
* Return copies or immutable views when callers must not mutate synchronized internal state.
* Preserve ordering between document state changes and diagnostic publication.
* Test cancellation, idempotent cleanup, stale state, and concurrent access where the behavior is meaningful.
* Use the race detector for changes that add or materially alter shared mutable state or goroutine coordination.

## Error-handling rules

* Use standard `errors` and `%w` wrapping so callers can inspect error identity.
* Add context at subsystem and process boundaries without repeating the entire call chain.
* Error strings should be lowercase sentence fragments unless they contain a proper name or protocol-defined text.
* Keep sentinel errors for stable conditions callers need to classify.
* Do not compare error strings in production code when `errors.Is`, `errors.As`, or a typed error can express the contract.
* Distinguish cancellation, invalid client input, missing state, stale state, dependency failures, transport failures, and internal invariants where callers need different behavior.
* Do not log and return the same error at every layer. The owning process or transport boundary should decide how to report it.
* Never write errors to protocol stdout.

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

* A file centered on a method-bearing type should contain that type, its methods, and its constructors.
* Constructors are the normally allowed package-level functions in a type-centered file.
* If logic conceptually belongs to the primary type, implement it as a method.
* If logic is genuinely package-level, place it in a separate helper-focused file.
* Do not mix unrelated package-level helpers into a type-centered file.
* Keep protocol conversion helpers near the adapter concern they serve.
* Keep Ferret-to-protocol-neutral conversion in the language or source layer, not in process setup.
* Prefer the narrowest ownership level that keeps behavior testable and avoids duplicate semantics.
* Do not introduce interfaces until there are meaningful alternate implementations, a required test seam, or an external contract.

## Comment rules

* Do not add comments to every function or method by default.
* Exported declarations should have useful doc comments, even in `internal` packages, when they define package-facing contracts.
* Comment unexported code only when it carries non-obvious behavior, invariants, side effects, ownership, synchronization, lifecycle, protocol, or compatibility constraints.
* Explain why the code exists, what must remain true, or how ownership works.
* Do not restate names or signatures.
* Keep future plans out of code comments unless the comment describes a deliberate current boundary.
* Update or remove comments when implementation makes them obsolete.
* Prefer dense, meaningful comments over comment wallpaper.

Preferred:

```go
// OffsetToPosition converts a Ferret rune offset to a zero-based UTF-16
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

## Naming and API style

* Follow standard Go initialism and package naming conventions.
* Keep package names short, lowercase, and responsibility-oriented.
* Avoid package names such as `util`, `common`, or `helpers` that obscure ownership.
* Name protocol-neutral concepts independently from a specific transport.
* Use `New` when a package has one primary construction path; use a qualified constructor when multiple meanings would otherwise be ambiguous.
* Keep receiver names short and consistent within a type.
* Avoid stutter between package and exported names.
* Prefer concrete types until an interface is required by a real boundary.
* Do not export symbols from internal packages without a package-to-package need.
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
* Verify both success and failure paths, including error identity when it is part of the contract.
* Avoid network-dependent tests unless the task explicitly requires integration coverage and the repository provides a deterministic fixture.

Place coverage according to ownership:

* CLI behavior belongs in `cmd/ferretd` tests.
* Daemon lifecycle belongs in `internal/daemon` tests.
* Document state, versioning, diagnostics, and Ferret conversion belong in `internal/language` tests.
* LSP capability and payload translation belong in `internal/lsp` tests.
* URI and position behavior belongs in `internal/source` tests.
* Future workspace, execution, and debug behavior belongs in the corresponding manager package plus adapter-level integration tests.
* Protobuf or gRPC work requires generation reproducibility and wire/translation tests, not only direct method tests.

For bug fixes, add a regression test that fails without the fix whenever practical. For concurrency changes, add deterministic lifecycle tests and run the race detector on affected packages.

## Development practice expectations

### Core principles

* Preserve correctness and protocol compatibility first.
* Preserve ownership boundaries and lifecycle invariants.
* Prefer the smallest local change that fully solves the task.
* Adapt an existing repository pattern before introducing a new abstraction.
* Avoid speculative implementation of planned architecture.
* Do not optimize by intuition alone; measure performance-sensitive work.
* Keep behavior, state ownership, cancellation, and cleanup obvious.
* Do not treat the first compiling implementation as complete.

### Required workflow for non-trivial changes

1. Identify the owning package and observable contract.
2. Identify the current invariant, lifecycle, protocol behavior, or compatibility surface being preserved or changed.
3. Choose the smallest implementation that fits the current architecture.
4. Determine whether the change is performance-significant or concurrency-sensitive.
5. Add or update focused correctness tests.
6. Add or update benchmarks when the change is performance-significant.
7. Run the narrowest relevant validation first, then broaden as appropriate.
8. Perform the mandatory final self-review described below.
9. Correct issues found during self-review without widening scope.
10. Re-run validation affected by review-driven corrections.
11. Inspect the complete final diff as a whole.
12. Report implementation, tests, benchmarks, review, and limitations accurately.

Do not perform opportunistic refactors, dependency upgrades, formatting churn, generated-file changes, or documentation rewrites unrelated to the task.

## Mandatory final self-review

After implementation and initial validation for every non-trivial task, review the complete change before considering the task finished. The review must evaluate the implementation, not merely confirm that tests pass.

Review the final change for:

### Correctness

* Verify every requested behavior and explicit non-goal.
* Look for missing cases, regressions, invalid assumptions, boundary conditions, and partial state changes.
* Check cancellation, cleanup, idempotency, error identity, version ordering, and lifecycle transitions where applicable.
* Check concurrency behavior, lock scope, goroutine termination, and callback ordering where applicable.
* Verify protocol conversions, URI handling, source ranges, Unicode behavior, and output-channel purity when touched.
* Ensure tests would detect plausible regressions rather than merely repeat implementation structure.

### Code clarity and Go practices

* Look for unnecessary abstraction, duplication, nesting, misleading names, awkward control flow, and hidden state ownership.
* Prefer straightforward idiomatic Go over clever implementations.
* Check error wrapping, context propagation, synchronization, resource ownership, and cleanup.
* Remove temporary code, debugging output, dead branches, obsolete helpers, and comments describing abandoned approaches.
* Verify compliance with the type/file, method ownership, comment, and control-flow rules in this guide.

### Architecture and organization

* Verify behavior remains in the owning package and dependency direction remains clear.
* Keep protocol types in adapters and protocol-neutral behavior in shared services.
* Keep Ferret-owned semantics in Ferret.
* Avoid exposing new APIs or wire contracts unless the task requires them.
* Check that files, types, methods, helpers, and packages each have a coherent responsibility without excessive fragmentation.
* Distinguish implemented behavior from placeholder or planned architecture.

### Tests and performance

* Review positive, negative, boundary, cancellation, cleanup, stale-state, and concurrency coverage as relevant.
* Check assertions for meaningful observable behavior and error classification.
* Look for flaky timing, leaked goroutines, mutable global state, or unnecessary dependence on implementation details.
* For significant changes, inspect allocations, repeated conversions, lock contention, blocking work, and hot-path overhead.
* Compare relevant benchmark results against a baseline when performance is in scope.

When review finds a problem, fix it, add or improve coverage where necessary, and re-run affected validation. Do not leave known correctness, lifecycle, protocol, ownership, architecture, or significant test-coverage issues unresolved merely because initial tests passed.

Do not use self-review to justify unrelated cleanup, speculative refactoring, broad package reshuffling, dependency upgrades, or implementation of future features.

### Final diff inspection

Immediately before finishing, inspect the complete final diff and verify that:

* every changed line belongs to the requested task or a necessary supporting change;
* unrelated user changes remain intact;
* no debugging or temporary artifacts remain;
* no accidental behavior, API, protocol, dependency, generated-file, or documentation changes slipped in;
* tests describe intended behavior;
* comments describe current contracts and invariants;
* package, file, type, and function ownership remains coherent;
* cancellation, concurrency, cleanup, and resource lifetimes remain correct;
* the result is the smallest coherent change that fully solves the task.

If final inspection causes another edit, repeat the affected validation afterward.

## Significant changes and benchmarks

A change is significant when it could reasonably affect:

* request or diagnostic latency;
* repeated source mapping or compilation cost;
* allocation patterns for open documents or protocol payloads;
* lock contention or concurrency throughput;
* daemon startup, shutdown, or long-running memory behavior;
* execution or debug-session throughput once those features exist.

For significant changes:

* identify or add a focused benchmark;
* run it before the implementation and save a baseline;
* run the same benchmark after the implementation;
* compare `ns/op`, `B/op`, and `allocs/op` where applicable;
* investigate meaningful regressions;
* report the command and comparison accurately.

Documentation-only, test-only, pure rename, and narrow non-hot-path refactoring changes are normally not significant. If benchmark tooling or the environment is unavailable, state that explicitly rather than claiming benchmark validation.

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

The release target creates and pushes a tag. Run `make release TAG=<version>` only when the user explicitly requests a release and the release preconditions have been verified.

## Validation expectations

Run the narrowest validation that proves the changed behavior, then broaden according to risk.

* Package-local Go changes: run `go test` for the affected package first.
* Cross-package language or LSP changes: run affected package tests, then `go test ./...`.
* Shared-state or goroutine changes: run affected tests with `-race`, then broader tests as appropriate.
* CLI or daemon lifecycle changes: run the relevant package tests and compile the binary.
* Lint-sensitive or exported-contract changes: run `make lint` when the required tools are available.
* Broad or release-facing changes: finish with `make build` when the environment supports the toolchain.
* Documentation-only changes: validate exact scope, referenced commands and paths, Markdown structure, whitespace, and the complete diff. Do not run unrelated code tests merely to create validation theater.

After review-driven code changes, re-run every command whose result may have been invalidated.

When finishing a non-trivial change, report:

* owning subsystem;
* files changed;
* behavior and invariants changed or preserved;
* tests added or updated;
* validation commands actually run;
* benchmarks and baseline comparison, if applicable;
* final self-review completion and meaningful corrections;
* remaining limitations or environmental failures.

Never claim tests, lint, builds, benchmarks, generation, or review succeeded unless the work was actually completed.

## Editing and change-discipline rules

* Preserve unrelated dirty or untracked files.
* Keep the diff focused on the requested behavior.
* Do not update dependencies unless the task requires a dependency change.
* Do not edit files under `gen/` manually; update protobuf sources/configuration and regenerate.
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

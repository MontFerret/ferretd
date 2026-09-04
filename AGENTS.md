# AGENTS.md

This file is the canonical operating guide for coding agents working in this
repository. It contains rules that apply to essentially every non-trivial
change. Detailed contributor documentation for individual subsystems lives
under `docs/development/` and should be read only when relevant to the task.

## Purpose and sources of truth

`ferretd` is the experimental long-running developer service for Ferret. It
coordinates language tooling, local workspaces, execution, and debugging while
Ferret remains the owner of FQL parsing, compilation, runtime semantics, VM
execution, and core debugger behavior.

Describe the implementation that exists, not an aspirational architecture.
When information conflicts, use this precedence:

1. current source, tests, and dependency contracts for executable behavior;
2. `go.mod` for the Go toolchain and dependencies;
3. `Makefile` and `.github/workflows/` for commands and CI or release behavior;
4. `buf.yaml` and `buf.gen.yaml` for protobuf linting and generation;
5. `.goreleaser.yml` and `scripts/` for versioning and release behavior;
6. repository documentation for explanation and design direction.

Do not infer implemented behavior from diagrams, placeholder declarations,
future-facing names, historical discussion, or another Ferret checkout. Verify
the exact dependency selected by `go.mod` when behavior crosses into Ferret.

## Development documentation

Before making a substantial subsystem change, read the relevant guide. Do not
require every guide for every task.

* Architecture and package boundaries: `docs/development/architecture.md`
* Daemon, local transport, gRPC, client, and protobufs:
  `docs/development/daemon.md`
* Workspace discovery and retained source: `docs/development/workspace.md`
* Language service, source mapping, and LSP: `docs/development/language.md`
* Compiled Sessions and one-shot Executions:
  `docs/development/execution.md`
* Debug Sessions and DAP: `docs/development/debugging.md`
* Build, generation, CI, and releases: `docs/development/release.md`

The user-facing protocol guides remain `docs/lsp.md` and `docs/dap.md`.

## Universal architecture and ownership

Begin with the package that owns the behavior. Protocol adapters validate and
translate; they do not become alternate owners of domain behavior.

* `cmd/ferretd` owns process startup, Cobra command behavior, signal handling,
  process-facing output, and top-level composition.
* `internal/daemon` owns long-running service lifecycle and coordination.
* `internal/transport` owns local endpoint discovery, listening, and dialing.
* `internal/grpc`, `internal/lsp`, and `internal/dap` own their protocol
  translation, framing, handles, and transport-facing state.
* `internal/language` owns protocol-neutral editor overlays, snapshot
  resolution, analysis coordination, and language features.
* `internal/workspace` owns process-local workspace identity, discovery,
  retained source and syntax state, refresh, and close coordination.
* `internal/exec` owns compiled Sessions, the shared per-run execution state,
  caller parameters (`Parameters`), one-shot Executions, lazy debug Plans,
  DebugRuntime leases, and execution lifecycle observation. It borrows one
  composition-owned `api.Runtime` and never closes it.
* `internal/debug` owns retained DebugSessions, debugger commands, inspection,
  events, and debug child cleanup.
* `internal/ferretapi` is the provisional and sole bridge between the
  Universal Runtime API and native Ferret runtime, Plan, Session, debugger, and
  diagnostic types.
* `internal/source`, `internal/diagnostic`, and `internal/lifecycle` own their
  protocol-neutral shared concepts. Do not move those semantics into adapters
  or process setup.
* `client` is the supported public Go client. Keep generated protobuf details
  behind its API and preserve its error-classification contracts.
* `proto/ferretd` owns versioned wire source contracts. `gen/` contains
  checked-in generated output and must not be edited manually.
* The Ferret dependency owns language, compiler, runtime, VM, standard-library,
  and core debugger semantics. Execution and debug domain packages use the
  Universal API; native runtime translation stays in `internal/ferretapi`.
  Change Ferret semantics in Ferret rather than copying or redefining them here.

Keep dependency direction clear: commands compose adapters and services;
adapters depend on protocol-neutral services; execution and debug consume the
Universal API; native runtime translation stays in `internal/ferretapi`. Do not
export internal APIs merely to share implementation across packages.

## Compatibility and observable behavior

Treat observable behavior as intentional until the task explicitly changes it.
Compatibility-sensitive surfaces include:

* public `client` APIs and error identity;
* CLI commands, arguments, version text, exit behavior, cancellation, and
  stdout/stderr separation;
* LSP and DAP capabilities, synchronization or launch semantics, event ordering,
  coordinates, payloads, handles, and protocol-pure stdout;
* protobuf packages, `go_package` paths, services, RPCs, messages, field numbers,
  field types, enum values, and version directories;
* local endpoint syntax, API negotiation, source URI acceptance, source ranges,
  and other integration-visible conversions.

For a compatibility-sensitive change, make the behavior change explicit, add
focused coverage at the observable boundary, preserve old behavior unless
incompatibility is required, and update affected user-facing documentation.
Do not implement planned architecture as collateral cleanup.

LSP and DAP stdout contain framed protocol messages only. Logs, progress,
ordinary CLI text, and process-facing errors must go elsewhere. Generated
protobuf code changes only through source/configuration changes and regeneration.

## Protocol and boundary rules

* Keep LSP, DAP, gRPC, and generated protobuf types in their adapters.
* Put reusable behavior below adapters even when only one adapter calls it.
* Keep wire identifiers, handles, framing, client capability state,
  serialization, and notifications in the owning adapter.
* Validate unsupported protocol forms explicitly; do not silently reinterpret
  them as supported forms.
* Preserve request and notification ordering when state transitions depend on
  it.
* Map errors without destroying identities needed by `errors.Is` or `errors.As`.
* Avoid transport callbacks, blocking I/O, Ferret operations, or unknown
  re-entrant code while holding service locks unless an explicit, tested
  ordering invariant requires it.
* Test adapter translation separately from domain behavior when practical.

## Context, lifecycle, and concurrency

* Accept `context.Context` at operations that block, perform I/O, run potentially
  long work, or participate in caller-owned lifecycles.
* Require non-nil contexts at operation boundaries. Do not silently replace a
  nil caller context with `context.Background()`.
* Check or propagate cancellation before committing state. Do not store contexts
  in long-lived structs; store explicit state and cancellation functions.
* Long-running work must not outlive its owner without a concrete lifecycle
  reason. Every goroutine needs an owner, termination condition, and cleanup path.
* Keep cleanup correct on success, errors, cancellation, partial startup, and
  repeated close or shutdown. Preserve idempotency where it is part of the
  contract.
* Identify the lock protecting each state group. Keep lock scope narrow and
  return copies or immutable views when callers must not mutate shared state.
* Prefer one authoritative lock-owned lifecycle representation. Do not combine
  mutexes, atomics, channels, and once-guards for the same transition without a
  concrete ordering or performance reason.
* Preserve ordering between state changes and externally visible events.
* Share lifecycle machinery only when semantics and ownership genuinely match;
  similar execution and debug state does not make one generic session model.
* Test cancellation, cleanup, stale state, repeated operations, and concurrency
  deterministically. Avoid sleeps; use channels, contexts, deadlines, or
  observable state.
* Run the race detector for packages whose shared state or goroutine coordination
  changes materially.
* Concurrency comments explain ownership, invariants, and non-obvious ordering,
  not individual statements.

## Error handling

* Use standard `errors` and `%w` wrapping so callers can inspect identity.
* Add context at subsystem and process boundaries without repeating the entire
  call chain.
* Error strings are lowercase sentence fragments unless they contain a proper
  name or protocol-defined text.
* Use sentinel errors for stable classifiable conditions and typed errors for
  meaningful structured contracts. Do not compare production error strings.
* Distinguish cancellation, invalid input, missing or stale state, dependency or
  transport failures, domain failures, and internal invariant violations when
  callers need different behavior.
* Do not log and return the same error at every layer. The process or transport
  boundary decides how to report it.
* Never write errors to protocol stdout.

## Go design and API ownership

Use Effective Go, Go Code Review Comments, and standard-library conventions as
the general baseline. The repository-specific rules below take precedence where
they make a deliberate choice.

### Semantic types, dependencies, and resources

* Introduce a named type when it can own semantics, invariants, behavior,
  validation, conversion, lifecycle, or API safety. Otherwise use the underlying
  type.
* APIs for an established semantic type should accept or return it rather than
  bypassing it with primitives. Keep intrinsic behavior with that type.
* Required peer-service dependencies are explicit. Constructors must not treat a
  nil required dependency as a request to construct a hidden default.
* Construct required services once at a clear composition root. Optional
  dependencies may have defaults only when optionality and the default are
  intentional.
* Avoid service locators, hidden globals, implicit initialization, and normally
  valid nil receivers for domain objects.
* Make resource ownership and cleanup visible in APIs. State whether a resource
  is owned, borrowed, leased, or transferred when cleanup depends on it.
* Release partially acquired resources on every failure path. Do not eagerly
  retain, copy, or materialize expensive resources without a concrete need.
* Unless zero has a natural safe meaning, reserve it as unspecified or invalid
  for enum-like types and keep sibling packages consistent.
* Keep option validation, trimming, and defaults with the option-owning type or
  constructor rather than repeating normalization across layers.

### Type declarations and file structure

These rules are mandatory unless the task explicitly requires otherwise.

* Prefer grouped `type ( ... )` declarations for package-level types.
* Types declared in the same file should normally be placed in a single grouped
  `type` declaration rather than written as independent `type` declarations.
* This applies equally to structs, interfaces, aliases, named primitive types,
  and method-bearing types.
* Do not split types into independent declarations merely because one or more of
  them have methods.
* Keep related types together when they belong to the same narrow responsibility
  and their proximity makes ownership, lifecycle, or state transitions easier
  to understand.
* A file may contain multiple related behavioral types when they form one
  cohesive concern.
* Split types into separate files based on responsibility and ownership, not
  simply because multiple types have methods.
* When a file contains only one package-level type, a standalone declaration is
  acceptable; do not create an artificial group containing a single type.
* When adding a package-level type to a file that already contains type
  declarations, incorporate it into the existing type group when it belongs to
  the same concern.
* Keep small state, lifecycle, protocol, or coordination types together when
  they collectively describe one implementation concern.
* Avoid scattering a cohesive family of small types across multiple files.
* Do not use `helpers.go`, `utils.go`, or similar files as dumping grounds.
  Organize growing concerns by predictable responsibilities.

Preferred:

```go
type (
	sessionRegistry struct {
		mu sync.RWMutex

		entries map[SessionID]*sessionEntry
		groups  map[workspace.ID]*workspaceGroup
		closed  bool
	}

	sessionEntry struct {
		session *Session
		state   registryState
	}

	sessionCreation struct {
		workspace workspace.ID
		group     *workspaceGroup
	}

	workspaceClose struct {
		id    workspace.ID
		group *workspaceGroup
		owner bool
	}
)
```

Avoid independent declarations when the types form the same cohesive concern:

```go
type sessionRegistry struct {
	// ...
}

type sessionEntry struct {
	// ...
}

type sessionCreation struct {
	// ...
}

type workspaceClose struct {
	// ...
}
```

### Functions, methods, packages, and abstractions

* Prefer a method for behavior intrinsic to a semantic type, its state,
  invariants, lifecycle, synchronization, or resources it owns.
* Prefer a package-level function only for construction, package-wide
  conversion, or behavior with no natural receiver.
* Organize files around cohesive responsibilities rather than individual types.
  A file may contain multiple related types and their methods when they
  participate in the same narrow concern.
* Keep methods close to the types they belong to.
* A file containing methods must not also contain regular package-level
  functions unless those functions are constructors for types owned by that
  file.
* Constructors include conventional `New...` functions and other explicit
  construction functions whose primary responsibility is creating or
  initializing one of the file's types.
* Do not keep a regular helper function beside methods merely because those
  methods are its only callers.
* If behavior belongs to a type's state, invariants, lifecycle,
  synchronization, or owned resources, make it a method.
* If package-level behavior genuinely has no natural receiver, place it in a
  separate responsibility-focused file.
* Split files when responsibilities diverge, not merely because several types
  have methods.
* Keep protocol conversions with their adapter concern and Ferret-to-neutral
  conversions in the language, diagnostic, or source layer.
* Keep package boundaries domain-oriented. Do not create packages merely to
  shorten files or remove a few repeated lines.
* Prefer concrete and unexported types until a real substitution boundary,
  multiple meaningful implementations, focused consumer contract, or valuable
  test seam justifies an interface.
* Extract shared behavior only when it is the same concept with the same
  semantics, ownership, and lifecycle.
* Do not introduce interfaces, wrappers, managers, factories, generic types, or
  layers for aesthetic symmetry, easier mocking alone, a few repeated lines, or
  hypothetical future requirements.
* Avoid both oversized responsibilities and fragmentation across excessive
  helpers, files, interfaces, or packages. Ownership should remain predictable
  and the primary execution path easy to follow.
* Do not split cohesive behavior across files merely to enforce one type or one
  method-bearing type per file.

Preferred:

```go
type (
	sessionRegistry struct {
		entries map[SessionID]*sessionEntry
	}

	sessionEntry struct {
		session *Session
		state   registryState
	}
)

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		entries: make(map[SessionID]*sessionEntry),
	}
}

func (r *sessionRegistry) add(session *Session) {
	// ...
}

func (r *sessionRegistry) close() error {
	// ...
}
```

Avoid:

```go
func (r *sessionRegistry) add(session *Session) {
	// ...
}

func resolveWorkspaceGroup(id workspace.ID) *workspaceGroup {
	// ...
}

func (r *sessionRegistry) close() error {
	// ...
}
```

If `resolveWorkspaceGroup` belongs to registry state or lifecycle, make it a
method. If it is genuinely package-level behavior, move it to an appropriately
named responsibility-focused file.

### Comments

* Do not comment every function or method by default.
* Exported declarations should have useful doc comments, including in `internal`
  packages, when they define package-facing contracts.
* Comment unexported code only for non-obvious behavior, invariants, side effects,
  ownership, synchronization, lifecycle, cleanup, protocol, or compatibility.
* Explain why, what must remain true, what is guaranteed, or why ordering matters.
  Do not restate names or signatures.
* Keep speculative future plans out of code comments. Update or remove comments
  when the implementation makes them obsolete.

### Control-flow spacing

These rules apply to handwritten Go code.

* A declaration, call, lookup, parse, or assertion stays adjacent to the `if`
  that immediately checks or consumes its result.
* Separate that producer-and-check pair from preceding independent work.
* Separate independent consecutive `if` statements with a blank line.
* Add a blank line after completed control flow before independent work.
* When another statement precedes `return` or `break` in the same block, start
  the control transfer as a new logical group. No blank line is required when it
  is already the first statement in the block.
* Do not add artificial leading blank lines or surround every return mechanically.

```go
path, err := uri.Path()
if err != nil {
	return fmt.Errorf("resolve document URI: %w", err)
}

if len(changes) == 0 {
	return ErrNoTextChanges
}

result := buildResult()

return result
```

### Local types and naming

Local types are appropriate when small, passive, method-free, used by one
function, and helpful to its algorithm. Promote a type when it represents a
domain, lifecycle, protocol, or reusable concept; spans a substantial function;
clarifies ownership; or may reasonably gain methods.

Follow Go initialism and package naming conventions. Keep package names short,
lowercase, and responsibility-oriented; protocol-neutral names independent of a
transport; receiver names short and consistent; and exported names free of
package stutter. Treat new protobuf and CLI names as long-lived contracts.

## Tests and performance

* Add or update tests for every behavior change and place them with the package
  that owns the behavior.
* Test observable contracts rather than mirroring implementation details. Use
  focused table tests when several inputs exercise one contract.
* Use `t.Helper()` in reusable helpers and `t.Cleanup` for restoration,
  cancellation, goroutine stopping, and resource closure.
* Cover relevant positive, negative, boundary, invalid-input, cancellation,
  cleanup, repeated-operation, idempotency, stale-state, error-identity,
  concurrency, and cross-layer integration cases.
* Keep timeouts bounded and CI-tolerant. Avoid network-dependent tests unless a
  task requires integration and the repository provides deterministic fixtures.
* Bug fixes should include a regression test that fails without the fix whenever
  practical. Protobuf or gRPC work also requires reproducible generation and
  wire or translation coverage.
* Passing tests are evidence, not proof that the design is appropriate.

A change is performance-significant when it may materially affect latency,
allocations, copying, compilation, mapping, caching, memory retention, lock
contention, startup or shutdown, resource lifetime, or execution/debug
throughput. For such changes, identify a focused benchmark, retain a comparable
pre-change baseline, rerun it afterward, compare `ns/op`, `B/op`, and
`allocs/op`, investigate meaningful regressions, and report the commands and
results accurately. Documentation-only, test-only, pure rename, formatting-only,
and narrow non-hot-path refactors normally do not require benchmarks.

## Required workflow for non-trivial changes

1. Identify the owning subsystem and read its development guide, source, and
   tests.
2. Identify observable contracts, invariants, lifecycle, resource ownership,
   error semantics, compatibility, and explicit non-goals.
3. Choose the smallest coherent design that fits current ownership boundaries;
   do not rely on historical prose or existing technical debt as precedent.
4. Determine concurrency, lifecycle, compatibility, and performance risk; retain
   a focused pre-change benchmark when performance matters.
5. Add or update correctness tests and implement the focused change without
   collateral cleanup.
6. Run the narrowest validation that exercises the change, then broaden by risk
   with integration, race, lint, build, generation, or repository-wide checks.
7. Evaluate documentation impact and update affected repository and public
   documentation.
8. Perform the mandatory final self-review below and inspect the complete diff.
9. Fix issues introduced by the task and appropriate small adjacent findings,
   then repeat every invalidated validation and benchmark.
10. Report the implementation, preserved invariants, documentation impact,
    tests, measurements, review, limitations, and unresolved follow-up
    accurately.

Do not perform opportunistic refactors, dependency upgrades, formatting churn,
API redesign, package reshuffling, generated-file changes, or implementation of
future features unrelated to the task. Do not perform unrelated documentation
rewrites; documentation updates required to keep affected contracts, behavior,
examples, and guidance accurate are part of the task.

## Validation expectations

Use the `Makefile` and current CI workflows as command sources. Start narrow and
broaden according to risk.

* Handwritten Go changes require formatting of affected code and focused package
  tests. Inspect formatter output for unrelated churn.
* Cross-package changes normally require affected tests followed by
  `go test ./...` when practical.
* Shared-state or goroutine changes require `-race` on every affected domain and
  adapter package; do not assume the current CI package list is exhaustive.
* CLI or daemon lifecycle changes require relevant package tests and compilation.
* Lint-sensitive or exported-contract changes require repository lint when the
  tools are available.
* Protobuf changes require source lint, pinned regeneration, generated-diff
  inspection, and the checked-in generation gate.
* Broad or release-facing Go changes should finish with the broad build gate when
  the environment supports it.
* Documentation-only changes require exact-scope, link/path, Markdown structure,
  whitespace, and complete-diff validation; do not run unrelated suites for
  validation theater.

After review-driven changes, rerun every command whose result may be invalid.
Report tooling, environment, permission, or dependency limitations explicitly.
Never claim tests, lint, builds, race checks, benchmarks, generation, or review
succeeded unless they actually completed.

## Mandatory final self-review

Every coding task ends with a design and style review after implementation and
initial validation. Review changed and directly adjacent code as though reviewing
another engineer's pull request; for non-trivial work, inspect the complete diff
as one coherent change.

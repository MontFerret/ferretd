# Execution Development

`internal/exec` coordinates compiled Universal API Plans and one common per-run
execution state used by ordinary execution and debugging. It borrows the
composition-owned `runtimes.Owner`. Each compiled Session acquires and owns one
binding; native bindings use Ferret's upstream `uapi` adapter, and Wire bindings
use the configured hosted runtime. The execution manager owns long-lived
identity, option preparation, cancellation, lifecycle, observation, and cleanup.

The resource hierarchy is:

```text
Workspace
    -> execution Session
        -> zero or more one-shot Executions -> common execution runtime
        -> zero or more retained DebugSessions through internal/debug
            -> DebugRuntime -> common execution runtime + debugger capability
```

See [workspace.md](workspace.md) for source refresh and root identity and
[debugging.md](debugging.md) for the debug child path.

## Compiled Sessions

Creating a Session explicitly selects one workspace-relative lowercase `.fql`
regular file. Workspace refresh admits the selected file beneath discovery-excluded
directories (including `.tmp`, `testdata`, and nested Go modules) while preserving
containment and nested-symlink restrictions. It does not admit excluded neighbors.
After loading source, the execution manager acquires the exact selected runtime
(or catalog default) and compiles there using `api.NewSource` with the absolute
source path and retained content. No native compiler preflight runs for Wire.
The name identifies source; it does not synchronize remote files or remap paths.
Load, syntax, or compiler failures are returned as structured
compilation diagnostics without publishing a Session.

A published Session owns one runtime binding, its resolved immutable `RuntimeName`,
one normal `api.Plan`, workspace/source identity, source revision, and declared
parameter names. Parameter metadata is retrieved through fallible `Plan.Params(ctx)` using the creation context before
publication, outside registry locks. Metadata failure closes the unpublished
Plan and retains both retrieval and cleanup errors. Later disk refreshes do not
mutate existing Sessions. The same Session can create multiple isolated
Executions.

The Session also coordinates lazy construction of one matching debug Plan. That
Plan is separate because it carries debugger instrumentation. It compiles on the
same bound runtime from the original source, without reacquisition or refresh.
Debug consumers
receive an `exec.DebugRuntime`, never mutable Session internals or a raw Plan.
The DebugRuntime owns its Plan lease through the common execution state's
one-time Universal debugger Session close attempt.

The concrete Session and Execution implementations are package-private.
Sibling packages use `exec.Manager`, immutable snapshots, subscriptions, and
`DebugRuntime` rather than mutable resource objects.

## Common runtime and Executions

`exec.Parameters` is the named caller-facing parameter map. It recursively
clones `map[string]any` and `[]any` containers before retained state is created.
Raw maps are converted to `Parameters` only at gRPC and DAP boundaries.

The package has one concrete per-run implementation. It owns the immutable
Session/Plan/source target, cloned parameters, normalized options, a
manager-owned context and cancellation function, one `api.Session` or Universal
debugger Session, ordinary output capture, `RuntimeFailure`
materialization, and idempotent output/session cleanup. Both normal and debug
creation use the same preparation and cloning semantics.

Ordinary `Session.Run` returns a caller-owned live `api.Output` handle, not a
terminal result. The per-run owner calls `Collect` with its manager-owned context,
retains the detached `*api.Content` directly, then explicitly closes the output
and the caller-owned Universal session in that order. Collection settles
output-owned execution, delivery, and cleanup; explicit closure also handles
collection rejected before admission and retrieves retained cleanup errors.
Terminal state and its event are published only after collection and owned
cleanup settle. Closing unread output alone does not establish successful
execution, so even side-effect-only runs consume their handles.

A nil collected content pointer means absent output. Non-nil content remains
present even with nil or empty `Data`, including when collection or cleanup also
fails. Content metadata describes the complete payload, so its known length may
exceed a prefix returned with an error; unknown length remains unchanged.
Collected content stays valid after cleanup. Live handles never enter snapshots
or watcher events. Debug events already carry detached content, retained by the
debug Session. Both snapshot APIs copy metadata values and mutable bytes for
each caller and watcher.

Runtime options retain an optional working directory prepared through the parent
Session's provider policy. Native Sessions default to the workspace root and
validate explicit absolute paths through canonicalization and rooted-directory
opening; an override may be outside the workspace. Wire Sessions send no implicit
root and inherit the hosted runtime's default. Nonblank Wire overrides retain
exact bytes and bypass daemon-side filesystem checks and normalization; the host
validates them during runtime session creation or use. Normal and debug creation
use the same policy, while low-level `api.WithFSRoot` application is provider-neutral.

An empty internal working directory means no explicit override. The gRPC and DAP
boundaries reject present blank fields; DAP also rejects JSON null and non-string
values. Source containment and identity always belong to the daemon workspace.

Parameters and output content type are applied with `api.WithParams` and
`api.WithOutputContentType`. Runtime-specific parameter rejection therefore
occurs when `api.Plan.NewSession` applies these options. Ordinary execution
retains it as the existing asynchronous session-creation failure category;
JSON/protobuf validation at public transport boundaries is unchanged.

Normal compilation omits optimization options and uses the wrapped engine's
configuration. Native per-plan options accept explicit `OptimizationNone`,
`OptimizationBasic`, or `OptimizationFull` without changing shared compiler
configuration; `OptimizationAggressive` is unsupported. Debug compilation accepts
omission or `OptimizationNone` only. Non-nil Universal option callbacks run once
in order and their errors are joined. The upstream adapter queues native options;
native validation occurs when settings are applied or used. In particular, output
codec availability may fail during encoding after query execution.

Each Execution owns that runtime plus its ordinary one-shot state, ordered
lifecycle events, and terminal result or failure. The fresh Universal runtime
Session is still created only after `Start`, so session-creation failures remain
asynchronous terminal failures. Executions do not share runtime state, queue
work, persist results, replay, or act as a REPL.

Running transitions a created Execution to running and returns that snapshot
immediately. VM execution continues under manager-owned lifecycle control rather
than the triggering RPC context. Cancellation requests stop active work;
terminal state remains observable until explicit close or parent cleanup.

Successful output is retained with its content type and encoded bytes. Failures
distinguish runtime, runtime-session creation, and cleanup categories and retain
protocol-neutral source diagnostics when Ferret supplies them. `Run` and `Collect`
errors use the runtime category, including cleanup causes already reported by
collection. Explicit cleanup failure after otherwise successful collection uses
the cleanup category. Joined causes survive until failure materialization;
cleanup does not discard the primary error or available content.

## Watches and ordering

An execution watch first receives the event representing current state, then
future ordered events. Events carry a monotonically increasing sequence. A
terminal event is followed by end-of-stream.

Each watcher has a bounded buffer. A lagging watcher is disconnected with a
classifiable lag error without blocking execution or other watchers. Canceling
the watch context removes that subscriber but does not cancel the underlying
Execution.

State changes occur before their corresponding events become visible. Terminal
results, close behavior, and watcher completion must agree on one authoritative
Execution lifecycle.

## Cleanup and parent ownership

Closing an Execution cancels active work, waits for output settlement and
common-runtime cleanup, ends watchers, and becomes idempotent. Closing a
Session stops both normal and debug runtime creation, cancels pending preparation,
closes every child Execution and DebugSession, waits for DebugRuntime leases,
closes its Plans, and finally releases its binding. Closing a workspace invokes the execution manager's registered close
hook before retained workspace state is cleared.

The manager orchestrates two package-local registries rather than owning all
resource maps under one lock. The Session registry owns active-versus-closing
reachability, service shutdown admission, and workspace groups. Each workspace
group owns its Session membership and the creation gate that orders compilation
against workspace close. The Execution registry owns active-versus-closing
reachability and the Session-to-Execution index. Closing entries remain retained
for concurrent waiters and parent teardown, but normal lookups expose active
entries only.

Each Session owns the gate that admits ordinary and debug runtime creation.
Session close stops that gate, cancels pending constructors outside locks, and
waits for every admitted creator to publish or roll back, then marks the ordinary child set closing and invokes debug child cleanup.
Before leaving admission, each constructor checks parent closure and transfers
its rollback cleanup failures to that closing parent's retained result under the
same lock. Teardown collects those failures after waiting for constructors.
Preparation and cancellation errors remain creation failures; they do not make
parent cleanup fail. Rollback settled while admission remains open stays with
the creation caller. `PreparationCleanupError` marks those cleanup causes while
preserving their identity and text, allowing composition to retain them in its
final close result separately from retryable preparation failures.

A completed Execution remains in its Session group while that parent is closing,
including while the parent waits for admitted creators. This preserves child
cleanup errors until the parent has collected them.
No Session-registry lock is held while entering the Execution registry, and no
registry lock is held during compilation, hooks, runtime cleanup, Plan closure,
or lifecycle waits.

Output and session cleanup failures are retained by the per-run owner so parent
cleanup and concurrent close waiters can observe both. A cleanup failure does not
skip remaining resource closure. Concurrent close calls share one close
operation and retained result. A caller's canceled wait does not transfer or
abandon ownership of the underlying cleanup; a later caller can still observe
completion.

The manager itself has a terminal closed state. New resources are rejected after
close, and daemon shutdown closes execution resources before clearing
workspaces. The manager borrows its acquisition owner and never closes it. Workspace and
manager close cancel pending Session preparation before waiting. Temporary
construction contexts link caller cancellation with parent closure, preserving
both context identities and domain causes; publication checks prevent adopting
resources after closure. Failed construction also checks parent state when
leaving admission, preserving its closed classification independently of which
cancellation cause arrived first. Successfully published resources keep their
success even if their parent closes before construction returns.
Cleanup settles cooperatively and retains failures,
even when a close waiter cancels. Composition closes the owner afterward.
Binding disconnection does not trigger reacquisition or replay; a new compiled
Session obtains a new binding.

## gRPC and public client projection

The execution v1 protobuf defines Session and Execution identity, source
snapshots, JSON-shaped parameters, output, structured failures, lifecycle RPCs,
the optional runtime selector and resolved Session name, the runtime working
directory, and the server-streaming watch.
The adapter maps `Content.Metadata.ContentType` and `Content.Data` to the
existing output content-type and data fields; upstream Content JSON is not the
public result representation. `internal/grpc` preserves optional-field presence
and maps those values and stable domain errors without owning execution state.
A present blank working directory is classified separately from invalid
parameters.

The public `client` package presents supported Session, Execution, watcher,
output, failure, and working-directory option types while hiding generated
protobuf clients. An empty Go working-directory value omits the wire field. Its
sentinel and typed error mapping is part of the public compatibility surface.
API 1.3 introduces selection. The client remembers initial negotiation, rejects
explicit names on older servers before RPC, and validates new names and exact
acknowledgment outside the shared converter. Rejected creation responses with an
ID receive one detached, five-second cleanup attempt; failures are joined.
Older unselected responses may leave the name empty, meaning unreported.
Runtime resource details are classified before generic source/daemon mappings.
Nondiagnostic compilation and metadata errors retain readable RPC messages.

## Testing changes

Execution tests should cover compile failure without publication, immutable
Sessions, refreshed source revisions, shared normal/debug parameter semantics,
one-shot run semantics, isolated runtime Sessions, cancellation, terminal
results, failure categories, current-plus-future watching, lagged watchers,
context cancellation, concurrent close, parent cascades, Plan close counts,
DebugRuntime leases and setup rollback, and manager shutdown.

Cross-boundary changes require domain tests, gRPC translation tests, public
client tests, and daemon end-to-end coverage. Lifecycle tests should coordinate
with observable hooks or channels rather than sleeps. Benchmarks cover unchanged
Session creation, execution creation and watching, and parallel observation;
changes should be assessed for compilation work, allocations, copies, lock
contention, goroutine lifetime, and retained resources.

Orchestration tests use API fakes with explicit results and hooks. Native
compilation, filesystem, and execution integration tests and their unchanged
benchmarks live under the test-only `internal/integration` package. Dependency
integration tests cover the upstream adapter's source coordinates, debugger
operations, diagnostics, and live breakpoint replacement. Debug lifecycle
benchmarks remain with the debug manager.

### Recoverable Session acquisition (API 1.4)

A client can send a fresh, canonical UUID in `CreateSession.acquisition_id`.
The Session echoes it. The pair of registered workspace ID and acquisition ID
identifies the operation before the client knows its Session ID. A duplicate
live create fails with ALREADY_EXISTS. Omission preserves legacy behavior.

Use `CancelSessionAcquisition` on the original daemon after a lost reply.
Cancellation fences a later create, joins rollback of a pending compilation, or
closes the published Session and its children. Success confirms settlement.
Concurrent cancellations share the cleanup result. A client deadline stops its
wait while committed cleanup continues. Cancelled keys and cleanup failures
are retained until workspace teardown. An ordinary successful CloseSession
releases the acquisition record. The Go client exposes the key on
CreateSessionRequest and Session, and checks negotiation before dispatch.

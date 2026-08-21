# Debugging Development

Debugging is split between the shared execution runtime, retained
protocol-neutral DebugSessions, and a single-session DAP adapter:

```text
internal/exec
    -> common execution runtime + lazy debug Plan + leased DebugRuntime
internal/debug
    -> retained debugger-specific Session state and lifecycle
internal/dap
    -> DAP requests, events, coordinates, and handles over stdio
```

The supported launch arguments and wire behavior are documented in
[the DAP guide](../dap.md).

## Debug runtimes and Plan ownership

An execution Session eagerly owns its normal Plan and lazily compiles one
matching debug Plan. Lazy construction is coordinated so concurrent debug
requests converge on one result. A failed debug compilation is retained
consistently with the Session's source snapshot.

`internal/exec` creates a DebugRuntime through the same lower-level machinery as
ordinary execution. The common runtime owns prepared parameters, normalized
`exec.RuntimeOptions`, immutable source/Plan data, manager-owned cancellation,
the concrete Ferret session, shared `RuntimeOutput` and `RuntimeFailure`
materialization, and idempotent cleanup. The DebugRuntime exposes the Ferret
debugger capability and owns the debug-Plan lease. It does not expose mutable
Session maps, locks, or child state.

## Retained DebugSessions

`internal/debug` requires an execution manager and registers its Session-child
cleanup hook during construction. Each DebugSession owns one `exec.DebugRuntime`
plus breakpoints, authoritative debugger lifecycle state, asynchronous command
coordination, event watchers, paused-state inspection, and terminal debugger
data. It does not prepare runtime values, construct Ferret sessions, translate
runtime failures, or release Plan leases independently.

The concrete debug Session implementation is package-private; adapters consume
`debug.Manager` operations and immutable `SessionSnapshot` values.

Resolved breakpoint identities and reported hits use `debug.BreakpointID`; DAP
owns the projection from those identities to stable protocol breakpoint IDs.

Supported commands include start, continue, pause, step-in, step-over, step-out,
and terminate. Inspection includes threads, stack frames, scopes, variables, and
frame-scoped evaluation. Ferret decides executable locations, debugger stops,
stack semantics, values, and runtime behavior; this repository coordinates
identity and lifecycle around those capabilities.

Debug watches publish current and future ordered events through bounded buffers.
Lagging subscribers disconnect without blocking the session. Commands that run
execution invalidate paused-state inspection before externally visible running
events. Terminal states eventually close the common runtime and end every watch.

Closing a DebugSession is idempotent and guarantees eventual common-runtime
cleanup even when an individual caller stops waiting. Closing the parent
execution Session closes all DebugSessions and their DebugRuntimes before
releasing normal or debug Plans.

## DAP composition

The `dap` command runs one protocol-pure server over stdin and stdout. It does
not connect to `ferretd serve`. The adapter constructs in-process workspace,
execution, and debug managers and owns one launched workspace, execution
Session, and DebugSession.

Launch follows the DAP initialization and configuration sequence. The launch
request remains pending while breakpoints are configured. After
`configurationDone`, the adapter acknowledges configuration, starts the debug
Session, responds to launch, and then emits stopped or terminal events in the
required order.

`internal/dap` owns client path format, line and column base conversion, message
sequence numbers, and all integer frame, scope, and variable handles. The
protocol-neutral debug model keeps Ferret source locations and value references.
Handles are cleared whenever execution runs, steps, completes, fails, or
terminates, so stale paused-state references cannot be reused.

Serialized writes preserve DAP message framing and sequence order. Stdout
contains DAP messages only; process-facing errors are returned to the command
and reported on stderr after the adapter exits.

## Runtime results and cleanup

Runtime errors stop as inspectable DAP exceptions. Continuing from that stop
reports error output and terminal events. Successful completion reports encoded
result output and successful exit. Explicit termination ends the session without
inventing a normal process exit.

Adapter cleanup cancels the active watch, terminates and closes the DebugSession,
closes its execution Session, closes the workspace, and then closes each manager.
The common execution runtime owns the concrete Ferret session and performs
cancellation and its one-time close attempt. The DebugRuntime releases its
debug-Plan lease afterward even when cleanup reports failure. Repeated
disconnect, termination, context cancellation, or transport failure cannot
release resources twice.

## Deliberate exclusions

DAP is stdio-only and single-session. It does not attach to `ferretd serve`,
restart sessions, mutate variables, execute backwards, or implement advanced
breakpoint forms that Ferret and the adapter do not currently support.

`proto/ferretd/debug/v1` remains an ungenerated placeholder. There is no debug
gRPC registration, daemon RPC behavior, generated debug package, or supported
public debug client. DAP work must not create those surfaces as a side effect.

## Testing changes

Execution-runtime tests own lazy debug Plan behavior, lease lifetime, shared
parameter/options semantics, setup rollback, cancellation, output/failure
conversion, and Ferret-session cleanup. Debug-manager tests own commands, state
transitions, event ordering, watcher lag, paused-state inspection, terminal
retention, parent cleanup, and concurrent close. DAP tests own initialization
defaults, launch sequencing, coordinate conversion, breakpoints,
request/response/event ordering, handle invalidation, output and termination
behavior, framing, and cleanup.

Concurrency tests should use deterministic debugger hooks or events rather than
sleeps. Performance-sensitive changes should examine debug Session creation,
command dispatch, event fan-out, variable materialization, handle retention,
goroutine lifetime, and contention without merging execution and debug ownership
into a generic model.

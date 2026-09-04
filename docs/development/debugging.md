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
ordinary execution. The common execution state owns cloned parameters,
normalized `exec.RuntimeOptions`, immutable source/Plan data, manager-owned
cancellation, one Universal debugger Session, copied `api.Output` and
`RuntimeFailure` materialization, and idempotent cleanup. The DebugRuntime
exposes the Universal debugger capability and owns the debug-Plan lease. It does
not expose mutable Session maps, locks, or child state.

## Retained DebugSessions

`internal/debug` requires an execution manager and registers its Session-child
cleanup hook during construction. Each DebugSession owns one `exec.DebugRuntime`
plus breakpoints, authoritative debugger lifecycle state, asynchronous command
coordination, event watchers, paused-state inspection, and terminal debugger
data. It does not prepare runtime values, construct Universal sessions,
translate runtime failures, or release Plan leases independently.

The concrete debug Session implementation is package-private; adapters consume
`debug.Manager` operations and immutable `SessionSnapshot` values.

Resolved breakpoint identities and reported hits use `debug.BreakpointID`; DAP
owns the projection from those identities to stable protocol breakpoint IDs.

Supported commands include start, continue, pause, step-in, step-over, step-out,
and terminate. Inspection includes threads, stack frames, scopes, variables, and
frame-scoped evaluation. Ferret decides executable locations, debugger stops,
stack semantics, values, and runtime behavior. The provisional
`internal/ferretapi` adapter translates Universal debugger events, locations,
breakpoints, variables, and outputs into the existing daemon debug model;
replacing that model is outside the current boundary.

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
not connect to `ferretd serve`. The adapter constructs and owns one Universal
runtime plus in-process workspace, execution, and debug managers, and owns one
launched workspace, execution Session, and DebugSession.

Launch follows the DAP initialization and configuration sequence. The launch
request remains pending while breakpoints are configured. After
`configurationDone`, the adapter acknowledges configuration, starts the debug
Session, responds to launch, and then emits stopped or terminal events in the
required order.

`internal/dap` owns client path format, line and column base conversion, message
sequence numbers, and all integer frame, scope, and variable handles. The
protocol-neutral debug model keeps Ferret source locations and value references.
Current handle payloads are invalidated whenever execution runs, steps,
completes, fails, terminates, or the adapter cleans up. The allocator remains
monotonic for the adapter session and never recycles an integer, so a stale
paused-state reference cannot alias a later frame or value. Typed tombstones for
the most recently invalidated non-empty handle set let the adapter distinguish
recognized late IDE inspection from malformed, wrong-kind, or random handles.
Late scopes and variables receive empty successful responses; passive hover and
watch evaluation does the same, while active or unfamiliar evaluation contexts
remain errors.

The adapter retains one canonical filesystem identity for the launched source
alongside its user-facing path. Breakpoint paths are resolved against the launch
root and compared by canonical path or operating-system file identity, while
debugger calls continue using the launched spelling. VS Code configures stored
breakpoints from other workspace files during startup; the adapter reports those
and unavailable local sources as unverified without transferring ownership or
mutating debugger state.

Serialized writes preserve DAP message framing and sequence order. Stdout
contains DAP messages only. The command injects the same stderr-backed JSON
logger and `--log-level` contract used by `serve`; ordinary lifecycle and
failure diagnostics use the default `info` verbosity, while `debug` adds
semantic DAP request, response, and event records. Each diagnostic is one JSON
object followed by a newline.

Request handlers select safe logging fields explicitly, and the common response
path records failures before writing their unchanged DAP error responses. After
launch, the logger carries the existing workspace, execution Session, and
DebugSession IDs. Trace records include compact identifiers, paths, counts, and
handle values but never serialize arbitrary protocol objects, query contents,
parameters, expressions, evaluated values, or output contents. DAP `output`
events remain protocol messages on stdout even when their category is `stderr`;
process diagnostics always go directly to stderr. Fatal adapter errors are
returned to the command for process-facing reporting rather than logged and
returned at both layers.

## Runtime results and cleanup

Runtime errors stop as inspectable DAP exceptions. Continuing from that stop
reports error output and terminal events. Successful completion reports encoded
result output and successful exit. Explicit termination ends the session without
inventing a normal process exit.

Adapter cleanup cancels the active watch, terminates and closes the DebugSession,
closes its execution Session, closes the workspace, closes each manager, and
then closes the composition runtime exactly once. The common execution state
owns the Universal debugger Session and performs cancellation and its one-time
close attempt. The DebugRuntime releases its debug-Plan lease afterward even
when cleanup reports failure. Repeated disconnect, termination, context
cancellation, or transport failure cannot release resources twice.

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
conversion, and Universal-session cleanup. Adapter tests own native translation
and diagnostic identity. Debug-manager tests own commands, state
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

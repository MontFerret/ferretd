# Execution Development

`internal/exec` coordinates compiled Ferret Plans and isolated one-shot runtime
Sessions. Ferret owns compilation, runtime values, VM execution, and result
encoding; the execution manager owns long-lived identity, lifecycle,
cancellation, observation, and cleanup.

The resource hierarchy is:

```text
Workspace
    -> execution Session
        -> zero or more one-shot Executions
        -> zero or more retained DebugSessions through internal/debug
```

See [workspace.md](workspace.md) for source refresh and engine ownership and
[debugging.md](debugging.md) for the debug child path.

## Compiled Sessions

Creating a Session selects one already-discovered workspace-relative `.fql`
document. The workspace refreshes its latest saved contents, then compiles it
with the workspace's rooted Ferret engine. Load, syntax, or compiler failures
are returned as structured compilation diagnostics without publishing a
Session.

A published Session owns one immutable normal Plan, its workspace and source
identity, source revision, and declared parameter names. Later disk refreshes do
not mutate existing Sessions. The same Session can create multiple isolated
Executions.

The Session also coordinates lazy construction of one matching debug Plan. That
Plan is separate because it carries debugger instrumentation. Debug consumers
receive only leased immutable targets, never mutable Session internals.

## Parameters and Executions

`internal/params` validates and copies caller-owned JSON-shaped parameter values
before they enter retained state. Maps, lists, scalars, and null values cross the
gRPC and public-client boundary without exposing mutable caller storage.

Each Execution owns copied parameters and options, one fresh Ferret runtime
Session, at most one run attempt, its cancellation function, ordered lifecycle
events, and a terminal result or failure. Executions do not share runtime state,
queue work, persist results, replay, or act as a REPL.

Running transitions a created Execution to running and returns that snapshot
immediately. VM execution continues under manager-owned lifecycle control rather
than the triggering RPC context. Cancellation requests stop active work;
terminal state remains observable until explicit close or parent cleanup.

Successful output is retained with its content type and encoded bytes. Failures
distinguish runtime, runtime-session creation, and cleanup categories and retain
protocol-neutral source diagnostics when Ferret supplies them.

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

Closing an Execution cancels active work, waits for owned runtime cleanup, ends
watchers, and becomes idempotent. Closing a Session closes every child Execution
and DebugSession before closing its Plans. Closing a workspace invokes the
execution manager's registered close hook before the workspace engine closes.

The manager orchestrates two package-local registries rather than owning all
resource maps under one lock. The Session registry owns active-versus-closing
reachability, service shutdown admission, and workspace groups. Each workspace
group owns its Session membership and the creation gate that orders compilation
against workspace close. The Execution registry owns active-versus-closing
reachability and the Session-to-Execution index. Closing entries remain retained
for concurrent waiters and parent teardown, but normal lookups expose active
entries only.

Each Session owns the gate that admits Execution creation. Session close stops
that gate, waits for every admitted creator to publish or leave, then detaches
the complete child set from the Execution registry. No Session-registry lock is
held while entering the Execution registry, and no registry lock is held during
compilation, hooks, runtime cleanup, Plan closure, or lifecycle waits.

Concurrent close calls share one close operation and retained result. A caller's
canceled wait does not transfer or abandon ownership of the underlying cleanup;
a later caller can still observe completion.

The manager itself has a terminal closed state. New resources are rejected after
close, and daemon shutdown closes execution resources before clearing
workspaces.

## gRPC and public client projection

The execution v1 protobuf defines Session and Execution identity, source
snapshots, JSON-shaped parameters, output, structured failures, lifecycle RPCs,
and the server-streaming watch. `internal/grpc` maps those values and stable
domain errors without owning execution state.

The public `client` package presents supported Session, Execution, watcher,
output, and failure types while hiding generated protobuf clients. Its sentinel
and typed error mapping is part of the public compatibility surface.

## Testing changes

Execution tests should cover compile failure without publication, immutable
Sessions, refreshed source revisions, parameter copying and rejection, one-shot
run semantics, isolated runtime Sessions, cancellation, terminal results,
failure categories, current-plus-future watching, lagged watchers, context
cancellation, concurrent close, parent cascades, Plan close counts, debug leases,
and manager shutdown.

Cross-boundary changes require domain tests, gRPC translation tests, public
client tests, and daemon end-to-end coverage. Lifecycle tests should coordinate
with observable hooks or channels rather than sleeps. Benchmarks cover unchanged
Session creation, execution creation and watching, and parallel observation;
changes should be assessed for compilation work, allocations, copies, lock
contention, goroutine lifetime, and retained resources.

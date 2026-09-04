# Execution Development

`internal/exec` coordinates compiled Ferret Plans and one common per-run
execution runtime used by ordinary execution and debugging. Ferret owns
compilation, runtime values, VM execution, and result encoding; the execution
manager owns long-lived identity, runtime preparation, cancellation, lifecycle,
observation, and cleanup.

The resource hierarchy is:

```text
Workspace
    -> execution Session
        -> zero or more one-shot Executions -> common execution runtime
        -> zero or more retained DebugSessions through internal/debug
            -> DebugRuntime -> common execution runtime + debugger capability
```

See [workspace.md](workspace.md) for source refresh and engine ownership and
[debugging.md](debugging.md) for the debug child path.

## Compiled Sessions

Creating a Session selects one eligible workspace-relative `.fql` document.
The workspace refreshes retained contents or defensively admits a missed
creation using the normal discovery boundaries, then compiles it with the
workspace's rooted Ferret engine. Load, syntax, or compiler failures are
returned as structured compilation diagnostics without publishing a Session.

A published Session owns one immutable normal Plan, its workspace and source
identity, source revision, and declared parameter names. Later disk refreshes do
not mutate existing Sessions. The same Session can create multiple isolated
Executions.

The Session also coordinates lazy construction of one matching debug Plan. That
Plan is separate because it carries debugger instrumentation. Debug consumers
receive an `exec.DebugRuntime`, never mutable Session internals or a raw Plan.
The DebugRuntime owns its Plan lease through the common runtime's one-time
Ferret-session close attempt.

The concrete Session and Execution implementations are package-private.
Sibling packages use `exec.Manager`, immutable snapshots, subscriptions, and
`DebugRuntime` rather than mutable resource objects.

## Common runtime and Executions

`exec.Parameters` is the named caller-facing parameter map. It recursively
clones `map[string]any` and `[]any` containers before retained state is created;
runtime preparation remains private to `internal/exec`. Raw maps are converted
to `Parameters` only at gRPC and DAP boundaries.

The package has one concrete execution-runtime implementation. It owns the
immutable Session/Plan/source target, prepared Ferret parameters plus the
daemon-retained copy, normalized options, a manager-owned context and
cancellation function, one Ferret session resource, `RuntimeOutput` and
`RuntimeFailure` materialization, and idempotent Ferret-session cleanup. Both
normal and debug creation use the same preparation and cloning semantics.

Runtime options also retain an optional canonical working directory. Validation
requires a nonblank absolute path that resolves to an accessible directory and
uses the same rooted-filesystem opening semantics as Ferret. This option is
independent of source/workspace containment and may name a directory outside the
workspace. An absent option preserves the workspace engine filesystem; a
present option is passed to `ferret.WithSessionFSRoot` for the fresh runtime
session without changing compilation or creating another Plan.

Each Execution owns that runtime plus its ordinary one-shot state, ordered
lifecycle events, and terminal result or failure. The fresh Ferret runtime
Session is still created only after `Start`, so session-creation failures remain
asynchronous terminal failures. Executions do not share runtime state, queue
work, persist results, replay, or act as a REPL.

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

Closing an Execution cancels active work, waits for common-runtime cleanup, ends
watchers, and becomes idempotent. Closing a Session stops both normal and debug
runtime creation, closes every child Execution and DebugSession, waits for
DebugRuntime leases, and then closes its Plans. Closing a workspace invokes the
execution manager's registered close hook before the workspace engine closes.

The manager orchestrates two package-local registries rather than owning all
resource maps under one lock. The Session registry owns active-versus-closing
reachability, service shutdown admission, and workspace groups. Each workspace
group owns its Session membership and the creation gate that orders compilation
against workspace close. The Execution registry owns active-versus-closing
reachability and the Session-to-Execution index. Closing entries remain retained
for concurrent waiters and parent teardown, but normal lookups expose active
entries only.

Each Session owns the gate that admits ordinary and debug runtime creation.
Session close stops that gate, waits for every admitted creator to publish or
leave, then marks the ordinary child set closing and invokes debug child cleanup.
No Session-registry lock is held while entering the Execution registry, and no
registry lock is held during compilation, hooks, runtime cleanup, Plan closure,
or lifecycle waits.

Concurrent close calls share one close operation and retained result. A caller's
canceled wait does not transfer or abandon ownership of the underlying cleanup;
a later caller can still observe completion.

The manager itself has a terminal closed state. New resources are rejected after
close, and daemon shutdown closes execution resources before clearing
workspaces.

## gRPC and public client projection

The execution v1 protobuf defines Session and Execution identity, source
snapshots, JSON-shaped parameters, output, structured failures, lifecycle RPCs,
the optional runtime working directory, and the server-streaming watch.
`internal/grpc` preserves optional-field presence and maps those values and
stable domain errors without owning execution state. A present blank working
directory is classified separately from invalid parameters.

The public `client` package presents supported Session, Execution, watcher,
output, failure, and working-directory option types while hiding generated
protobuf clients. An empty Go working-directory value omits the wire field. Its
sentinel and typed error mapping is part of the public compatibility surface.

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

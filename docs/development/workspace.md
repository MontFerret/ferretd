# Workspace Development

`internal/workspace` owns concurrency-safe, process-local workspace identity,
dynamic file discovery, retained source and syntax state, one rooted Ferret
engine per open workspace, targeted refresh, and parent-resource cleanup.

Workspace state is shared by the daemon, execution manager, and language
service. Editor overlays are not workspace state; see
[language.md](language.md).

## Identity and lifecycle

The public client resolves roots to absolute paths. The workspace service cleans
the supplied path without resolving symlinks and requires it to be an existing
absolute directory. Repeated opens of the same cleaned root converge on one
workspace ID, including concurrent opens, and state remains independent of
client connections.

The manager coordinates in-flight opens without holding its main lock across
filesystem I/O or Ferret parsing. A workspace is published only after coherent
loading and engine construction. Cancellation before publication must not leave
a manager entry or engine behind.

Published workspaces use one ID registry whose entries explicitly transition
from active to closing. Root lookup and normal ID lookup expose active entries
only. A closing entry composes `internal/lifecycle.CloseOperation`, remains
retained for concurrent `Close` or `Clear` waiters, and is removed after engine
cleanup publishes its result. The root-coalesced open operation remains
workspace-specific because it also owns a candidate Workspace, load result, and
manager generation.

Close is explicit and idempotent. Concurrent close callers observe one close
operation and retained result. The manager stops the workspace watcher, runs
registered child close hooks, and then closes the workspace engine. `Clear`
invalidates in-flight opens, removes published entries, and coordinates all
outstanding closes.

## Discovery

Opening a workspace synchronously walks its root and retains lowercase `.fql`
regular files. Discovery is deterministic and root-confined:

* nested symlinks are skipped;
* hidden and underscore-prefixed directories plus `node_modules`, `testdata`,
  and `vendor` are pruned;
* nested directories containing `go.mod` are module boundaries;
* non-regular files and non-`.fql` paths are ignored;
* relative paths use slash-separated, cleaned workspace paths;
* lists and document iteration are sorted deterministically.

The selected workspace root remains valid regardless of its own name or
`go.mod`. There is no ignore-file or Ferret project-manifest contract.

Each published workspace owns one filesystem watcher. It tracks the same
eligible directories used by initial discovery, reconciles file events or the
affected subtree, and performs a root reconciliation only after watcher
overflow or an unclassifiable error. Nested module-boundary directories remain
watched so removing their `go.mod` can admit the subtree. The watcher stops and
joins before workspace child and engine cleanup.

## Retained files and documents

The file model stores discovered identity and path information. The document
model additionally retains source contents, a typed `workspace.Revision`,
Ferret source and parse state, and load or syntax diagnostics. Callers receive
values or copies rather than mutable manager-owned collections.

A malformed or unreadable document does not fail an otherwise coherent
workspace. It remains represented with diagnostics so other files are usable.
Fatal root, discovery, or engine failures prevent publication of the entire
workspace.

Retained parser state is daemon-owned and treated as read-only by visitors. The
workspace also assigns an internal monotonic generation that does not reset
when a path is deleted and recreated. The language analysis cache uses this
identity instead of the client-visible per-file revision. The workspace owns
syntax state, not semantic compilation Plans or runtime state.

## Refresh and compilation boundary

Session creation asks the workspace to reconcile only the selected document.
The operation rereads a retained path, defensively admits a missed eligible
creation, and atomically publishes changed source, syntax state, diagnostics,
availability, and revision. Unchanged contents retain their revision. Missing
or newly ineligible paths are removed; an eligible but unreadable regular file
remains represented with load diagnostics.

After refresh, `CompileDocument` compiles the selected immutable document for a
normal Session. `CompileDebugSnapshot` compiles the exact source snapshot and
text retained by that Session. Existing Sessions keep the source text, revision,
and Plan with which they were created; a later refresh does not mutate them.

Each workspace engine uses Ferret's read-write filesystem rooted at the cleaned
workspace directory. Root confinement and filesystem semantics remain Ferret
embedding responsibilities; the workspace manager coordinates ownership and
lifetime.

## Consumers

* `internal/exec` creates immutable Sessions from refreshed documents and
  registers the child-resource close hook.
* `internal/language` looks up retained documents by absolute path as
  saved-source baselines for unopened editor documents.
* `internal/grpc` exposes workspace identity and lifecycle only; parser and
  document internals are not wire contracts.
* `client` converts public absolute roots and workspace IDs to that gRPC API.

## Testing changes

Tests should cover canonical roots, root confinement, deterministic ordering,
directory pruning and dynamic boundaries, symlink handling, recoverable
document failures, fatal open and watcher rollback, concurrent duplicate opens,
cancellation, refresh revisions, watcher event reconciliation, close ordering,
concurrent close, idempotency, and engine cleanup. Platform path behavior needs
Windows-specific coverage when root or separator rules change.

Workspace lifecycle and discovery benchmarks live with the manager. Changes to
walking, parsing, copying, lookup, refresh, synchronization, or engine lifetime
should be assessed for allocation, latency, contention, and retained-resource
impact.

# Workspace Development

`internal/workspace` owns concurrency-safe, process-local workspace identity,
static file discovery, retained source and syntax state, one rooted Ferret
engine per open workspace, refresh of existing targets, and parent-resource
cleanup.

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
operation and retained result. The manager runs registered child close hooks
before closing the workspace engine. `Clear` invalidates in-flight opens,
removes published entries, and coordinates all outstanding closes.

## Discovery

Opening a workspace synchronously walks its root and retains lowercase `.fql`
regular files. Discovery is deterministic and root-confined:

* nested symlinks are skipped;
* `.git`, `.hg`, `.svn`, `node_modules`, and `vendor` directories are pruned;
* non-regular files and non-`.fql` paths are ignored;
* relative paths use slash-separated, cleaned workspace paths;
* lists and document iteration are sorted deterministically.

There is no ignore-file or project-manifest contract. Discovery remains static
for the lifetime of the workspace. Creating, deleting, or renaming a file after
open requires closing and reopening the workspace before that file-set change is
visible.

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
workspace owns syntax state, not semantic compilation Plans or runtime state.

## Refresh and compilation boundary

Session creation asks the workspace to refresh only the selected
already-discovered document. The refresh rereads that saved path and atomically
publishes changed source, syntax state, diagnostics, availability, and revision.
Unchanged contents retain their revision. Missing, unreadable, or invalid
replacement contents remain retained as unavailable or diagnosed state rather
than removing the document from the discovered set.

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
* `internal/language` looks up retained documents by absolute path as static
  baselines for unopened editor documents.
* `internal/grpc` exposes workspace identity and lifecycle only; parser and
  document internals are not wire contracts.
* `client` converts public absolute roots and workspace IDs to that gRPC API.

## Testing changes

Tests should cover canonical roots, root confinement, deterministic ordering,
directory pruning, symlink handling, recoverable document failures, fatal open
rollback, concurrent duplicate opens, cancellation, refresh revisions,
unavailable replacements, close-hook ordering, concurrent close, idempotency,
and engine cleanup. Platform path behavior needs Windows-specific coverage when
root or separator rules change.

Workspace lifecycle and discovery benchmarks live with the manager. Changes to
walking, parsing, copying, lookup, refresh, synchronization, or engine lifetime
should be assessed for allocation, latency, contention, and retained-resource
impact.

# Language Tooling Development

The language path separates protocol-neutral analysis from LSP translation:

```text
ferretd lsp
    -> internal/lsp
    -> internal/language
        -> editor overlay or internal/workspace baseline
        -> Ferret analysis or canonical formatting
        -> internal/source and internal/diagnostic
```

The user-facing setup and supported feature summary lives in
[the LSP guide](../lsp.md). This document focuses on implementation ownership.

## Composition and roots

The `lsp` command constructs one workspace manager, the default immutable
function catalog, one language service, and one LSP server. The catalog merges
executable registration facts from Ferret's runtime with the embedded Standard
Library API Reference. It does not connect to `ferretd serve`.

During LSP initialization, the adapter resolves and deduplicates local roots
from `workspaceFolders`, then `rootUri`, then `rootPath`. Opening each root is a
synchronous workspace operation. Open workspaces track eligible filesystem
changes; dynamic workspace-folder changes are not implemented.

## Source snapshots

`internal/language` resolves one current source snapshot for each request:

1. a versioned editor overlay, when the URI is open; otherwise
2. the retained document from the deepest matching open workspace.

Opening a document stores client-supplied full text as a private overlay. A
change must advance the stored version and supplies full-document text;
incremental ranges are rejected by the adapter. Closing removes the overlay and
is idempotent. If a workspace baseline exists, subsequent requests fall back to
the latest tracked saved-source snapshot.

Overlay and workspace document generations form snapshot identities. Typed
`workspace.Generation` values are internal and remain monotonic across deletion
and same-path recreation; they are distinct from client-visible per-file
revisions. The service stores private overlay values rather than exposing lookup
APIs or mutable references. `DiagnosticReport` carries
`diagnostic.Diagnostic` values directly rather than language-owned aliases.
Editor lifecycle events do not mutate workspace discovery or execution source;
the workspace watcher tracks saved filesystem state independently.

## Analysis coordination

The language service coalesces concurrent analysis of the same URI and snapshot.
One cache entry owns completion signaling, the exact source snapshot, the Ferret
analysis result, and any failure. A snapshot change invalidates the cache entry;
stale asynchronous diagnostic work is suppressed before publication.

Ferret owns parsing, compiler diagnostics, symbols, references, calls, type
facts, and canonical formatting. `internal/language` projects those results into
protocol-neutral diagnostics and feature models. It does not maintain a second
parser, semantic walk, or language definition.

The default function environment is built once at composition. Runtime
registration determines whether a function is executable. For a matching
standard-library function, the embedded API Reference is authoritative for its
authored signatures, parameter names and semantic types, documentation,
returns, failures, variadic state, and deprecation. Runtime-only host functions
retain arity-based placeholder metadata. Reference-only standard-library
functions are omitted and reported as degraded metadata on process stderr;
they are never presented as executable functions.

`internal/language/stdlib/api.json` is checked-in generated content. It is
embedded in the binary, parsed through the Specs API Reference v1 model, and
never read from disk or downloaded while the language server is running.
Completion, hover, and signature help consume normalized catalog symbols rather
than depending on the embedded-reference package. Recursive named, union, and
list types have one shared renderer; language code must not parse legacy opaque
type-expression strings.

## Features and diagnostics

The current service provides compiler diagnostics, document symbols, hover,
document-local definitions and references, completion, signature help, full
semantic tokens, and full-document formatting. Standard-library completion,
hover, and signature help include the embedded authored API metadata. Structured
deprecation is surfaced through those features and does not create diagnostics.
Formatting delegates to Ferret's canonical formatter; invalid source receives
no edit.

Diagnostics preserve primary ranges, related information, codes, severity,
source, and messages from Ferret's current contract. LSP publication includes
the current editor version when available and clears diagnostics on close.

Definitions and references remain document-local. Module resolution, cross-file
indexing, workspace symbols, rename, code actions, range formatting, and
incremental synchronization are not implemented.

## URI and position mapping

`internal/source` owns local file URI parsing and filesystem conversion. Current
document URIs must use the local `file` scheme. Unsupported schemes, non-local
hosts, queries, fragments, and empty paths are rejected. Unix and Windows path
rules, escaping, and `localhost` handling are compatibility-sensitive.

Ferret source spans and protocol-neutral spans use zero-based, half-open UTF-8
byte offsets. Protocol-neutral positions and LSP positions use zero-based lines
and UTF-16 code units with half-open ranges. The mapper clamps invalid offsets
safely and preserves CR, LF, CRLF, Unicode, and astral-character behavior.

Protocol-neutral source and diagnostic types stay below the LSP adapter. The
adapter owns the final conversion to LSP positions, ranges, capabilities, and
payloads.

## LSP adapter and transport

`internal/lsp` owns capability advertisement, lifecycle handlers, full-document
change validation, protocol conversions, request contexts, notification
ordering, and stdio framing. It serializes or coordinates document lifecycle
where notification order affects shared state.

Stdout is protocol-only. Logs, diagnostics as process text, and ordinary command
output must never be written there. Unsupported incremental changes are explicit
errors rather than being treated as whole-document changes.

## Testing changes

Language-service tests own overlay versions, snapshot precedence, fallback,
cache coalescing and invalidation, diagnostics, formatting, symbols, hover,
navigation, completion, signature help, and tokens. Source tests own URI and
UTF-8/UTF-16 mapping. LSP tests own capabilities, protocol payload conversion,
lifecycle ordering, diagnostics publication, stale suppression, and stdio
purity.

Cover empty source, malformed source, CR/LF/CRLF, BMP and astral characters,
invalid offsets, unsupported URI forms, missing documents, stale versions,
cancellation, close fallback, and concurrent requests. Analysis, mapping, and
large-payload changes should be checked for repeated work, copying, allocations,
cache retention, and lock contention.

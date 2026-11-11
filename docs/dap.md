# Debug Adapter Protocol

`ferretd dap` runs a single-session Debug Adapter Protocol server over stdin and
stdout. Stdout contains framed DAP messages only. Adapter diagnostics are
newline-delimited JSON records on the process stderr stream; process-facing
errors are also written to stderr.

## Runtime configuration

Standalone DAP accepts the same startup definitions as `ferretd serve`:

```sh
ferretd dap --runtime-config /absolute/path/runtimes.json
```

See the [runtime configuration example](../README.md#runtime-definitions-and-discovery)
for the JSON schema. The implicit built-in `local` runtime is the default when
the flag or `defaultRuntime` is omitted. Additional Wire definitions and Wire defaults are accepted after strict
configuration validation. Startup and initialization never connect to or probe
Wire targets. Launch is the acquisition boundary; an unavailable default fails
launch without falling back to `local`.

An explicitly requested missing, unreadable, malformed, or invalid file fails
the process with diagnostics on stderr and no stdout bytes. Configuration uses
one strict JSON object, rejecting unknown fields, invalid types, null typed
fields, and trailing values. Relative file paths use the process working
directory. Pass the same absolute configuration path to `serve` and `dap` for
consistent definitions. Each reads once and constructs its own immutable catalog,
runtime acquisition owner, shared native root, and service graph. Its compiled
Session owns the selected binding; DAP closes children and plans before that binding
and closes the owner afterward. It does not connect to `serve`. There is no live
reload. Sharing a file shares definitions, never live instances.

## Diagnostics

DAP lifecycle transitions, rejected or failed requests, debugger stops, and
asynchronous debugger failures are logged at the default `info` verbosity.
Enable semantic request, response, and event tracing with:

```sh
ferretd dap --log-level debug
```

The shared `--log-level` option accepts `debug`, `info`, `warn`, or `error`.
Debug records identify message direction and ordering and include compact fields
such as `runtime_name`, source paths, breakpoint counts, thread IDs, frame handles,
variable references, and evaluation-expression length. Existing workspace, execution
Session, and DebugSession IDs correlate records after a successful launch.
Successful responses that intentionally short-circuit empty evaluations include
`empty=true` together with the request's context, frame handle, and expression
length. Frame allocations and handle invalidations are also recorded at debug
level, including the invalidation cause and per-kind counts. Benign responses
to recognized late requests include `stale=true`; malformed or unknown handles
remain warning-level request failures.
Breakpoint-source warnings include the requested and launched display paths plus
their canonical paths when both sources are available. An unavailable local
source instead records the underlying path-resolution error.

Launch diagnostics distinguish the source `program`, workspace `root`, and
explicit runtime `working_directory`. Successful launch records use the
native canonical directory or exact Wire host value; the field is omitted when
no override was supplied.

Diagnostics never serialize whole DAP payloads. Query contents, parameters,
environment variables, evaluation expressions and results, source contents, and
debug output contents are omitted. Output-event diagnostics include only the
category and byte count. DAP `output` events, including events whose category is
`stderr`, remain framed protocol messages on stdout; they are distinct from the
process diagnostics written directly to stderr.

## Launch

The adapter follows the standard DAP sequence: `initialize`, `launch`, the
`initialized` event, breakpoint configuration, and `configurationDone`.
The launch request remains pending while configuration requests are accepted.
After `configurationDone`, the adapter sends `ConfigurationDoneResponse`, starts
the debug Session, sends the pending `LaunchResponse`, and only then emits
stopped or terminal events.

When omitted, `pathFormat` defaults to `path` and client line and column bases
default to one-based. Explicit zero-based line or column conventions are
preserved.

Launch arguments:

* `program` (required): an existing local `.fql` file, absolute or relative to
  `cwd`;
* `cwd` (optional): an existing compilation/workspace root containing `program`;
  when omitted, the program directory is used;
* `runtimeName` (optional, Ferret-specific): an exact configured runtime name;
  omitted or empty uses this adapter process's configured default;
* `noDebug` (optional, default `false`): must be a boolean; `true` is unsupported;
* `workingDirectory` (optional, Ferret-specific): a runtime filesystem root,
  interpreted and validated by the selected runtime's provider;
* `parameters` (optional): a JSON object bound as Ferret query parameters;
* `stopOnEntry` (optional, default `false`): report the synthetic initial entry
  suspension when `true`. When `false`, suppress only that entry suspension;
  a real breakpoint bound to the entry location is still reported.

A breakpoint at the initial executable location takes priority over entry:
with either value of `stopOnEntry`, the adapter emits one `stopped` event with
reason `breakpoint` and the configured breakpoint IDs. Otherwise, `true` emits
one `stopped(entry)` event and `false` continues automatically. One native
suspension never produces duplicate DAP stopped events.

Initial-stop classification uses the resolved breakpoint bindings active when
start is accepted during `configurationDone`. Later replacements still apply
immediately to the debugger and govern subsequent execution; they cannot
retroactively reclassify or suppress that initial stop.

Launch arguments may contain additional client-supplied properties. The adapter
ignores properties it does not recognize while continuing to decode and validate
the supported arguments above. Explicit null/non-string `runtimeName` and
null/non-boolean `noDebug` fail before acquisition. Names are never trimmed,
interpreted as endpoints, or replaced with another target on failure.

An explicitly selected `program` may be beneath a directory excluded from
automatic workspace discovery, such as `.tmp`, `testdata`, or a nested Go module.
It must still be a regular lowercase `.fql` file inside `cwd`, without nested
symlinks. Selection admits only that file; breakpoints and stack frames keep its
original source path, and later disk edits do not change the launched snapshot.

`workingDirectory` is independent of `cwd`: it may be outside the workspace,
and the program does not need to be inside it. Relative filesystem operations
use this runtime root, while source resolution, breakpoints, and stack-frame
paths continue to use the workspace and launched source. The adapter does not
change its process working directory.

Omitting `workingDirectory` retains no explicit override. Native execution uses
the workspace root; Wire execution inherits the application's runtime default,
without transmitting a local root. Explicit null, blank, and non-string values
fail launch. Native overrides must be absolute: execution trims whitespace,
resolves symlinks, and validates directory access. Wire overrides retain exact
bytes, including host-specific/relative paths; their interpretation and validation
belong to the host. No local path check or OS normalization is applied to Wire.

Example launch configuration:

```json
{
  "type": "ferretd",
  "request": "launch",
  "name": "Debug application query",
  "runtimeName": "application",
  "program": "/project/queries/main.fql",
  "cwd": "/project",
  "parameters": {
    "url": "https://example.com"
  },
  "stopOnEntry": true
}
```

The IDE must separately configure the adapter executable with
`--runtime-config /project/.ferret/runtimes.json`. Configuration paths, endpoints,
credentials, and connection options are not launch arguments. This example does
not imply existing IDE extensions already configure these layers.

## Runtime selection compatibility

Initialize responses include the Ferret extension
`supportsFerretRuntimeSelection: true` alongside standard capabilities. Clients
implementing selection must check it **before sending any nonempty runtimeName**,
including `local`. Absent/false means explicit selection must be prevented: older
adapters may silently ignore the unknown launch property. The capability promises
only selector support, not reachability or debugger availability.

Every successful launch acknowledges the resolved Session name:

```json
{"body": {"runtimeName": "application"}}
```

Verify explicit selections against this acknowledgment. It cannot replace the
pre-launch capability check: the response follows execution-start admission.

## Cancellation and shutdown

The adapter advertises `supportsCancelRequest: true` for its single pending
launch. `cancel` with the launch request's `requestId` requests cancellation during
preparation and configuration, including blocked pre-configuration breakpoints.
Missing, unknown, and completed IDs are successful no-ops. No progress
cancellation is implemented. If cancellation wins, the original launch receives
exactly one unsuccessful response with top-level `message: "cancelled"`; its
error details retain preparation/cleanup causes. The cancel acknowledgment does
not replace that response. A late cancel cannot stop a successfully launched query.

A fully rolled-back attempt before publication permits a later valid launch.
After a prepared session is exposed, cancellation ends that session; restart and
relaunch are unsupported. Non-restart termination is valid during admitted launch
preparation. Disconnect, EOF, process cancellation, and termination interrupt
blocked runtime calls and settle owned resources. Termination and disconnect also
cancel every launch already queued before the control, so rollback cannot admit
another preparation ahead of shutdown. Targeted `cancel` affects only its matching
launch. A control received during `configurationDone` retains its acknowledgment
and normal launch-failure/event ordering. Cleanup is cooperative and may
outlast acquisition cancellation; there is no fixed shutdown deadline.

Input remains responsive through a bounded 64-request FIFO and one semantic
dispatch worker. Controls are validated before their cancellation effects. Queue
overflow is an exceptional adapter failure reported on stderr: active work is
cancelled and cleanup proceeds without depending on another DAP response.

Termination, disconnect (including terminate-related options), EOF, and adapter
failure release the query's resources while leaving the hosting application and
Wire server alive. There is no reconnection or query replay. Debugging steps
through FQL around application functions, not inside their native Go bodies.

## Supported behavior

The adapter supports source breakpoints, continue, pause, next, step-in,
step-out, threads, stack traces, scopes, variables, frame-scoped evaluation,
terminate, disconnect, and pending-launch cancel. Breakpoints replace all prior
breakpoints for the launched source atomically and bind to Ferret's next executable location in that
file. Replacement is supported before execution, while paused, and while running;
an empty set clears the source's breakpoints. A failed replacement leaves the
previous set intact. Updates apply immediately without pausing or resuming
execution. A stop already decided against the previous set remains inspectable
and reports the original DAP breakpoint IDs, even if those breakpoints have since
been removed.
Relative breakpoint paths are resolved against launch `cwd`; clean, symlinked,
and platform-equivalent paths for the launched file share one source identity.

The debugger still owns only the launched source. Clients such as VS Code may
send stored breakpoints for other workspace files while configuring a session.
Those requests succeed with unverified breakpoints and do not change debugger
state. Unavailable local source paths are reported the same way. Empty paths,
remote URIs, and nonzero source references remain request errors.

There is one thread named `Ferret`. Frame index zero is the current frame and
subsequent frames are callers. Each frame exposes `Locals` and `Parameters`.
Expandable value references and all DAP handles are valid only for the current
paused state; running, stepping, completion, failure, or termination makes them
stale. Handle numbers are opaque and never reused within an adapter session, so
a handle from an earlier stop cannot resolve to a frame or value from a later
stop.

Some clients issue inspection requests after a resume or step has already
invalidated the handles they reference. When the adapter recognizes a handle
from the most recently invalidated paused state, late `scopes` and `variables`
requests succeed with empty arrays. A non-empty `evaluate` request against such
a frame succeeds with an empty result only for passive `hover` and `watch`
contexts. REPL, omitted or unfamiliar evaluation contexts, wrong handle kinds,
and unknown handle numbers remain request errors.

After a debug Session is configured, an empty or whitespace-only evaluation
returns a successful empty result with no variable reference. This applies to
all evaluation contexts, including REPL input. The adapter does not resolve a
supplied frame handle or call the debugger for that response. Non-empty
expressions retain normal frame validation and debugger evaluation.

The adapter honors client line and column bases and accepts either native local
paths or local `file` URIs according to the initialized `pathFormat`. Remote
URIs and nonzero source references are rejected.

Runtime errors stop as DAP exceptions and remain inspectable. Continuing that
stop reports stderr output, exit code 1, and termination. Successful completion
emits the encoded result as stdout output, exit code 0, and termination.
Explicit termination emits termination without an exit event.

## Source coordinates

Ferret and the Universal API preserve native one-based lines and one-based
UTF-8 byte columns, with zero-based half-open byte spans. DAP columns count
UTF-16 code units. The adapter converts in both directions at the DAP boundary;
`linesStartAt1` and `columnsStartAt1` independently select the client bases
and default to one-based.

Conversion uses an immutable index of the exact source bytes compiled at launch.
Editing the file does not change active-session coordinates; relaunch to debug
the edited source. No source remapping or hot recompilation is performed.

An omitted breakpoint column remains a line-only request. An explicit zero is
the first column for a zero-based client and is invalid for a one-based client.
Out-of-range positions, columns inside surrogate pairs, and native positions
inside valid UTF-8 sequences are rejected rather than rounded. Valid positions
may subsequently bind to a later executable location under Ferret's existing
breakpoint policy. Unbound breakpoints retain their requested coordinates.

Emoji and other supplementary characters occupy two UTF-16 units. Tabs occupy
one unit, and combining sequences count their code units rather than displayed
glyphs. LF determines native source lines; CRLF terminators do not contribute
logical columns. Standalone CR and Unicode separators retain native Ferret
column behavior.

Malformed UTF-8 follows Ferret's existing decoding: each invalid byte consumes
exactly one original byte and contributes one replacement rune, hence one UTF-16
unit. Both conversion directions use those same boundaries. The retained bytes
are never normalized, rewritten, or rejected solely for malformed encoding.
DAP JSON transport remains UTF-8 independently of source-coordinate units.

## Exclusions

DAP is stdio-only and owns one launched session. It does not connect to
`ferretd serve`, listen on TCP, attach to an existing process, restart a session,
mutate variables, execute backwards, or implement conditional, hit-count, log,
function, data, or instruction breakpoints. Debug gRPC and public Go client APIs
remain unimplemented.

# Debug Adapter Protocol

`ferretd dap` runs a single-session Debug Adapter Protocol server over stdin and
stdout. Stdout contains framed DAP messages only. Adapter diagnostics are
newline-delimited JSON records on the process stderr stream; process-facing
errors are also written to stderr.

## Diagnostics

DAP lifecycle transitions, rejected or failed requests, debugger stops, and
asynchronous debugger failures are logged at the default `info` verbosity.
Enable semantic request, response, and event tracing with:

```sh
ferretd dap --log-level debug
```

The shared `--log-level` option accepts `debug`, `info`, `warn`, or `error`.
Debug records identify message direction and ordering and include compact fields
such as source paths, breakpoint counts, thread IDs, frame handles, variable
references, and evaluation-expression length. Existing workspace, execution
Session, and DebugSession IDs correlate records after a successful launch.
Breakpoint-source warnings include the requested and launched display paths plus
their canonical paths when both sources are available. An unavailable local
source instead records the underlying path-resolution error.

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
* `cwd` (optional): an existing workspace root containing `program`; when
  omitted, the program directory is used;
* `parameters` (optional): a JSON object bound as Ferret query parameters;
* `stopOnEntry` (optional): emit the initial `stopped(entry)` event when `true`;
  the default is `false`, which continues past Ferret's entry stop.

Launch arguments may contain additional client-supplied properties. The adapter
ignores properties it does not recognize while continuing to decode and validate
the supported Ferret arguments above.

Example launch configuration:

```json
{
  "type": "ferretd",
  "request": "launch",
  "name": "Debug query",
  "program": "${workspaceFolder}/queries/main.fql",
  "cwd": "${workspaceFolder}",
  "parameters": {
    "url": "https://example.com"
  },
  "stopOnEntry": true
}
```

## Supported behavior

The adapter supports source breakpoints, continue, pause, next, step-in,
step-out, threads, stack traces, scopes, variables, frame-scoped evaluation,
terminate, and disconnect. Breakpoints replace all prior breakpoints for the
launched source and bind to Ferret's next executable location in that file.
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
stale.

The adapter honors client line and column bases and accepts either native local
paths or local `file` URIs according to the initialized `pathFormat`. Remote
URIs and nonzero source references are rejected.

Runtime errors stop as DAP exceptions and remain inspectable. Continuing that
stop reports stderr output, exit code 1, and termination. Successful completion
emits the encoded result as stdout output, exit code 0, and termination.
Explicit termination emits termination without an exit event.

## Exclusions

DAP is stdio-only and owns one launched session. It does not connect to
`ferretd serve`, listen on TCP, attach to an existing process, restart a session,
mutate variables, execute backwards, or implement conditional, hit-count, log,
function, data, or instruction breakpoints. Debug gRPC and public Go client APIs
remain unimplemented.

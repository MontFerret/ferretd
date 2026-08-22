# Debug Adapter Protocol

`ferretd dap` runs a single-session Debug Adapter Protocol server over stdin and
stdout. Stdout contains framed DAP messages only; process-facing errors are
reported by the `ferretd` command on stderr after the adapter exits.

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
same source and bind to Ferret's next executable location in that file.

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

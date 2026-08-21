# Daemon and Protocol Development

The `serve` command runs the local gRPC daemon. Command setup remains thin:
`cmd/ferretd` parses process-facing options and composes `internal/daemon`, while
domain state stays in the workspace and execution managers.

See [architecture.md](architecture.md) for the repository-wide dependency map
and [workspace.md](workspace.md) or [execution.md](execution.md) for the state
owned below the daemon.

## Process composition

`cmd/ferretd/serve.go` parses an optional local endpoint, creates a logger that
writes to stderr, constructs the daemon, and starts it with the command context.
After `Start` returns, it calls `Stop` with a bounded shutdown context. Process
signals cancel the root context in `cmd/ferretd/main.go`.

The daemon constructs one workspace manager, one execution manager, and one
gRPC server. The execution manager registers its workspace close hook during
construction so child Sessions and Executions are released before a workspace
engine closes. Required service dependencies are passed explicitly.

## Lifecycle

`internal/daemon` has one authoritative lifecycle from new through starting,
running, stopping, and stopped. `Start` listens, marks gRPC health serving, and
serves until process cancellation, an RPC shutdown request, explicit stop, or a
transport failure.

`Stop` is idempotent and coordinates these responsibilities:

1. mark health unavailable;
2. close execution resources;
3. clear workspaces and their rooted engines;
4. drain or force-stop gRPC according to its context;
5. close the local listener;
6. publish one retained stop result to concurrent callers.

Cancellation and partial startup must not leave a listener, manager resource,
or goroutine behind. Shutdown errors are joined so cleanup continues after an
individual failure.

## Local transport

The daemon is local-only. `internal/transport` represents endpoint families with
`Network`, using `NetworkUnix` on macOS and Linux and `NetworkNamedPipe` on
Windows; TCP, TLS, and remote modes are not supported.

The default Unix endpoint uses the runtime directory when available and falls
back to the user cache directory. Created directories and sockets are restricted
to the current user. The Windows named pipe uses a current-user and LocalSystem
access-control list. Explicit endpoint syntax is part of the integration
contract and is shared by the server and public client.

Transport code owns endpoint parsing, default discovery, listener cleanup, and
platform-specific dialing. gRPC code should not duplicate those rules.

## gRPC services and compatibility

`internal/grpc` registers the standard health service plus the implemented
daemon, workspace, and execution services. It converts protobuf values and
domain errors without retaining workspace, Session, or Execution state.

Clients send their API version during daemon information negotiation. A major
version mismatch is rejected with structured compatibility detail; minor
versions are additive. Process-local state survives client disconnects but not
daemon restarts.

The supported `client` package owns public discovery, dialing, negotiation,
workspace and execution facades, stream handling, conversion, and error
classification. Generated clients remain an implementation detail. Changes to
public client types, endpoint behavior, or error identity require compatibility
review and boundary tests.

## Protobuf ownership and generation

Source contracts live below `proto/ferretd` in versioned packages:

* daemon v1: information, negotiation, and shutdown;
* workspace v1: open, get, list, and close;
* execution v1: Session and Execution lifecycle plus execution watching;
* debug v1: an unimplemented placeholder capability declaration.

`buf.gen.yaml` intentionally includes only daemon, workspace, and execution.
Pinned Buf and Go protobuf tools generate checked-in code below `gen/`. Debug
generation, registration, gRPC behavior, and public client APIs remain absent.

Never edit `gen/` directly. Change the versioned source and generation
configuration, run the repository generation target, inspect the complete
generated diff, lint all protobuf sources, and run the checked-in generation
gate. Field numbers, service and message names, package paths, and enum values
are compatibility-sensitive.

## Testing changes

Daemon lifecycle tests belong in `internal/daemon`; local endpoint behavior in
`internal/transport`; RPC conversion and health behavior in `internal/grpc`;
and public discovery, negotiation, streaming, and error behavior in `client`.

Lifecycle coverage should include cancellation, partial startup, repeated stop,
concurrent stop, listener cleanup, health transitions, and resource-close
ordering. Protocol changes require domain tests plus wire or translation tests.
The end-to-end daemon tests exercise the real local listener and supported
client across workspace and execution behavior.

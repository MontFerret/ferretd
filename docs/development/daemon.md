# Daemon and Protocol Development

The `serve` command runs the local gRPC daemon. Command setup remains thin:
`cmd/ferretd` parses process-facing options and composes `internal/daemon`, while
domain state stays in the workspace and execution managers.

See [architecture.md](architecture.md) for the repository-wide dependency map
and [workspace.md](workspace.md) or [execution.md](execution.md) for the state
owned below the daemon.

## Process composition

`cmd/ferretd/serve.go` parses an optional local endpoint, the TCP-only
`--auth-token-env` setting, an optional `--runtime-config` JSON file, and the shared
`--log-level` setting. It creates a logger that writes newline-delimited JSON
to stderr, constructs the daemon, and
starts it with the command context. The level accepts `debug`, `info`, `warn`,
or `error` and defaults to `info`; the DAP command uses the same logger
construction and level contract. After `Start` returns, `serve` calls `Stop`
with a bounded shutdown context. Process signals cancel the root context in
`cmd/ferretd/main.go`.

The daemon constructs its owning Universal runtime with Ferret's upstream
`uapi.New`, supplying the selected Ferret Core version independently of the
daemon version, and transfers it into `runtimes.Owner`. Composition owns that
owner alongside one workspace manager, one execution manager, and one gRPC server.
The execution manager borrows the owner and acquires one binding per compiled
Session. It registers its workspace close hook during construction so children,
Plans, and bindings are released before workspace state.
Required service dependencies are passed explicitly. Runtime cleanup closes the
owner, which closes the upstream wrapper exactly once; composition does not
also close the wrapper or engine independently.

`internal/runtimes` owns the shared configuration schema, normalization,
validation, exact metadata resolution, and immutable startup catalog. Commands
read an explicit file once through its context-aware loader and pass parsed
configuration through `daemon.Options.RuntimeConfig` or `dap.Options.RuntimeConfig`.
Programmatic construction uses the same catalog constructor. Each composition
validates one catalog before `uapi.New` or other owned services. Standalone DAP
also rejects unsupported defaults there; daemon Wire acquisition stays lazy. The daemon passes that catalog explicitly to gRPC;
adapters
and execution/debug managers never load configuration files.

The implicit `local` definition is always present, labeled `Built-in Ferret`,
with provider `native`. Additional definitions use provider `wire`, a unique
case-sensitive name without surrounding whitespace, a nonblank endpoint, an
optional label, and an optional positive duration string for `connectTimeout`.
Empty labels use the name; omitted timeouts use `5s`. An omitted default selects
`local`; blank or unknown defaults are invalid. Wire defaults are valid catalog
metadata and supported by daemon Session creation. Standalone DAP still rejects
them until DAP launch selection is implemented. Resolution itself never connects
or falls back after an unknown selection.

The loader requires one JSON object, rejects unknown fields, null typed fields,
invalid types, and trailing values, and reports explicit file failures. Relative
paths use the process working directory. Recommend the same absolute file path
for `serve` and standalone `dap`, which retain separate snapshots and runtimes.
There is no reload, workspace discovery, or mutation/persistence API. Outbound
Wire endpoint validation is definition-owned and uses `tcp://host:port`, including
hostnames and bracketed IPv6, with numeric ports `1..65535`. Userinfo, paths,
queries, fragments, and other schemes are rejected without dialing. The inbound
daemon endpoint parser has different semantics and is not reused.

An optional strict `insecure` boolean defaults to false: Wire construction uses
TLS with system trust and identity verification unless true explicitly supplies
`WithInsecure()` for plaintext. No bearer token is forwarded from incoming daemon
requests, and no weaker-transport retry is attempted.

`Owner.Acquire` resolves through the same catalog. Native bindings share one root;
every Wire binding owns a fresh `wireclient.New` runtime and channel. The caller's
earlier deadline or configured `connectTimeout` bounds startup, while Wire's
bounded detached rollback can outlast cancellation. Successful publication
separates the binding lifetime from its startup context. No extra probe, cached
connection, automatic replacement, or query replay is added. Session creation selects and retains a binding; discovery and startup never
connect to Wire, including when it is the default.

## Lifecycle

`internal/daemon` has one authoritative lifecycle from new through starting,
running, stopping, and stopped. `Start` listens, marks gRPC health serving, and
serves until process cancellation, an RPC shutdown request, explicit stop, or a
transport failure. Once startup succeeds, it emits one `ferretd.ready` JSON
diagnostic containing the actual bound endpoint and daemon version. Callers use
that event as the machine-readable startup handshake; it reports the OS-assigned
port for an ephemeral TCP listener and is never filtered by `--log-level`.

`Stop` is idempotent and coordinates these responsibilities:

1. mark health unavailable;
2. close execution resources;
3. clear workspaces;
4. drain gRPC under the committed cleanup operation;
5. close the local listener;
6. close the runtime owner and shared root exactly once, after Session bindings;
7. publish one retained stop result to concurrent callers.

Cancellation and partial startup must not leave a listener, manager resource,
runtime, or goroutine behind. Once stop commits, cleanup proceeds under its own
operation even if an individual caller stops waiting. Concurrent and repeated
callers observe the retained final result. Shutdown errors are joined so cleanup
continues after an individual failure.

Owner shutdown also cancels pending constructors and waits for their rollback,
then closes unreleased Wire roots. Closing bindings remain tracked until their
release settles. Child outputs, sessions/debuggers, and plans must close first;
Wire runtime closure cannot substitute for caller-owned child cleanup.

## Local transport

The daemon is local-only. `internal/transport` represents endpoint families with
`Network`, using `NetworkUnix` on macOS and Linux and `NetworkNamedPipe` on
Windows. Those permission-restricted native transports remain the defaults.
Opt-in TCP is restricted to IPv4 loopback: servers accept only
`tcp://127.0.0.1:0`, and clients dial only a reported nonzero loopback port.
Hostnames, other addresses, configured listener ports, TLS, and remote modes are
not supported.

The default Unix endpoint uses the runtime directory when available and falls
back to the user cache directory. Created directories and sockets are restricted
to the current user. The Windows named pipe uses a current-user and LocalSystem
access-control list. Explicit endpoint syntax is part of the integration
contract and is shared by the server and public client.

Transport code owns endpoint parsing, default discovery, listener cleanup, and
platform-specific dialing. gRPC code should not duplicate those rules.

TCP requires `--auth-token-env=<name>`. Command setup reads a nonempty bearer
token from that environment variable without logging it. `internal/grpc`
requires `authorization: Bearer <token>` on every unary and streaming RPC,
including health checks, and compares credentials in constant time. Native
endpoints reject bearer-token configuration and remain unauthenticated. After
resolving its effective endpoint, the public client enforces the same invariant
and attaches TCP credentials only through the explicit `client.WithBearerToken`
option.

## gRPC services and compatibility

`internal/grpc` registers the standard health service plus the implemented
daemon, workspace, and execution services. It converts protobuf values and
domain errors without retaining workspace, Session, or Execution state.

Clients send their API version during daemon information negotiation. A major
version mismatch is rejected with structured compatibility detail; minor
versions are additive. The current version is 1.3; transport and bootstrap
features do not change the RPC API version. Process-local state survives client
disconnects but not daemon restarts.

The supported `client` package owns public discovery, dialing, negotiation,
workspace and execution facades, stream handling, conversion, and error
classification. Generated clients remain an implementation detail. Changes to
public client types, endpoint behavior, or error identity require compatibility
review and boundary tests.

`DaemonService.ListRuntimes` returns one default name and descriptors containing
name, label, and string provider, ordered by name. It describes configured
targets without connection settings or capability/reachability claims and does
no outbound work. `Client.ListRuntimes` returns independent semantic snapshots,
validates incomplete responses, and preserves future nonempty provider strings.
Discovery against an older compatible daemon returns a classifiable
`client.ErrUnsupportedOperation` while retaining the original gRPC error.
Minor-version differences never prevent initial negotiation or existing calls.

API 1.3 adds Session runtime selection and acknowledgment. Protocol clients must
feature-gate every explicit name before creation, then require an identical
acknowledgment; new-feature Session responses require a nonempty resolved name.
Older unselected responses may omit it. Rejected creations with an identifiable
Session require bounded detached cleanup without replay. The Go client retains
initial negotiation, uses a five-second cleanup timeout, and distinguishes
runtime lookup/acquisition failures from source and daemon failures through
execution resource details.

## Protobuf ownership and generation

Source contracts live below `proto/ferretd` in versioned packages:

* daemon v1: information, negotiation, shutdown, and runtime discovery;
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

Daemon lifecycle and readiness tests belong in `internal/daemon`; local endpoint
behavior in `internal/transport`; RPC conversion, authentication, and health
behavior in `internal/grpc`; and public discovery, negotiation, streaming,
credentials, and error behavior in `client`.

Lifecycle coverage should include cancellation, partial startup, repeated stop,
concurrent stop, listener cleanup, health transitions, and resource-close
ordering. Protocol changes require domain tests plus wire or translation tests.
The end-to-end daemon tests exercise the real local listener and supported
client across workspace and execution behavior.

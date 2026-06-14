# Architecture

`ferretd` is intended to become a long-running coordinator for Ferret developer
tooling:

```text
ferretd
├── LSP adapter
├── gRPC daemon API
│   ├── WorkspaceService
│   ├── ExecutionService
│   └── DebugService
├── language service
├── workspace manager
├── execution session manager
└── debug session manager
```

## Responsibilities

The daemon owns lifecycle and coordination. The workspace manager will own
workspace state, while the protocol-neutral language service will provide
language features to protocol adapters and other integrations. Execution and
debug session managers will coordinate sessions backed by the main Ferret
project.

LSP and future DAP support are protocol adapters. They should translate
protocol messages and delegate to shared language, workspace, execution, and
debug services. Protocol adapters must not own core language or debugging
behavior.

The gRPC daemon API will expose versioned Ferret-specific services. The initial
contracts under `proto/ferretd/` establish package names only; generation and
server behavior are intentionally deferred.

## Boundaries

`ferretd` does not replace the Ferret VM. Parsing, compilation, runtime
semantics, VM execution, and core debugging behavior remain owned by the main
Ferret project. This repository coordinates those capabilities for long-running
developer workflows.

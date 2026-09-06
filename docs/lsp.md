# Experimental Language Server

`ferretd lsp` starts an experimental Ferret language server over stdin and
stdout. It provides Ferret compiler diagnostics, document symbols, hover,
definition, document-local references, completion, signature help, full
semantic tokens, and full-document formatting. It never executes documents.

Build the server:

```sh
make build
```

Configure an editor language client to launch:

```sh
/absolute/path/to/ferretd/bin/ferretd lsp
```

The client should associate the server with `.fql` files and send
full-document text synchronization. The server advertises open/close
notifications and full-document changes.

During initialization, the server opens deduplicated local roots from
`workspaceFolders`, falling back to `rootUri` and then `rootPath`. Tracked
workspace snapshots supply unopened document contents. Versioned editor text is
retained as an overlay and takes precedence until `didClose`, when language
requests fall back to the latest saved workspace snapshot. Dynamic discovery
uses the same exclusions, nested-module boundaries, and symlink rules as the
initial workspace load.

Language-word completions use canonical lowercase labels and insertion text.
Source-defined names, namespace aliases, bind parameters, and registered
functions retain their declared spelling.

Standard-library completion, hover, and signature help use the structured API
Reference matching the Ferret dependency used to build `ferretd`. Authored
overloads, parameter names and semantic types, descriptions, returns, failures,
variadic state, and deprecation are preserved. Runtime-only host functions
remain available with limited placeholder metadata. A standard-library API
entry that has no executable runtime function is omitted and reported as a
warning on stderr.

The reference is embedded in the executable. Language requests do not download
documentation or depend on an installed reference file, so these features work
fully offline. Deprecation appears in completion and documentation where LSP
supports it; the server does not emit deprecation diagnostics.

Formatting delegates to Ferret's canonical formatter. `tabSize` selects its
indent width, but canonical output remains space-indented even when the client
sets `insertSpaces` to false. Invalid source receives no formatting edit.

For a minimal VS Code extension client, configure the server command and file
selector similarly to:

```ts
const serverOptions = {
  command: "/absolute/path/to/ferretd/bin/ferretd",
  args: ["lsp"],
};

const clientOptions = {
  documentSelector: [{ scheme: "file", pattern: "**/*.fql" }],
};
```

Editor integration is experimental. Definitions and references remain within
one document. Rename, code actions, workspace symbols, cross-file references,
range formatting, incremental synchronization, project/module resolution,
execution, and debugging are not supported.

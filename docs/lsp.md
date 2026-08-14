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
`workspaceFolders`, falling back to `rootUri` and then `rootPath`. Static
workspace snapshots supply unopened document contents. Versioned editor text
is retained as an overlay and takes precedence until `didClose`, when language
requests fall back to the unchanged workspace snapshot. Disk changes are not
watched; close and reopen the workspace to reload them.

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

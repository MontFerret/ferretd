# Experimental Language Server

`ferretd lsp` starts an experimental Ferret language server over stdin and
stdout. It currently tracks open `.fql` documents and publishes parser and
compiler diagnostics. It does not execute documents or read their contents from
disk.

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

Editor integration is experimental. Completion, hover, formatting, navigation,
semantic tokens, code actions, execution, and debugging are not supported.

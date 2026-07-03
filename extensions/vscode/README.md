# Renart SQL LSP

VS Code extension for the Go-based Renart SQL language server.

The extension starts:

```sh
renart sql-lsp --workspace <workspace>
```

It is intended for dbt and Bruin pipeline repositories. The server downloads
and verifies the matching Polyglot SQL FFI library automatically unless
`renartSqlLsp.disablePolyglotDownload` is enabled.


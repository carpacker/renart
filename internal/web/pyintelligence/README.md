# Python Intelligence WASM

`ty_wasi.wasm` is a custom `wasm32-wasip1` build of ty from the Ruff repository.

- Source repository: `https://github.com/astral-sh/ruff`
- Source commit used for this artifact: `7287ad75a3313004b364d97a4cc8d8369e764e5b`
- Wrapper shape: a small non-`wasm-bindgen` crate that reuses ty's in-memory workspace logic and exposes JSON over raw linear-memory pointers.
- Exports used by Go: `ty_alloc`, `ty_dealloc`, `ty_result_free`, `ty_format_python`, and `ty_check_python`.
- Requests can include virtual files. Renart uses this to mount lightweight stubs under `/site-packages` for packages installed through Bruin/uv dependency flows.

This avoids depending on the browser-oriented `ty_wasm` JavaScript glue in the Go server. The Go runtime instantiates the module through wazero with `wasi_snapshot_preview1`.

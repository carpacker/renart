# ty-wasi Wrapper

This crate is the source for Renart's embedded `internal/web/pyintelligence/ty_wasi.wasm` artifact.

The wrapper is intentionally not a standalone Rust workspace. It is a small crate meant to be copied into the Ruff repository under `crates/ty_wasi`, where it can reuse Ruff/ty internal crates such as `ty_project`, `ruff_db`, and `ruff_python_formatter`.

Source used for the current artifact:

- Ruff repository: `https://github.com/astral-sh/ruff`
- Ruff commit: `7287ad75a3313004b364d97a4cc8d8369e764e5b`
- Rust toolchain: `1.96.0`
- Target: `wasm32-wasip1`

Rebuild workflow:

1. Clone Ruff at the commit above.
2. Copy this directory to `<ruff>/crates/ty_wasi`.
3. Build with `cargo build -p ty_wasi --target wasm32-wasip1 --release`.
4. Copy `<ruff>/target/wasm32-wasip1/release/ty_wasi.wasm` to `internal/web/pyintelligence/ty_wasi.wasm`.

The exported ABI is JSON over raw linear-memory pointers:

- `ty_alloc(len) -> ptr`
- `ty_dealloc(ptr, len)`
- `ty_result_free(packed_ptr_len)`
- `ty_format_python(ptr, len) -> packed_ptr_len`
- `ty_check_python(ptr, len) -> packed_ptr_len`
- `ty_complete_python(ptr, len) -> packed_ptr_len`
- `ty_hover_python(ptr, len) -> packed_ptr_len`
- `ty_signature_help_python(ptr, len) -> packed_ptr_len`
- `ty_goto_definition_python(ptr, len) -> packed_ptr_len`

Requests may include virtual files under `/site-packages`. Renart uses this to expose installed Python packages to ty without copying full uv cache environments into WASM memory.

Completion requests may set `completion_details` and `completion_documentation` to include per-item type strings and rendered documentation. Renart leaves these disabled for the normal completion list and relies on hover/signature requests for detailed information, which avoids eager rendering for large objects such as `pandas.DataFrame`.

Diagnostics and completions may include `session_id` and `session_fingerprint`. Matching sessions reuse a warm ty project database and apply file-content changes incrementally; changed fingerprints rebuild the session so package stubs and options stay correct.

#!/usr/bin/env bash

set -euo pipefail

target="${1:?missing target name}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="${RENART_BRUIN_SQLPARSER_STUB_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/renart/bruin-sqlparser-stub}"
output_dir="${cache_root}/${target}/release"
archive="${output_dir}/libbruin_rustsqlparser.a"
source_file="${repo_root}/scripts/bruin_rustsqlparser_stub.c"

read -r -a compiler <<< "${CC:-cc}"
read -r -a archiver <<< "${AR:-ar}"

if ! command -v "${compiler[0]}" >/dev/null 2>&1; then
  echo "${compiler[0]} is required to build the Bruin SQL parser link shim" >&2
  exit 1
fi
if ! command -v "${archiver[0]}" >/dev/null 2>&1; then
  echo "${archiver[0]} is required to archive the Bruin SQL parser link shim" >&2
  exit 1
fi

mkdir -p "${output_dir}"
build_dir="$(mktemp -d "${output_dir}/.build.XXXXXX")"
cleanup() {
  rm -rf "${build_dir}"
}
trap cleanup EXIT

"${compiler[@]}" -std=c11 -O2 -c "${source_file}" -o "${build_dir}/bruin_rustsqlparser_stub.o"
"${archiver[@]}" rcs "${build_dir}/libbruin_rustsqlparser.a" "${build_dir}/bruin_rustsqlparser_stub.o"
mv "${build_dir}/libbruin_rustsqlparser.a" "${archive}"

printf '%s\n' "${output_dir}"

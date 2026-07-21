#!/usr/bin/env bash

set -euo pipefail

target="${1:?missing target triple}"
rust_toolchain="${RENART_RUST_TOOLCHAIN:-1.96.0}"
lockdir="${HOME}/.cache/renart-rustsqlparser-release.lock"
cache_root="${XDG_CACHE_HOME:-${HOME}/.cache}/renart/rustsqlparser"
target_dir="${RENART_RUSTSQLPARSER_TARGET_DIR:-${cache_root}/target}"
go_command="${GO:-go}"

mkdir -p "$(dirname "${lockdir}")"

while ! mkdir "${lockdir}" 2>/dev/null; do
	sleep 1
done

cleanup() {
	rmdir "${lockdir}"
}

trap cleanup EXIT

if ! command -v rustup >/dev/null 2>&1; then
	echo "rustup is required to build Bruin's Rust SQL parser (expected toolchain ${rust_toolchain})" >&2
	exit 1
fi
if ! command -v "${go_command}" >/dev/null 2>&1; then
	echo "${go_command} is required to locate the pinned Bruin module" >&2
	exit 1
fi

if ! rustup toolchain list | awk '{print $1}' | grep -Eq "^${rust_toolchain}(-|$)"; then
	echo "Rust toolchain ${rust_toolchain} is not installed; install it with: rustup toolchain install ${rust_toolchain} --profile minimal" >&2
	exit 1
fi

bruin_version="$("${go_command}" list -m -f '{{.Version}}' github.com/bruin-data/bruin)"
if [ -z "${bruin_version}" ]; then
	echo "unable to resolve github.com/bruin-data/bruin from go.mod" >&2
	exit 1
fi

"${go_command}" mod download "github.com/bruin-data/bruin@${bruin_version}"

module_dir="$("${go_command}" list -m -f '{{.Dir}}' github.com/bruin-data/bruin 2>/dev/null || true)"

vendor_dir="$(pwd)/vendor/github.com/bruin-data/bruin"
if [ -f "${vendor_dir}/pkg/sqlparser/rustffi/Cargo.toml" ]; then
	module_dir="${vendor_dir}"
elif [ -z "${module_dir}" ] || [ ! -d "${module_dir}" ]; then
	module_dir="$("${go_command}" env GOMODCACHE)/github.com/bruin-data/bruin@${bruin_version}"
fi

rustffi_dir="${module_dir}/pkg/sqlparser/rustffi"

if [ ! -f "${rustffi_dir}/Cargo.toml" ]; then
	echo "unable to locate Bruin rustffi sources at ${rustffi_dir}" >&2
	exit 1
fi

if [ "${target}" = "x86_64-pc-windows-gnu" ]; then
	mingw_include_dir="/usr/x86_64-w64-mingw32/include"
	for header in KnownFolders.h ShlObj.h Propkey.h; do
		lower_header="$(printf '%s' "${header}" | tr '[:upper:]' '[:lower:]')"
		if [ -f "${mingw_include_dir}/${lower_header}" ] && [ ! -e "${mingw_include_dir}/${header}" ]; then
			ln -s "${mingw_include_dir}/${lower_header}" "${mingw_include_dir}/${header}"
		fi
	done
fi

rustup target add --toolchain "${rust_toolchain}" "${target}" >/dev/null
rustup run "${rust_toolchain}" cargo build \
	--locked \
	--release \
	--manifest-path "${rustffi_dir}/Cargo.toml" \
	--target "${target}" \
	--target-dir "${target_dir}"

target_archive="${target_dir}/${target}/release/libbruin_rustsqlparser.a"

test -f "${target_archive}"
printf '%s\n' "${target_dir}/${target}/release"

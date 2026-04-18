#!/usr/bin/env bash

set -euo pipefail

target="${1:?missing target triple}"
lockdir="${HOME}/.cache/renart-rustsqlparser-release.lock"

mkdir -p "$(dirname "${lockdir}")"

while ! mkdir "${lockdir}" 2>/dev/null; do
	sleep 1
done

cleanup() {
	rmdir "${lockdir}"
}

trap cleanup EXIT

if ! command -v rustup >/dev/null 2>&1; then
	curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal --default-toolchain stable
fi

if [ -f "${HOME}/.cargo/env" ]; then
	# shellcheck source=/dev/null
	. "${HOME}/.cargo/env"
fi

if ! command -v cargo >/dev/null 2>&1; then
	echo "cargo not found after Rust setup" >&2
	exit 1
fi

modcache="$(go env GOPATH)/pkg/mod"
module_dir="${modcache}/github.com/bruin-data/bruin@v0.11.528"

if [ ! -d "${module_dir}" ]; then
	go mod download github.com/bruin-data/bruin@v0.11.528
fi

rustffi_dir="${module_dir}/pkg/sqlparser/rustffi"

if [ ! -f "${rustffi_dir}/Cargo.toml" ]; then
	echo "unable to locate Bruin rustffi sources at ${rustffi_dir}" >&2
	exit 1
fi

build_root="$(mktemp -d)"
full_cleanup() {
	rm -rf "${build_root}"
	cleanup
}
trap full_cleanup EXIT
work_dir="${build_root}/rustffi"
mkdir -p "${work_dir}"
cp -R "${rustffi_dir}/." "${work_dir}/"

rustup target add "${target}"
cargo build --release --manifest-path "${work_dir}/Cargo.toml" --target "${target}"

mkdir -p "${rustffi_dir}/target/${target}/release"
cp "${work_dir}/target/${target}/release/libbruin_rustsqlparser.a" "${rustffi_dir}/target/${target}/release/"

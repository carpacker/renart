#!/usr/bin/env bash

set -euo pipefail

version="${RENART_ZIG_VERSION:-0.15.2}"
if [ "${version}" != "0.15.2" ]; then
	echo "unsupported Zig version ${version}; update this script with its pinned checksum first" >&2
	exit 1
fi

case "$(uname -m)" in
	x86_64)
		archive_arch="x86_64"
		checksum="02aa270f183da276e5b5920b1dac44a63f1a49e55050ebde3aecc9eb82f93239"
		;;
	aarch64 | arm64)
		archive_arch="aarch64"
		checksum="958ed7d1e00d0ea76590d27666efbf7a932281b3d7ba0c6b01b0ff26498f667f"
		;;
	*)
		echo "unsupported host architecture for the pinned Zig release: $(uname -m)" >&2
		exit 1
		;;
esac

cache_root="${XDG_CACHE_HOME:-${HOME}/.cache}/renart/zig"
install_dir="${cache_root}/${version}"
zig="${install_dir}/zig"

if [ -x "${zig}" ] && [ "$("${zig}" version)" = "${version}" ]; then
	printf '%s\n' "${zig}"
	exit 0
fi

for command in curl sha256sum tar; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		echo "${command} is required to install the pinned Zig release toolchain" >&2
		exit 1
	fi
done

mkdir -p "${cache_root}"
workdir="$(mktemp -d "${cache_root}/install.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT
archive="${workdir}/zig.tar.xz"
url="https://ziglang.org/download/${version}/zig-${archive_arch}-linux-${version}.tar.xz"

curl --fail --location --proto '=https' --tlsv1.2 --output "${archive}" "${url}"
printf '%s  %s\n' "${checksum}" "${archive}" | sha256sum --check --status
mkdir -p "${workdir}/unpacked"
tar -xJf "${archive}" --strip-components=1 -C "${workdir}/unpacked"
rm -rf "${install_dir}"
mv "${workdir}/unpacked" "${install_dir}"

test "$("${zig}" version)" = "${version}"
printf '%s\n' "${zig}"

#!/usr/bin/env bash

set -euo pipefail

version="${RENART_SYFT_VERSION:-1.44.0}"
if [ "${version}" != "1.44.0" ]; then
	echo "unsupported Syft version ${version}; update this script with its pinned checksum first" >&2
	exit 1
fi

case "$(uname -m)" in
	x86_64)
		archive_arch="amd64"
		checksum="0e91737aee2b5baf1d255b959630194a302335d848ff97bb07921eb6205b5f5a"
		;;
	aarch64 | arm64)
		archive_arch="arm64"
		checksum="6f6cdcdc695721d91ce756e3b5bc3e3416599c464101f5e32e9c3f33054ee6d9"
		;;
	*)
		echo "unsupported host architecture for the pinned Syft release: $(uname -m)" >&2
		exit 1
		;;
esac

cache_root="${XDG_CACHE_HOME:-${HOME}/.cache}/renart/syft"
install_dir="${cache_root}/${version}"
syft="${install_dir}/syft"

installed_version() {
	"${syft}" version | sed -n 's/^Version:[[:space:]]*//p'
}

if [ -x "${syft}" ] && [ "$(installed_version)" = "${version}" ]; then
	printf '%s\n' "${syft}"
	exit 0
fi

for command in curl sha256sum tar; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		echo "${command} is required to install the pinned Syft release tool" >&2
		exit 1
	fi
done

mkdir -p "${cache_root}"
workdir="$(mktemp -d "${cache_root}/install.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT
archive="${workdir}/syft.tar.gz"
url="https://github.com/anchore/syft/releases/download/v${version}/syft_${version}_linux_${archive_arch}.tar.gz"

curl --fail --location --proto '=https' --tlsv1.2 --output "${archive}" "${url}"
printf '%s  %s\n' "${checksum}" "${archive}" | sha256sum --check --status
mkdir -p "${install_dir}"
tar -xzf "${archive}" -C "${install_dir}" syft
chmod 0755 "${syft}"

test "$(installed_version)" = "${version}"
printf '%s\n' "${syft}"

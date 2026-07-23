#!/usr/bin/env bash

set -euo pipefail

dist="${1:-dist}"
dist="$(cd "${dist}" && pwd)"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

require_file() {
	local root="$1"
	local filename="$2"
	local archive_name="$3"
	local description="$4"
	local result
	result="$(find "${root}" -type f -name "${filename}" -print -quit)"
	if [ -z "${result}" ]; then
		echo "${archive_name} does not contain ${description} (${filename})" >&2
		exit 1
	fi
	printf '%s\n' "${result}"
}

assert_go_helper() {
	local helper="$1"
	local archive_name="$2"
	if ! go version -m "${helper}" | grep -F $'path\trenart/cmd/renart-gui' >/dev/null; then
		echo "${archive_name} contains an invalid standalone helper: ${helper}" >&2
		exit 1
	fi
}

assert_glibc_ceiling() {
	local binary="$1"
	local ceiling="$2"
	local label="$3"
	local max_glibc
	max_glibc="$(readelf --version-info "${binary}" | grep -Eo 'GLIBC_[0-9]+\.[0-9]+' | sed 's/GLIBC_//' | sort -Vu | tail -1)"
	if [ -z "${max_glibc}" ] || [ "$(printf '%s\n' "${ceiling}" "${max_glibc}" | sort -V | tail -1)" != "${ceiling}" ]; then
		echo "${label} requires unsupported GLIBC_${max_glibc:-unknown}; release maximum is GLIBC_${ceiling}" >&2
		exit 1
	fi
	printf '%s requires at most GLIBC_%s\n' "${label}" "${max_glibc}"
}

mapfile -t archives < <(find "${dist}" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \) -print | sort)
if [ "${#archives[@]}" -ne 5 ]; then
	echo "expected 5 release archives, found ${#archives[@]}" >&2
	printf '  %s\n' "${archives[@]}" >&2
	exit 1
fi

for archive in "${archives[@]}"; do
	name="$(basename "${archive}")"
	target="${workdir}/extracted"
	rm -rf "${target}"
	mkdir -p "${target}"
	if [[ "${archive}" == *.zip ]]; then
		mapfile -t members < <(unzip -Z1 "${archive}")
		unzip -qo "${archive}" -d "${target}"
	else
		mapfile -t members < <(tar -tzf "${archive}")
		tar -xzf "${archive}" -C "${target}"
	fi
	for required in LICENSE README.md THIRD_PARTY_NOTICES.md; do
		if ! printf '%s\n' "${members[@]}" | grep -E "(^|/)${required}$" >/dev/null; then
			echo "${name} does not contain ${required}" >&2
			exit 1
		fi
	done
	if ! printf '%s\n' "${members[@]}" | grep -E '(^|/)third_party/licenses/.+\.txt$' >/dev/null; then
		echo "${name} does not contain the third-party license texts referenced by THIRD_PARTY_NOTICES.md" >&2
		exit 1
	fi

	if [[ "${name}" == *Windows* ]]; then
		binary="$(require_file "${target}" renart.exe "${name}" "the Renart CLI")"
		helper="$(require_file "${target}" renart-gui.exe "${name}" "the standalone helper")"
	else
		binary="$(require_file "${target}" renart "${name}" "the Renart CLI")"
		helper="$(require_file "${target}" renart-gui "${name}" "the standalone helper")"
	fi
	if [ ! -x "${binary}" ] || [ ! -x "${helper}" ]; then
		echo "${name} contains a non-executable CLI or standalone helper" >&2
		exit 1
	fi

	if [[ "${name}" == *Linux* ]]; then
		webkit40="$(require_file "${target}" renart-gui-webkit2_40 "${name}" "the WebKitGTK 4.0 standalone helper")"
		webkit41="$(require_file "${target}" renart-gui-webkit2_41 "${name}" "the WebKitGTK 4.1 standalone helper")"
		assert_go_helper "${webkit40}" "${name}"
		assert_go_helper "${webkit41}" "${name}"
		assert_glibc_ceiling "${webkit40}" "2.31" "${name} WebKitGTK 4.0 helper"
		assert_glibc_ceiling "${webkit41}" "2.34" "${name} WebKitGTK 4.1 helper"
	else
		assert_go_helper "${helper}" "${name}"
	fi

	if [[ "${name}" == *Linux_x86_64* ]]; then
		"${binary}" --version | grep -F 'renart version' >/dev/null
		assert_glibc_ceiling "${binary}" "2.31" "${name} CLI"
	fi
	rm -rf "${target}"
done

if [ ! -f "${dist}/checksums.txt" ]; then
	echo "release checksum file is missing" >&2
	exit 1
fi
(cd "${dist}" && sha256sum --check checksums.txt)

if [ -z "$(find "${dist}" -maxdepth 1 -type f -iname '*sbom*' -print -quit)" ]; then
	echo "release SBOMs are missing" >&2
	exit 1
fi

printf 'Release archive smoke checks passed.\n'

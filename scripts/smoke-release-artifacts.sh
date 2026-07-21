#!/usr/bin/env bash

set -euo pipefail

dist="${1:-dist}"
dist="$(cd "${dist}" && pwd)"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

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

	if [[ "${name}" == *Linux_x86_64* ]]; then
		binary="$(find "${target}" -type f -name renart -print -quit)"
		if [ -z "${binary}" ]; then
			echo "${name} does not contain the Renart binary" >&2
			exit 1
		fi
		"${binary}" --version | grep -F 'renart version' >/dev/null
		max_glibc="$(readelf --version-info "${binary}" | grep -Eo 'GLIBC_[0-9]+\.[0-9]+' | sed 's/GLIBC_//' | sort -Vu | tail -1)"
		if [ -z "${max_glibc}" ] || [ "$(printf '%s\n' "2.31" "${max_glibc}" | sort -V | tail -1)" != "2.31" ]; then
			echo "${name} requires unsupported GLIBC_${max_glibc:-unknown}; release maximum is GLIBC_2.31" >&2
			exit 1
		fi
		printf '%s requires at most GLIBC_%s\n' "${name}" "${max_glibc}"
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

#!/usr/bin/env bash

set -euo pipefail

target_os="${1:?usage: build_standalone_helper.sh <os> <arch> <output-dir>}"
target_arch="${2:?usage: build_standalone_helper.sh <os> <arch> <output-dir>}"
output_dir="${3:?usage: build_standalone_helper.sh <os> <arch> <output-dir>}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "${output_dir}"
output_dir="$(cd "${output_dir}" && pwd)"

host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"
if [ "${host_os}/${host_arch}" != "${target_os}/${target_arch}" ]; then
	echo "standalone helper must be built natively: runner is ${host_os}/${host_arch}, target is ${target_os}/${target_arch}" >&2
	exit 1
fi

tags="standalone,desktop,production"
case "${target_os}" in
linux)
	CGO_ENABLED=1 go build \
		-trimpath \
		-tags "webkit2_41,${tags}" \
		-ldflags "-s -w" \
		-o "${output_dir}/renart-gui-webkit2_41" \
		./cmd/renart-gui

	builder_image="renart-standalone-linux-builder:bullseye"
	docker build \
		--quiet \
		--file "${repo_root}/scripts/standalone-linux.Dockerfile" \
		--tag "${builder_image}" \
		"${repo_root}"

	goroot="$(go env GOROOT)"
	gomodcache="$(go env GOMODCACHE)"
	docker run --rm \
		--user "$(id -u):$(id -g)" \
		-e CGO_ENABLED=1 \
		-e GOCACHE=/tmp/go-build \
		-e GOMODCACHE=/go/pkg/mod \
		-e GOTOOLCHAIN=local \
		-e HOME=/tmp \
		-v "${goroot}:/usr/local/go:ro" \
		-v "${gomodcache}:/go/pkg/mod:ro" \
		-v "${repo_root}:/src:ro" \
		-v "${output_dir}:/out" \
		-w /src \
		"${builder_image}" \
		sh -ceu '
			export PATH=/usr/local/go/bin:$PATH
			go build \
				-trimpath \
				-tags standalone,desktop,production \
				-ldflags "-s -w" \
				-o /out/renart-gui-webkit2_40 \
				./cmd/renart-gui
		'

	cp "${repo_root}/scripts/renart-gui-linux" "${output_dir}/renart-gui"
	chmod +x \
		"${output_dir}/renart-gui" \
		"${output_dir}/renart-gui-webkit2_40" \
		"${output_dir}/renart-gui-webkit2_41"
	helpers=(
		"${output_dir}/renart-gui-webkit2_40"
		"${output_dir}/renart-gui-webkit2_41"
	)
	launcher="${output_dir}/renart-gui"
	;;
darwin)
	CGO_ENABLED=1 go build \
		-trimpath \
		-tags "${tags}" \
		-ldflags "-s -w" \
		-o "${output_dir}/renart-gui" \
		./cmd/renart-gui
	chmod +x "${output_dir}/renart-gui"
	helpers=("${output_dir}/renart-gui")
	launcher="${output_dir}/renart-gui"
	;;
windows)
	CGO_ENABLED=0 go build \
		-trimpath \
		-tags "${tags}" \
		-ldflags "-s -w -H windowsgui" \
		-o "${output_dir}/renart-gui.exe" \
		./cmd/renart-gui
	helpers=("${output_dir}/renart-gui.exe")
	launcher="${output_dir}/renart-gui.exe"
	;;
*)
	echo "unsupported standalone helper target: ${target_os}/${target_arch}" >&2
	exit 1
	;;
esac

for helper in "${helpers[@]}"; do
	go version -m "${helper}" | grep -F $'path\trenart/cmd/renart-gui' >/dev/null
done

"${launcher}" --help >/dev/null 2>&1

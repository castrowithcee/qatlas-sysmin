#!/bin/sh
# Builds the sysmin release assets for linux/amd64 and linux/arm64 into <out> (default: dist):
# one archive per architecture, install.sh and checksums.txt over all of them. The output is
# reproducible: the same source and SOURCE_DATE_EPOCH give byte-identical files.
set -eu

version=${1:-}
output=${2:-dist}

if ! printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'; then
	printf 'release: version must be a SemVer tag such as v0.1.0 or v0.1.0-rc.1\n' >&2
	exit 2
fi

if [ -d "$output" ] && [ -n "$(find "$output" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
	printf 'release: output directory is not empty: %s\n' "$output" >&2
	exit 2
fi

scripts_dir=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$scripts_dir/.." && pwd)
cd "$root"

mkdir -p "$output"
output=$(cd "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

if [ -z "${SOURCE_DATE_EPOCH:-}" ]; then
	SOURCE_DATE_EPOCH=$(git show -s --format=%ct "${version}^{commit}")
fi
export SOURCE_DATE_EPOCH

for goarch in amd64 arm64; do
	stage="$work/stage-$goarch"
	mkdir -p "$stage/bin" "$stage/share/doc/sysmin" "$stage/lib/systemd/system"

	CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/castrowithcee/qatlas-sysmin/internal/cli.version=$version" \
		-o "$stage/bin/sysmin" .
	cp LICENSE "$stage/share/doc/sysmin/LICENSE"
	cp packaging/sysmin.service "$stage/lib/systemd/system/sysmin.service"
	chmod 755 "$stage/bin/sysmin"
	chmod 644 "$stage/share/doc/sysmin/LICENSE" "$stage/lib/systemd/system/sysmin.service"

	# Fixed order, owner and mtime; gzip -n leaves out the name and timestamp.
	(cd "$stage" && LC_ALL=C tar --sort=name --owner=0 --group=0 --numeric-owner \
		--mtime="@$SOURCE_DATE_EPOCH" --format=gnu -cf - .) |
		gzip -n -9 >"$output/sysmin_${version}_linux_${goarch}.tar.gz"
done

# install.sh is a release asset and listed in checksums.txt, so the signature covers it.
cp "$scripts_dir/install.sh" "$output/install.sh"

(cd "$output" && LC_ALL=C sha256sum "sysmin_${version}_linux_amd64.tar.gz" \
	"sysmin_${version}_linux_arm64.tar.gz" install.sh) >"$output/checksums.txt"

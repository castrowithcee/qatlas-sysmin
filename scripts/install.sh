#!/bin/sh
# sysmin installer for Linux hosts with systemd. Run it as root:
#
#   curl -fsSL https://github.com/castrowithcee/qatlas-sysmin/releases/latest/download/install.sh | sudo sh
#   curl -fsSL https://github.com/castrowithcee/qatlas-sysmin/releases/latest/download/install.sh | sudo sh -s -- --version v1.2.3
#
# Options:
#   --version <tag>        install this release (pre-releases such as v1.2.3-rc.1 included) instead of the
#                          newest stable release
#   --auto-unlock <mode>   tpm2 or host: prepare for automatic vault unlocking and print the command that
#                          binds it. tpm2 aborts before any change when the TPM2 is not usable.
#   --release-url <base>   releases URL to download from (default below); it only changes where files come
#                          from, never which key is trusted
#   -h, --help             show this help
#
# The signature of checksums.txt is verified with the release key embedded below, then the archive checksum;
# the system is not touched before both match. Running the script again updates the installation. A fresh
# install through sudo adds the calling user to the group sysmin, whose members may use the service.
#
# SYSMIN_INSTALL_ROOT is for tests only: it is prepended to every path the script writes.
set -eu

repo_url=https://github.com/castrowithcee/qatlas-sysmin/releases
signer_line='sysmin-release ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH4YQ6+OBRhnnWWv4rQmjq5QRBDe0V08Ym46RxRMRWuz'
namespace=sysmin-release
service_user=sysmin

fail() {
	printf 'sysmin-install: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
usage: install.sh [--version <tag>] [--auto-unlock tpm2|host] [--release-url <base>]

  --version <tag>        install this release instead of the newest stable one
  --auto-unlock <mode>   tpm2 or host: prepare automatic vault unlocking and print the bind command
  --release-url <base>   releases URL to download from
USAGE
}

main() {
	version=
	unlock=
	base=$repo_url
	while [ $# -gt 0 ]; do
		case $1 in
		--version | --auto-unlock | --release-url)
			[ $# -ge 2 ] || fail "$1 needs a value"
			case $1 in
			--version) version=$2 ;;
			--auto-unlock) unlock=$2 ;;
			--release-url) base=$2 ;;
			esac
			shift 2
			;;
		--version=*) version=${1#*=}; shift ;;
		--auto-unlock=*) unlock=${1#*=}; shift ;;
		--release-url=*) base=${1#*=}; shift ;;
		-h | --help) usage; exit 0 ;;
		*) fail "unknown argument '$1'" ;;
		esac
	done
	base=${base%/}
	root=${SYSMIN_INSTALL_ROOT:-}
	root=${root%/}

	case $unlock in
	'' | tpm2 | host) ;;
	*) fail "invalid --auto-unlock '$unlock'; expected tpm2 or host" ;;
	esac
	if [ -n "$version" ] && ! printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'; then
		fail "invalid version '$version'; expected a tag such as v1.2.3 or v1.2.3-rc.1"
	fi

	# Everything below up to the verification only checks; nothing on the system is changed.
	[ "$(id -u)" = 0 ] || fail 'this installer must run as root, for example with sudo'
	[ "$(uname -s)" = Linux ] || fail 'sysmin supports Linux only'
	for tool in curl tar gzip awk mktemp ssh-keygen sha256sum systemctl; do
		command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
	done
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "unsupported architecture $(uname -m); supported: amd64, arm64" ;;
	esac
	getent group docker >/dev/null 2>&1 ||
		fail 'the group docker does not exist; install Docker first, the service needs access to it'
	if [ -n "$unlock" ]; then
		command -v systemd-creds >/dev/null 2>&1 || fail 'systemd-creds is required for --auto-unlock'
		if [ "$unlock" = tpm2 ]; then
			[ "$(systemd-analyze has-tpm2 2>/dev/null || true)" = yes ] ||
				fail 'no usable TPM2 on this host; use --auto-unlock host or unlock the vault by hand'
		fi
	fi

	if [ -z "$version" ]; then
		latest=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "$base/latest") || fail 'could not determine the latest release'
		version=${latest##*/}
		printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$' ||
			fail "the latest release has an unexpected tag '$version'"
	fi

	archive="sysmin_${version}_linux_${arch}.tar.gz"
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/sysmin-install.XXXXXX")
	cleanup() {
		rm -rf "$tmp"
		[ -z "${staged:-}" ] || rm -f "$staged"
	}
	staged=
	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM

	printf 'Downloading sysmin %s for linux/%s\n' "$version" "$arch"
	for name in "$archive" checksums.txt checksums.txt.sig; do
		curl -fsSL -o "$tmp/$name" "$base/download/$version/$name" || fail "download of $name failed"
	done

	printf '%s\n' "$signer_line" >"$tmp/allowed-signers"
	ssh-keygen -Y verify -f "$tmp/allowed-signers" -I "$namespace" -n "$namespace" \
		-s "$tmp/checksums.txt.sig" <"$tmp/checksums.txt" >/dev/null 2>&1 ||
		fail 'signature of checksums.txt is invalid; nothing was installed'

	want=$(awk -v n="$archive" '{ f = $2; sub(/^\*/, "", f) } f == n { print tolower($1); exit }' "$tmp/checksums.txt")
	[ -n "$want" ] || fail "checksums.txt does not list $archive; nothing was installed"
	got=$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)
	[ "$got" = "$want" ] || fail "SHA-256 mismatch for $archive; nothing was installed"

	mkdir "$tmp/extract"
	tar -xzf "$tmp/$archive" -C "$tmp/extract" --no-same-owner || fail 'could not extract the archive; nothing was installed'
	for file in bin/sysmin lib/systemd/system/sysmin.service; do
		[ -f "$tmp/extract/$file" ] && [ ! -L "$tmp/extract/$file" ] ||
			fail "archive does not contain $file; nothing was installed"
	done

	# Verified: from here on the system is changed.
	bin_dir=$root/usr/local/bin
	unit_dir=$root/etc/systemd/system
	config_dir=$root/etc/sysmin
	state_dir=$root/var/lib/sysmin
	if [ -f "$unit_dir/sysmin.service" ]; then fresh=; else fresh=1; fi

	if ! getent group "$service_user" >/dev/null 2>&1; then
		groupadd --system "$service_user"
	fi
	if id "$service_user" >/dev/null 2>&1; then
		id -nG "$service_user" | tr ' ' '\n' | grep -qx docker || usermod -aG docker "$service_user"
	else
		useradd --system --gid "$service_user" --groups docker --no-create-home \
			--home-dir /var/lib/sysmin --shell "$(nologin_shell)" "$service_user"
	fi
	# Whoever can run the installer through sudo can become root anyway. Only a fresh install adds them, so an
	# update never undoes a deliberate removal.
	operator=
	if [ -n "$fresh" ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ] && id "$SUDO_USER" >/dev/null 2>&1; then
		usermod -aG "$service_user" "$SUDO_USER"
		operator=$SUDO_USER
	fi

	mkdir -p "$config_dir" "$state_dir" "$bin_dir" "$unit_dir"
	chown "$service_user:$service_user" "$config_dir" "$state_dir"
	chmod 0750 "$config_dir"
	chmod 0700 "$state_dir"

	# Temporary files sit next to their targets, so every mv is an atomic rename; the unit goes first so a
	# running service never meets a new binary with an old unit.
	place "$tmp/extract/lib/systemd/system/sysmin.service" "$unit_dir/sysmin.service" 0644
	place "$tmp/extract/bin/sysmin" "$bin_dir/sysmin" 0755

	systemctl daemon-reload
	if [ -n "$fresh" ]; then
		systemctl enable --now sysmin
	else
		systemctl enable sysmin
		systemctl restart sysmin
	fi

	printf '\nInstalled sysmin %s\n  program: /usr/local/bin/sysmin\n  service: sysmin.service (user %s)\n' "$version" "$service_user"
	if [ -n "$operator" ]; then
		printf '\nAdded %s to the group %s, which may use sysmin. Sessions that started before take it up at the\n' \
			"$operator" "$service_user"
		printf 'next login; until then sysmin tui offers to restart itself with sudo -g %s.\n' "$service_user"
	fi
	if [ -n "$unlock" ]; then
		printf '\nAutomatic vault unlocking (%s): once the vault exists, bind it with\n  sudo sysmin vault bind\n' "$unlock"
		printf 'It asks for the vault passphrase and seals to the TPM2 when usable, otherwise to the host key.\n'
	fi
}

# nologin_shell prints the first existing shell that refuses logins.
nologin_shell() {
	for shell in /usr/sbin/nologin /sbin/nologin /usr/bin/nologin; do
		if [ -x "$shell" ]; then
			printf '%s\n' "$shell"
			return
		fi
	done
	printf '/bin/false\n'
}

# place installs src as dst with root ownership and the given mode through a temporary file in dst's directory.
place() {
	src=$1 dst=$2 mode=$3
	staged=$(mktemp "$(dirname "$dst")/.sysmin-stage.XXXXXX")
	cp "$src" "$staged"
	chown root:root "$staged"
	chmod "$mode" "$staged"
	mv -f "$staged" "$dst"
	staged=
}

main "$@"

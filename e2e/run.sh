#!/bin/sh
# End-to-end environment: a simulated Linux host (systemd, no Docker) with a local release server, where qsysmin
# is installed from locally signed test releases.
#
#   e2e/run.sh up                 build everything, start the environment, install qsysmin v0.7.0 on the host
#   e2e/run.sh down               remove every container, network, volume, image and temp directory it made
#   e2e/run.sh status             show the environment and the installation
#   e2e/run.sh exec [-u user] <command...>   run a command on the simulated host (default user e2e-admin)
#   e2e/run.sh env                print the path of the file with the generated test values (source it)
#   e2e/run.sh scenario           run the acceptance scenario once after up (see e2e/scenario.sh)
#
# Everything is named qsysmine2e-* and labelled qsysmin.e2e=1; down touches only what carries both. Nothing is
# published on the machine's ports. Set E2E_KEEP_ON_FAIL=1 to keep a failed environment for inspection.
# Needs docker (rootless is fine), go, openssl, ssh-keygen, tar, gzip, sha256sum.
set -eu

prefix=qsysmine2e
label=qsysmin.e2e=1
domain=qsysmine2e.test
hostname_=qsysmin-e2e-host
admin=e2e-admin
net=$prefix-net
host=$prefix-host
module=github.com/castrowithcee/qatlas-sysmin
versions='v0.7.0 v0.8.0'
install_version=v0.7.0

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
tmp_base=${TMPDIR:-/tmp}

fail() {
	printf 'e2e: %s\n' "$*" >&2
	exit 1
}

log() {
	printf 'e2e: %s\n' "$*"
}

# owned lists "<id> <name>" of the objects of one kind whose name starts with the prefix and that carry the label.
owned() {
	case $1 in
	container) docker ps -a --filter "label=$label" --format '{{.ID}} {{.Names}}' ;;
	network) docker network ls --filter "label=$label" --format '{{.ID}} {{.Name}}' ;;
	volume) docker volume ls --filter "label=$label" --format '{{.Name}} {{.Name}}' ;;
	image) docker images --filter "label=$label" --format '{{.ID}} {{.Repository}}:{{.Tag}}' ;;
	esac | awk -v p="$prefix-" 'index($2, p) == 1'
}

# named lists the objects of one kind by prefix alone, to find leftovers that lost their label.
named() {
	case $1 in
	container) docker ps -a --format '{{.Names}}' ;;
	network) docker network ls --format '{{.Name}}' ;;
	volume) docker volume ls --format '{{.Name}}' ;;
	image) docker images --format '{{.Repository}}:{{.Tag}}' ;;
	esac | awk -v p="$prefix" 'index($0, p) == 1'
}

workdirs() {
	for dir in "$tmp_base"/$prefix.*; do
		[ -d "$dir" ] && [ -O "$dir" ] && [ -f "$dir/.$prefix" ] && printf '%s\n' "$dir"
	done
	return 0
}

current_work() {
	workdirs | head -n 1
}

require_up() {
	work=$(current_work)
	[ -n "$work" ] || fail 'the environment is not up; run: e2e/run.sh up'
}

down() {
	log 'removing containers, networks, volumes, images and temp directories'
	owned container | while read -r id name; do
		docker rm -f -v "$id" >/dev/null && log "removed container $name"
	done
	owned network | while read -r id name; do
		docker network rm "$id" >/dev/null && log "removed network $name"
	done
	owned volume | while read -r id name; do
		docker volume rm "$id" >/dev/null && log "removed volume $name"
	done
	# Removed by tag, not by ID: only our tag goes, never an image that happens to share content.
	owned image | while read -r _ name; do
		docker rmi "$name" >/dev/null && log "removed image $name"
	done
	workdirs | while read -r dir; do
		rm -rf "$dir" && log "removed $dir"
	done

	left=
	for kind in container network volume image; do
		found=$(named "$kind")
		if [ -n "$found" ]; then
			left="$left $kind:$(printf '%s' "$found" | tr '\n' ',')"
		fi
	done
	[ -z "$(workdirs)" ] || left="$left tmp-dir"
	[ -z "$left" ] || fail "leftovers with the prefix $prefix:$left"
	log "clean: nothing with the prefix $prefix remains"
}

# sh_on_host runs a shell snippet on the simulated host as the given user.
sh_on_host() {
	user=$1
	shift
	docker exec -i -u "$user" "$host" sh -c "$*"
}

make_keys_and_certs() {
	mkdir -p "$work/keys" "$work/ctx/certs"
	ssh-keygen -q -t ed25519 -N '' -C "$prefix-test-release" -f "$work/keys/release"
	pub=$(cut -d ' ' -f 1,2 "$work/keys/release.pub")

	ca=$work/ctx/certs
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 2 \
		-subj "/CN=$prefix test CA" -keyout "$work/keys/ca.key" -out "$ca/ca.crt" 2>/dev/null
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
		-subj "/CN=$prefix release server" -keyout "$ca/server.key" -out "$work/keys/server.csr" 2>/dev/null
	cat >"$work/keys/server.ext" <<EXT
subjectAltName = DNS:releases.$domain, DNS:api.github.com, DNS:github.com
extendedKeyUsage = serverAuth
EXT
	openssl x509 -req -in "$work/keys/server.csr" -CA "$ca/ca.crt" -CAkey "$work/keys/ca.key" -CAcreateserial \
		-days 2 -extfile "$work/keys/server.ext" -out "$ca/server.crt" 2>/dev/null
}

write_testenv() {
	cat >"$work/testenv" <<ENV
E2E_WORK=$work
E2E_HOST_CONTAINER=$host
E2E_HOST_ADMIN=$admin
E2E_HOSTNAME=$hostname_
E2E_SIGNING_KEY=$work/keys/release
E2E_CA_CERT=$work/ctx/certs/ca.crt
E2E_VERSIONS='$versions'
E2E_INSTALL_URL=https://releases.$domain/install.sh
E2E_RELEASES_URL=https://releases.$domain/releases
E2E_GITHUB_API=https://api.github.com
ENV
}

# build_releases builds the test releases for the host architecture in the layout of scripts/release.sh and
# signs each checksums.txt with the run's test key.
build_releases() {
	case $(uname -m) in
	x86_64 | amd64) goarch=amd64 ;;
	arm64 | aarch64) goarch=arm64 ;;
	*) fail "unsupported architecture $(uname -m)" ;;
	esac
	rel=$work/ctx/releases
	mkdir -p "$rel"

	# The installer copy trusts the test key; scripts/install.sh itself stays untouched.
	sed "s|^signer_line=.*|signer_line='qsysmin-release $pub'|" "$root/scripts/install.sh" >"$rel/install.sh"
	grep -q "^signer_line='qsysmin-release $pub'\$" "$rel/install.sh" || fail 'could not set the test signer in the installer copy'
	[ "$(grep -c '^signer_line=' "$rel/install.sh")" = 1 ] || fail 'the installer has more than one signer_line'

	epoch=1767225600
	for v in $versions; do
		stage=$work/stage-$v
		out=$rel/$v
		mkdir -p "$stage/bin" "$stage/share/doc/qsysmin" "$stage/share/man/man1" "$stage/lib/systemd/system" "$out"
		log "building qsysmin $v (linux/$goarch)"
		(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=$goarch go build -trimpath -buildvcs=false \
			-ldflags "-s -w -X $module/internal/cli.version=$v -X '$module/internal/release.extraKeyLines=$pub'" \
			-o "$stage/bin/qsysmin" .)
		(cd "$root" && SOURCE_DATE_EPOCH=$epoch go run -ldflags "-X $module/internal/cli.version=$v" scripts/gen_man.go "$stage/share/man/man1")
		cp "$root/LICENSE" "$stage/share/doc/qsysmin/LICENSE"
		cp "$root/packaging/qsysmin.service" "$stage/lib/systemd/system/qsysmin.service"
		chmod 755 "$stage/bin/qsysmin"
		chmod 644 "$stage/share/doc/qsysmin/LICENSE" "$stage/share/man/man1/qsysmin.1" "$stage/lib/systemd/system/qsysmin.service"
		(cd "$stage" && LC_ALL=C tar --sort=name --owner=0 --group=0 --numeric-owner \
			--mtime="@$epoch" --format=gnu -cf - .) | gzip -n -9 >"$out/qsysmin_${v}_linux_${goarch}.tar.gz"
		cp "$rel/install.sh" "$out/install.sh"
		(cd "$out" && LC_ALL=C sha256sum "qsysmin_${v}_linux_${goarch}.tar.gz" install.sh >checksums.txt &&
			ssh-keygen -q -Y sign -f "$work/keys/release" -n qsysmin-release checksums.txt)
		[ -f "$out/checksums.txt.sig" ] || fail "signing $v failed"
		rm -rf "$stage"
	done
}

build_images() {
	log 'building the release server'
	(cd "$root" && CGO_ENABLED=0 go build -tags e2efakes -trimpath -o "$work/ctx/fakes" ./e2e/fakes)

	log 'building the images (the simulated host takes a while on the first run)'
	docker build -q --label "$label" -t "$prefix-fakes:latest" -f "$here/fakes.Dockerfile" "$work/ctx" >/dev/null
	docker build -q --label "$label" -t "$prefix-host:latest" - <"$here/host.Dockerfile" >/dev/null
}

start_services() {
	docker network create --label "$label" --label "qsysmin.e2e.dir=$work" "$net" >/dev/null

	docker run -d --name "$prefix-releases" --label "$label" --network "$net" \
		--network-alias "releases.$domain" --network-alias api.github.com --network-alias github.com \
		"$prefix-fakes:latest" releases >/dev/null

	docker run -d --name "$host" --label "$label" --hostname "$hostname_" --network "$net" \
		--privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock \
		"$prefix-host:latest" >/dev/null
}

wait_for() {
	what=$1 tries=$2
	shift 2
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt "$tries" ] || fail "timeout waiting for $what"
		sleep 1
	done
}

host_booted() {
	state=$(docker exec "$host" systemctl is-system-running 2>/dev/null || true)
	[ "$state" = running ] || [ "$state" = degraded ]
}

prepare_host() {
	wait_for 'the host to boot' 90 host_booted
	docker cp "$work/ctx/certs/ca.crt" "$host:/usr/local/share/ca-certificates/$prefix-ca.crt"
	docker exec "$host" update-ca-certificates >/dev/null
}

check_services() {
	log 'checking the release server from the host'
	api=$(docker exec "$host" curl -fsS https://api.github.com/repos/castrowithcee/qatlas-sysmin/releases/latest) ||
		fail 'the releases fake is not reachable over trusted HTTPS'
	printf '%s' "$api" | grep -q '"tag_name":"v0.8.0"' || fail 'the releases fake does not offer v0.8.0 as the latest release'
}

install_qsysmin() {
	log "installing qsysmin $install_version on the host with install.sh"
	sh_on_host "$admin" "curl -fsSL https://releases.$domain/install.sh | sudo sh -s -- --release-url https://releases.$domain/releases --version $install_version"
	wait_for 'the qsysmin service' 30 docker exec "$host" systemctl is-active --quiet qsysmin
}

verify_install() {
	[ "$(docker exec "$host" systemctl is-active qsysmin)" = active ] || fail 'qsysmin.service is not active'
	docker exec "$host" id qsysmin >/dev/null || fail 'the service user qsysmin does not exist'
	got=$(docker exec "$host" qsysmin version) || fail 'qsysmin version failed'
	printf '%s\n' "$got" | grep -q "$install_version" || fail "qsysmin version does not show $install_version: $got"
	log "service: active; user: $(docker exec "$host" id qsysmin); version: $got"
}

up() {
	for tool in docker go openssl ssh-keygen tar gzip sha256sum; do
		command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
	done
	[ -z "$(named container)$(named network)$(named volume)$(named image)$(workdirs)" ] ||
		fail "something with the prefix $prefix exists already; run: e2e/run.sh down"

	work=$(mktemp -d "$tmp_base/$prefix.XXXXXX")
	: >"$work/.$prefix"
	done_=
	trap 'if [ -z "$done_" ]; then
		if [ -n "${E2E_KEEP_ON_FAIL:-}" ]; then printf "e2e: up failed; environment kept, remove it with: e2e/run.sh down\n" >&2
		else printf "e2e: up failed; tearing down\n" >&2; down || true; fi
	fi' EXIT
	trap 'exit 1' HUP INT TERM

	make_keys_and_certs
	write_testenv
	build_releases
	build_images
	start_services
	prepare_host
	check_services
	install_qsysmin
	verify_install
	done_=1
	log "up; test values: $work/testenv"
}

status() {
	require_up
	owned container | while read -r _ name; do
		printf '%s %s\n' "$name" "$(docker inspect -f '{{.State.Status}}' "$name")"
	done
	printf 'qsysmin.service: %s\n' "$(docker exec "$host" systemctl is-active qsysmin 2>/dev/null || true)"
	printf 'qsysmin version: %s\n' "$(docker exec "$host" qsysmin version 2>/dev/null || true)"
	printf 'test values: %s\n' "$work/testenv"
}

main() {
	cmd=${1:-}
	[ $# -eq 0 ] || shift
	case $cmd in
	up) up ;;
	down) down ;;
	status) status ;;
	env)
		require_up
		printf '%s\n' "$work/testenv"
		;;
	scenario)
		require_up
		. "$here/scenario.sh"
		run_scenario
		;;
	exec)
		require_up
		user=$admin
		if [ "${1:-}" = -u ]; then
			[ $# -ge 2 ] || fail 'exec -u needs a user'
			user=$2
			shift 2
		fi
		[ $# -gt 0 ] || fail 'exec needs a command'
		if [ -t 0 ]; then
			exec docker exec -it -u "$user" "$host" "$@"
		fi
		exec docker exec -i -u "$user" "$host" "$@"
		;;
	*)
		sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
		[ -z "$cmd" ] || exit 2
		;;
	esac
}

main "$@"

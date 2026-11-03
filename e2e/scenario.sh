# Acceptance scenario of the end-to-end environment, sourced by run.sh after up. It drives the installed sysmin
# on the simulated host through its command line and writes one record per step to $work/scenario/results.md:
# the commands, the evidence and a verdict.
#
# Verdicts: PASS, DEVIATION (sysmin behaves other than expected; the record names the expectation, what
# happened and any workaround that let the scenario go on without changing what the step tests) and FAIL.
#
# A step is a function named in scenario_steps. It records commands with the h* helpers and findings with
# evidence, deviation, workaround and failed, and returns non-zero only when it cannot go on. Later steps are
# appended to scenario_steps.

scenario_steps='
s01_status_and_log|Status and the verified action log right after the installation
s02_folder_change_kept|A folder change with a running demo container that stays healthy is kept
s03_folder_change_restored|A folder change that makes the demo container unhealthy is restored
s04_folder_undo|The kept folder change is undone from the journal
s05_self_update|Update to the newer signed release
s06_uninstall|Uninstall keeps the configuration and the running containers
'

demo_dir=/srv/demo-app
demo_container=demo-app
demo_image=demo-app:e2e
demo_uid=4242
journal_kept=

# --- record ---------------------------------------------------------------------------------------------

sc_note() {
	printf '%s\n' "$*" | sed '1s/^/    /; 2,$s/^/      /' >>"$step_dir/commands"
}

evidence() {
	printf '%s\n' "$*" | sed '1s/^/    - /; 2,$s/^/      /' >>"$step_dir/evidence"
}

deviation() {
	: >"$step_dir/deviation"
	evidence "DEVIATION: $*"
}

workaround() {
	evidence "WORKAROUND: $*"
}

failed() {
	: >"$step_dir/failed"
	evidence "FAIL: $*"
}

# --- host access ------------------------------------------------------------------------------------------

# hrun runs a command on the host as the administrator and records it.
hrun() {
	sc_note "\$ $*"
	docker exec -u "$admin" "$host" "$@"
}

# hroot runs a shell snippet on the host as root and records it; for the scenario's own preparation.
hroot() {
	sc_note "# $1"
	docker exec -i "$host" sh -c "$1"
}

cli() {
	hrun sudo sysmin "$@"
}

# cliq is cli without a record, for polling.
cliq() {
	docker exec -u "$admin" "$host" sudo sysmin "$@"
}

# kv prints the value of a key=value line of compact output.
kv() {
	printf '%s\n' "$1" | sed -n "s/^$2=//p" | head -n 1
}

# poll waits up to $1 seconds for a command to succeed.
poll() {
	poll_end=$(($(date +%s) + $1))
	shift
	until "$@" >/dev/null 2>&1; do
		[ "$(date +%s)" -lt "$poll_end" ] || return 1
		sleep 1
	done
}

# containers prints name, id and start time of the running containers on the host.
containers() {
	docker exec "$host" docker ps --no-trunc --format '{{.Names}} {{.ID}}' | sort | while read -r name id; do
		printf '%s %s %s\n' "$name" "$id" "$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' "$id")"
	done
}

# --- demo container ---------------------------------------------------------------------------------------

# stat_of prints owner, group and mode of the demo folder.
stat_of() {
	docker exec "$host" stat -c '%U:%G %a' "$demo_dir"
}

demo_health() {
	docker exec "$host" docker inspect -f '{{.State.Status}}/{{.State.Health.Status}}' "$demo_container" 2>&1
}

demo_healthy() {
	[ "$(demo_health)" = running/healthy ]
}

# demo_start builds the demo image from the fake application and runs a container that reads the demo folder
# as an unprivileged user, so that the folder's permissions decide whether it stays healthy.
demo_start() {
	hroot "useradd --system --no-create-home --shell /usr/sbin/nologin demo-user
mkdir -p $demo_dir /root/demo-image && echo ok >$demo_dir/data.txt && chmod 755 $demo_dir && chmod 644 $demo_dir/data.txt"
	cat >"$sc_dir/demo.Dockerfile" <<'EOF'
FROM scratch
COPY demoapp /demoapp
ENTRYPOINT ["/demoapp", "demoapp"]
HEALTHCHECK --interval=1s --timeout=3s --retries=1 CMD ["/demoapp", "demoapp", "check"]
EOF
	sc_note "# docker cp demo.Dockerfile and the fake application to $host:/root/demo-image"
	docker cp "$work/ctx/fakes" "$host:/root/demo-image/demoapp"
	docker cp "$sc_dir/demo.Dockerfile" "$host:/root/demo-image/Dockerfile"
	hroot "docker build -q -t $demo_image /root/demo-image >/dev/null
docker run -d --name $demo_container --user $demo_uid:$demo_uid -v $demo_dir:/data:ro -e DEMO_DIR=/data $demo_image >/dev/null"
	poll 60 demo_healthy
}

# apply_folder runs the folder change with the given levels (user, group, other) on the demo folder.
apply_folder() {
	cli host apply folder.set-permissions "$demo_dir" demo-user demo-user "$1" "$2" "$3" --yes --output compact 2>&1
}

# --- steps ------------------------------------------------------------------------------------------------

s01_status_and_log() {
	out=$(cli status --output compact 2>&1) && rc=0 || rc=$?
	evidence "status: exit $rc: version=$(kv "$out" version) socket=$(kv "$out" socket)"
	[ "$rc" = 0 ] && [ "$(kv "$out" version)" = v0.1.0 ] || failed "status: $out"
	out=$(cli log verify --output compact 2>&1) && rc=0 || rc=$?
	evidence "log verify: exit $rc:
$out"
	[ "$rc" = 0 ] || failed 'the action log does not verify'
}

s02_folder_change_kept() {
	if ! demo_start; then
		failed "the demo container did not become healthy: $(demo_health)"
		return 1
	fi
	before=$(stat_of)
	evidence "demo folder before: $before; container: $(demo_health)"
	out=$(apply_folder read-write read read) && rc=0 || rc=$?
	journal_kept=$(kv "$out" journal_id)
	evidence "host apply (the folder stays readable for the container): exit $rc: result=$(kv "$out" result) check=$(kv "$out" check) restored=$(kv "$out" restored) reversible=$(kv "$out" reversible) journal_id=$journal_kept"
	[ "$rc" = 0 ] && [ "$(kv "$out" result)" = applied ] || failed "the change was not applied: $out"
	after=$(stat_of)
	evidence "demo folder after: $after; container: $(demo_health)"
	[ "$after" = 'demo-user:demo-user 755' ] || failed "the folder is not demo-user:demo-user 755: $after"
	demo_healthy || failed 'the demo container is not healthy after the change'
}

s03_folder_change_restored() {
	before=$(stat_of)
	id_before=$(docker exec "$host" docker inspect -f '{{.Id}}' "$demo_container")
	out=$(apply_folder read-write read nothing) && rc=0 || rc=$?
	evidence "host apply (the container loses access): exit $rc: result=$(kv "$out" result) check=$(kv "$out" check) restored=$(kv "$out" restored)
check_error=$(kv "$out" check_error)"
	[ "$rc" != 0 ] && [ "$(kv "$out" result)" = restored ] || failed "the change was not restored: exit $rc: $out"
	after=$(stat_of)
	evidence "demo folder before: $before; after: $after; container: $(demo_health)"
	[ "$after" = "$before" ] || failed 'the previous folder state was not restored'
	poll 60 demo_healthy || failed 'the demo container is not healthy after the restore'
	[ "$(docker exec "$host" docker inspect -f '{{.Id}}' "$demo_container")" = "$id_before" ] || failed 'the demo container was replaced'
}

s04_folder_undo() {
	out=$(cli host journal --output compact 2>&1) || true
	evidence "host journal:
$out"
	out=$(cli host undo "$journal_kept" --yes --output compact 2>&1) && rc=0 || rc=$?
	evidence "host undo $journal_kept: exit $rc: result=$(kv "$out" result) check=$(kv "$out" check)"
	[ "$rc" = 0 ] && [ "$(kv "$out" result)" = applied ] || failed "undo: $out"
	after=$(stat_of)
	evidence "demo folder after the undo: $after; container: $(demo_health)"
	[ "$after" = 'root:root 755' ] || failed "the folder is not back to root:root 755: $after"
	demo_healthy || failed 'the demo container is not healthy after the undo'
}

s05_self_update() {
	api=${E2E_RELEASES_URL%/releases}/repos/castrowithcee/qatlas-sysmin
	out=$(cli update --list --api-url "$api" --output compact 2>&1) || true
	evidence "update --list:
$out"
	sc_note '# on the release server: v0.2.0/checksums.txt.sig replaced by the signature of v0.1.0'
	docker exec "$prefix-releases" sh -c 'cd /srv/releases && cp v0.2.0/checksums.txt.sig /tmp/v0.2.0.sig && cp v0.1.0/checksums.txt.sig v0.2.0/checksums.txt.sig'
	out=$(cli update --api-url "$api" --to v0.2.0 --yes --output compact 2>&1) && rc=0 || rc=$?
	ver=$(docker exec "$host" sysmin version --output compact | sed 's/^version=//')
	evidence "with the signature of another release: exit $rc: $out; installed: $ver"
	[ "$rc" != 0 ] && [ "$ver" = v0.1.0 ] || failed 'a release with a wrong signature was installed'
	sc_note '# on the release server: the signature of v0.2.0 restored'
	docker exec "$prefix-releases" sh -c 'cp /tmp/v0.2.0.sig /srv/releases/v0.2.0/checksums.txt.sig'
	out=$(cli update --api-url "$api" --yes --output compact 2>&1) && rc=0 || rc=$?
	evidence "update: previous=$(kv "$out" previous) version=$(kv "$out" version) updated=$(kv "$out" updated) restarted=$(kv "$out" restarted)"
	if [ "$rc" != 0 ]; then
		failed "update: $out"
		return 1
	fi
	poll 60 cliq status || true
	ver=$(hrun sysmin version --output compact 2>&1 | sed 's/^version=//')
	evidence "sysmin version: $ver; sysmin.service $(docker exec "$host" systemctl is-active sysmin)"
	[ "$ver" = v0.2.0 ] || failed 'sysmin version does not show v0.2.0'
	[ "$(docker exec "$host" systemctl is-active sysmin)" = active ] || failed 'sysmin.service is not active after the update'
	out=$(cli log verify --output compact 2>&1) && rc=0 || rc=$?
	evidence "log verify after the update: exit $rc"
	[ "$rc" = 0 ] || failed 'the action log does not verify after the update'
	demo_healthy || failed 'the demo container is not healthy after the update'
}

s06_uninstall() {
	before=$(containers)
	out=$(cli uninstall --output compact 2>&1) && rc=0 || rc=$?
	evidence "uninstall without --yes and without a terminal: exit $rc: $out"
	[ "$rc" != 0 ] || failed 'uninstall ran without a confirmation'
	hroot 'cp /usr/local/bin/sysmin /root/sysmin-kept'
	out=$(cli uninstall --yes --output compact 2>&1) && rc=0 || rc=$?
	evidence "uninstall --yes: exit $rc: uninstalled=$(kv "$out" uninstalled) purged=$(kv "$out" purged)"
	[ "$rc" = 0 ] || failed "uninstall: $out"
	gone=$(docker exec "$host" sh -c '
		for f in /usr/local/bin/sysmin /etc/systemd/system/sysmin.service; do
			[ -e "$f" ] && echo "$f present" || echo "$f absent"
		done
		echo "unit: $(systemctl show -p LoadState --value sysmin), service: $(systemctl is-active sysmin)"
		id sysmin >/dev/null 2>&1 && echo "user sysmin present" || echo "user sysmin absent"')
	evidence "after uninstall:
$gone"
	printf '%s\n' "$gone" | grep -qE 'bin/sysmin present|service present|LoadState|unit: loaded|service: active|user sysmin present' &&
		failed 'program, unit, service or user remain'
	kept=$(docker exec "$host" sh -c 'ls -d /etc/sysmin /var/lib/sysmin 2>&1')
	evidence "kept:
$kept"
	printf '%s\n' "$kept" | grep -q '^/var/lib/sysmin$' || failed 'state was removed without --purge'
	after=$(containers)
	evidence "running containers unchanged (name, id, start): $([ "$before" = "$after" ] && echo yes || echo no)"
	[ "$before" = "$after" ] || failed "the containers changed:
$after"
	out=$(hroot '/root/sysmin-kept uninstall --purge --yes --output compact; rm -f /root/sysmin-kept' 2>&1) || true
	left=$(docker exec "$host" sh -c 'ls -d /etc/sysmin /var/lib/sysmin 2>&1')
	evidence "uninstall --purge with a copy of the program kept before: purged=$(kv "$out" purged); what is left:
$left"
	printf '%s\n' "$left" | grep -qE '^/etc/sysmin$|^/var/lib/sysmin$' && failed '--purge left configuration or state'
	after=$(containers)
	evidence "running containers unchanged after --purge: $([ "$before" = "$after" ] && echo yes || echo no)"
	[ "$before" = "$after" ] || failed 'the containers changed after --purge'
}

# --- runner -----------------------------------------------------------------------------------------------

run_step() {
	step_id=$1
	step_dir=$sc_dir/steps/$step_id
	mkdir -p "$step_dir"
	: >"$step_dir/commands"
	: >"$step_dir/evidence"
	log "scenario: $step_id $2"
	"$step_id" || [ -f "$step_dir/failed" ] || failed 'the step stopped early'
	verdict=PASS
	[ ! -f "$step_dir/deviation" ] || verdict=DEVIATION
	[ ! -f "$step_dir/failed" ] || verdict=FAIL
	{
		printf '\n## %s: %s\n\nVerdict: %s\n\nCommands:\n\n' "$step_id" "$2" "$verdict"
		cat "$step_dir/commands"
		printf '\nEvidence:\n\n'
		cat "$step_dir/evidence"
	} >>"$results"
	printf '%-26s %s\n' "$step_id" "$verdict" >>"$sc_dir/summary"
	log "scenario: $step_id $verdict"
}

run_scenario() {
	. "$work/testenv"
	sc_dir=$work/scenario
	[ ! -e "$sc_dir" ] || fail "the scenario ran in this environment already; start again with: e2e/run.sh down && e2e/run.sh up"
	mkdir -p "$sc_dir/steps"
	results=$sc_dir/results.md
	: >"$sc_dir/summary"
	{
		printf '# sysmin end-to-end scenario\n\n'
		printf 'Started: %s\nsysmin: %s\nHost: %s (Debian 12, systemd, Docker %s)\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
			"$(docker exec "$host" sysmin version --output compact | sed 's/^version=//')" "$hostname_" \
			"$(docker exec "$host" docker version --format '{{.Server.Version}}')"
		printf 'Commands starting with $ run on the host as %s, # as root to prepare the scenario.\n' "$admin"
	} >"$results"

	printf '%s\n' "$scenario_steps" | while IFS='|' read -r fn title; do
		[ -n "$fn" ] || continue
		run_step "$fn" "$title" </dev/null
	done

	{
		printf '\n## Summary\n\n'
		sed 's/^/    /' "$sc_dir/summary"
	} >>"$results"
	cat "$results"
	log "scenario record: $results"
	! grep -q ' FAIL$' "$sc_dir/summary" || fail 'at least one step failed'
}

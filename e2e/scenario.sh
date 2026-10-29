# Acceptance scenario of the end-to-end environment, sourced by run.sh after up. It drives the installed sysmin
# on the simulated host through its command line and, over tmux, through its terminal interface, and writes
# one record per step to $work/scenario/results.md: the commands, the evidence and a verdict.
#
# Verdicts: PASS, DEVIATION (sysmin behaves other than expected; the record names the expectation, what
# happened and any workaround that let the scenario go on without changing what the step tests) and FAIL.
#
# A step is a function named in scenario_steps. It records commands with the h* and tui_* helpers and
# findings with evidence, deviation, workaround and failed, and returns non-zero only when it cannot go on.
# Later steps are appended to scenario_steps. Secrets reach sysmin only on stdin or typed into the interface;
# the record shows the variable that holds them, and every file of the record is masked at the end.

scenario_steps='
s01_vault_setup|Vault setup with a passphrase in the terminal interface
s02_image_project|Image project from the command line, pending until approved in the terminal interface
s03_new_tag_deploy|A new tag is deployed automatically
s04_broken_tag_rollback|A tag whose check fails is rolled back and reported
s05_adopt_stack|A running stack with two images and an override file is adopted without a deploy
s06_manual_update|An available update with release notes is applied from the terminal interface
s07_stepwise_major|Major updates go one major version at a time
'

tui=sysmine2e-tui
build_dir=/opt/sysmine2e/build

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

# hfile runs a command on the host as the administrator with a local file on stdin and records it.
hfile() {
	file=$1
	shift
	sc_note "\$ $* < ${file#"$sc_dir/"}"
	docker exec -i -u "$admin" "$host" "$@" <"$file"
}

# hsecret runs a command on the host as the given user with the value of the named variable on stdin.
hsecret() {
	user=$1 var=$2
	shift 2
	sc_note "\$ printf '%s\\n' \"\$$var\" | $*"
	eval "printf '%s\n' \"\${$var}\"" | docker exec -i -u "$user" "$host" "$@"
}

cli() {
	hrun sudo sysmin "$@"
}

# cliq is cli without a record, for polling.
cliq() {
	docker exec -u "$admin" "$host" sudo sysmin "$@"
}

# put copies a local file of the record to the host and gives it to the service user.
put() {
	sc_note "# docker cp ${1#"$sc_dir/"} $host:$2 (owner sysmin)"
	docker cp "$1" "$host:$2" >/dev/null && docker exec "$host" chown sysmin:sysmin "$2"
}

# kv prints the value of a key=value line of compact output.
kv() {
	printf '%s\n' "$1" | sed -n "s/^$2=//p" | head -n 1
}

field() {
	printf '%s\n' "$1" | cut -d '|' -f "$2"
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

# --- terminal interface over tmux -------------------------------------------------------------------------

tmuxh() {
	docker exec -u "$admin" "$host" tmux "$@"
}

tui_screen() {
	tmuxh capture-pane -p -t "$tui"
}

tui_has() {
	tui_screen | grep -qF -- "$1"
}

tui_wait() {
	poll "${2:-15}" tui_has "$1"
}

# tui_keys sends one key at a time, with a pause so that escape is never read as a modifier.
tui_keys() {
	sc_note "tui: keys $*"
	for key in "$@"; do
		tmuxh send-keys -t "$tui" "$key"
		sleep 0.6
	done
}

tui_type() {
	sc_note "tui: type '$1'"
	tmuxh send-keys -t "$tui" -l "$1"
	sleep 0.3
}

# tui_passphrase types the vault passphrase; it travels on stdin, never in an argument list of the machine.
tui_passphrase() {
	sc_note 'tui: type "$E2E_VAULT_PASSPHRASE"'
	printf '%s\n' "$E2E_VAULT_PASSPHRASE" |
		docker exec -i -u "$admin" "$host" sh -c 'IFS= read -r p; tmux send-keys -t "$1" -l "$p"' sh "$tui"
	sleep 0.3
}

tui_save() {
	tui_screen >"$sc_dir/screens/$step_id-$1.txt"
	evidence "screen: screens/$step_id-$1.txt"
}

tui_start() {
	tmuxh kill-session -t "$tui" >/dev/null 2>&1 || true
	sc_note "tui: tmux new-session -d -s $tui -x 160 -y 48 'sudo sysmin tui'"
	tmuxh new-session -d -s "$tui" -x 160 -y 48 'sudo sysmin tui' && tui_wait Overview 20
}

# tui_admin answers a passphrase prompt if the interface shows one.
tui_admin() {
	if tui_has 'Admin passphrase' || tui_has 'Unlock vault'; then
		tui_passphrase
		tui_keys Enter
	fi
}

tui_unlock() {
	tui_keys C-l
	tui_wait 'Unlock vault' || return 1
	tui_passphrase
	tui_keys Enter
	tui_wait 'vault open' 20
}

# tui_approve_all approves every open approval in Access.
tui_approve_all() {
	tui_keys Escape 4
	tui_wait 'Approvals (' || return 1
	! tui_has 'No open approvals.' || return 1
	tui_keys a
	tui_wait 'Approve all' || return 1
	tui_keys y
	sleep 1
	tui_admin
	tui_wait 'No open approvals.' 20
}

# --- demo images and stacks -------------------------------------------------------------------------------

# build_prepare puts the demo application and its Dockerfile on the host and logs the inner Docker in to the
# test registry, so the host can build and push the demo images.
build_prepare() {
	cat >"$sc_dir/files/Dockerfile" <<'EOF'
FROM scratch
COPY demoapp /demoapp
ARG NAME=demo
ARG VERSION=0.0.0
ARG HEALTH=ok
ARG SOURCE=
ENV DEMO_NAME=$NAME DEMO_VERSION=$VERSION DEMO_HEALTH=$HEALTH
LABEL org.opencontainers.image.source=$SOURCE
EXPOSE 8080
ENTRYPOINT ["/demoapp", "demoapp"]
EOF
	hroot "mkdir -p $build_dir" &&
		sc_note "# docker cp <fakes binary> $host:$build_dir/demoapp; docker cp files/Dockerfile $host:$build_dir/" &&
		docker cp "$work/ctx/fakes" "$host:$build_dir/demoapp" >/dev/null &&
		docker cp "$sc_dir/files/Dockerfile" "$host:$build_dir/Dockerfile" >/dev/null &&
		hsecret root E2E_REGISTRY_PASSWORD docker login -u "$E2E_REGISTRY_USER" --password-stdin "$E2E_REGISTRY" >/dev/null 2>&1
}

# push_image NAME VERSION CHANGELOG_REPO [ok|fail] builds registry/demo/NAME:VERSION on the host and pushes it.
push_image() {
	hroot "docker build -q --build-arg NAME=$1 --build-arg VERSION=$2 --build-arg HEALTH=${4:-ok} --build-arg SOURCE=https://github.com/$3 -t $E2E_REGISTRY/demo/$1:$2 $build_dir >/dev/null 2>&1 && docker push -q $E2E_REGISTRY/demo/$1:$2 >/dev/null"
}

# running_tag PROJECT SERVICE prints the tag of the image the service's container runs.
running_tag() {
	docker exec "$host" docker ps --filter "label=com.docker.compose.project=$1" \
		--filter "label=com.docker.compose.service=$2" --format '{{.Image}}' | sed 's/.*://'
}

# stack_state PROJECT lists name, id and start time of the project's containers.
stack_state() {
	hroot "docker ps -q --filter label=com.docker.compose.project=$1 | xargs -r docker inspect -f '{{.Name}} {{.Id}} {{.State.StartedAt}}' | sort"
}

# start_stack DIR PROJECT FILES... copies the files of the record into DIR, starts the stack the way an
# administrator does and hands the directory to the service user, which writes its env file.
start_stack() {
	dir=$1 project=$2
	shift 2
	hroot "mkdir -p $dir" || return 1
	for f in "$@"; do
		put "$sc_dir/files/$project/$f" "$dir/$f" || return 1
	done
	hroot "cd $dir && docker compose -p $project up -d --quiet-pull >/dev/null 2>&1 && chown -R sysmin:sysmin $dir"
}

# last_run PROJECT TAG prints the last history line of the project whose change goes to TAG.
last_run() {
	cliq deploy history "$1" --format compact 2>/dev/null | grep -F -- "-> $2" | tail -n 1
}

has_run() {
	[ -n "$(last_run "$1" "$2")" ]
}

# wait_run PROJECT TAG waits for a finished run to TAG, whoever started it, and prints its history line.
wait_run() {
	sc_note "poll: sysmin deploy history $1 until a run to $2 is listed"
	poll 90 has_run "$1" "$2" || return 1
	last_run "$1" "$2"
}

# run_log PROJECT ID keeps the log file of a run in the record and prints it.
run_log() {
	docker exec "$host" cat "/var/lib/sysmin/deploy/$1/runs/$2.log" >"$sc_dir/logs/$1-$2.log" 2>&1
	cat "$sc_dir/logs/$1-$2.log"
}

hook_has() {
	docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null | grep -F -- "$1" | grep -qF -- "$2"
}

# --- steps ------------------------------------------------------------------------------------------------

s01_vault_setup() {
	if ! tui_start; then
		failed 'the terminal interface did not show the overview'
		return 1
	fi
	tui_save overview
	tui_has 'not set up' || deviation 'the overview does not say that the vault is not set up'
	tui_keys Enter
	if ! tui_wait 'Set up vault'; then
		tui_save no-setup
		failed 'enter on the overview did not open the vault setup'
		return 1
	fi
	tui_passphrase
	tui_keys Tab
	tui_passphrase
	tui_keys Enter
	if ! tui_wait 'vault open' 20; then
		tui_save not-created
		failed 'the vault was not created'
		return 1
	fi
	tui_save created
	out=$(cli status --format compact 2>&1) || true
	evidence "sysmin status: vault=$(kv "$out" vault) unlock=$(kv "$out" unlock)"
	[ "$(kv "$out" vault)" = unlocked ] || failed "the service does not report an unlocked vault: $out"
	tui_has 'admin off' && evidence 'creating the vault opened no admin session (header: admin off)'
}

# settings_workaround saves the admin timeout in Settings › Vault, which is the one action that writes
# sysmin.yaml on a fresh installation, and ends the admin session it opened.
settings_workaround() {
	tui_keys Escape 6
	tui_wait 'Language:' || return 1
	tui_keys Right
	tui_wait 'Admin timeout:' || return 1
	tui_keys Down Enter BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace
	tui_type 30m
	tui_keys Enter F2
	sleep 1
	tui_admin
	tui_wait 'Admin timeout saved.' || return 1
	tui_save settings-saved
	tui_keys C-l
	tui_wait 'admin off'
}

s02_image_project() {
	if ! build_prepare || ! push_image web 1.0.0 "$E2E_CHANGELOG_IMAGE_REPO"; then
		failed 'the demo image could not be built and pushed on the host'
		return 1
	fi
	mkdir -p "$sc_dir/files/demo-web"
	cat >"$sc_dir/files/demo-web/compose.yaml" <<'EOF'
services:
  web:
    image: registry.sysmine2e.test/demo/web:${WEB_TAG}
    ports:
      - "127.0.0.1:18081:8080"
EOF
	printf 'WEB_TAG=1.0.0\n' >"$sc_dir/files/demo-web/.env"
	cat >"$sc_dir/files/demo-web.yaml" <<'EOF'
schema: 1
name: demo-web
dir: /srv/demo-web
template: image-tag
env_file: /srv/demo-web/.env
compose_files:
  - /srv/demo-web/compose.yaml
compose_project: demo-web
checks:
  - type: check-container
    services:
      - web
  - type: check-http
    url: http://127.0.0.1:18081/health
    status: 200
    timeout: 15s
notify:
  channels:
    - e2e-hook
  events:
    - update-succeeded
    - update-failed
images:
  - name: web
    ref: registry.sysmine2e.test/demo/web
    env_var: WEB_TAG
    services:
      - web
    credential: e2e-registry
    version_rule:
      mode: auto
EOF
	if ! start_stack /srv/demo-web demo-web compose.yaml .env; then
		failed 'the demo-web stack did not start'
		return 1
	fi
	evidence "demo-web runs $(running_tag demo-web web) before sysmin knows it"

	out=$(hsecret "$admin" E2E_REGISTRY_PASSWORD sudo sysmin credential set e2e-registry --kind registry \
		--user "$E2E_REGISTRY_USER" --from-stdin --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "credential set: $out"
		return 1
	fi
	evidence "credential set (token on stdin): action=$(kv "$out" action) state=$(kv "$out" state)"

	out=$(cli notify channel add e2e-hook --type webhook --url "$E2E_WEBHOOK_URL" --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ] && printf '%s' "$out" | grep -q 'control: internal' &&
		! docker exec "$host" test -f /etc/sysmin/sysmin.yaml; then
		deviation "expected: a changing command works on a fresh installation; actual: 'sysmin notify channel add' (and likewise 'project add', 'approvals list', 'deploy status') fails with '$out' because /etc/sysmin/sysmin.yaml does not exist; the journal says: open sysmin.yaml: no such file or directory"
		if ! settings_workaround; then
			tui_save settings-failed
			failed 'the admin timeout could not be saved in Settings'
			return 1
		fi
		workaround 'Settings › Vault › Admin timeout set to 30m and saved with F2 in the interface; that writes sysmin.yaml (the admin session it opened was ended with ctrl+l)'
		out=$(cli notify channel add e2e-hook --type webhook --url "$E2E_WEBHOOK_URL" --format compact 2>&1) && rc=0 || rc=$?
	fi
	if [ "$rc" != 0 ]; then
		failed "notify channel add: $out"
		return 1
	fi
	evidence "notify channel add e2e-hook: state=$(kv "$out" state)"

	out=$(hfile "$sc_dir/files/demo-web.yaml" sudo sysmin project add demo-web --file - --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "project add: $out"
		return 1
	fi
	evidence "project add demo-web: state=$(kv "$out" state)"
	[ "$(kv "$out" state)" = 'pending approval' ] || failed 'the change made without an admin session is not pending'

	out=$(cli approvals list --format compact 2>&1) || true
	evidence "approvals list:
$out"
	printf '%s\n' "$out" | grep -q '^demo-web|new|' || failed 'demo-web is not listed as an open approval'
	sleep 3
	out=$(cli deploy history demo-web --format compact 2>&1) || true
	[ "$(printf '%s\n' "$out" | grep -c .)" -le 1 ] || failed "the pending project ran: $out"
	tag=$(running_tag demo-web web)
	env=$(docker exec "$host" cat /srv/demo-web/.env)
	evidence "not carried out while pending: no run in the history, the container runs $tag, .env holds $env"
	[ "$tag" = 1.0.0 ] && [ "$env" = WEB_TAG=1.0.0 ] || failed 'the pending project changed the stack'

	if ! tui_approve_all; then
		tui_save approve-failed
		failed 'approving in Access did not succeed'
		return 1
	fi
	tui_save approved
	out=$(cli project show demo-web --format compact 2>&1) || true
	evidence "after the approval in the interface: project state=$(kv "$out" state); open approvals: $(cliq approvals list --format compact | sed 1d | wc -l)"
	[ "$(kv "$out" state)" = approved ] || failed 'the project is not approved after the approval'
}

# workaround_rw lets the service write below /srv with a unit drop-in, restarts it and unlocks the vault
# again in the interface.
workaround_rw() {
	hroot "mkdir -p /etc/systemd/system/sysmin.service.d && printf '[Service]\nReadWritePaths=/srv\n' >/etc/systemd/system/sysmin.service.d/e2e-project-dirs.conf && systemctl daemon-reload && systemctl restart sysmin" || return 1
	poll 30 cliq status || return 1
	tui_start && tui_unlock
}

workaround_login() {
	hsecret sysmin E2E_REGISTRY_PASSWORD env HOME=/var/lib/sysmin docker login -u "$E2E_REGISTRY_USER" --password-stdin "$E2E_REGISTRY" >/dev/null 2>&1
}

s03_new_tag_deploy() {
	for tag in 1.1.0 1.1.1 1.1.2; do
		if ! push_image web "$tag" "$E2E_CHANGELOG_IMAGE_REPO"; then
			failed "web:$tag could not be pushed"
			return 1
		fi
		out=$(cli deploy run-now demo-web --format compact 2>&1) || true
		evidence "pushed web:$tag; run-now: status=$(kv "$out" status) result=$(kv "$out" result) changes=$(kv "$out" changes)"
		if ! line=$(wait_run demo-web "$tag"); then
			failed "no run to $tag was listed"
			return 1
		fi
		id=$(field "$line" 1) result=$(field "$line" 5) step=$(field "$line" 9) err=$(field "$line" 10)
		log=$(run_log demo-web "$id")
		evidence "run $id: result=$result failed_step=$step; log: logs/demo-web-$id.log"
		if [ "$result" = succeeded ]; then
			now=$(running_tag demo-web web)
			page=$(docker exec "$host" curl -fsS http://127.0.0.1:18081/ 2>&1) || true
			env=$(docker exec "$host" cat /srv/demo-web/.env)
			out=$(cli deploy status demo-web --format compact 2>&1) || true
			evidence "container runs $now, it answers '$page', .env holds $env, deploy status: sources=$(kv "$out" sources) last_run=$(kv "$out" last_run)"
			evidence "run log:
$(printf '%s\n' "$log" | grep -E 'result=|-> ' | sed 's/^[^ ]* //')"
			[ "$now" = "$tag" ] && [ "$page" = "web $tag" ] || failed "the stack does not run $tag after a successful run"
			if poll 20 hook_has deploy.succeeded "$tag"; then
				evidence "the webhook receiver got deploy.succeeded for $tag"
			fi
			return 0
		fi
		case $err in
		*'read-only file system'*)
			deviation "expected: an automatic deploy of $tag; actual: run $id ended with $result, step $step failed: $err. The unit sets ProtectSystem=strict and ReadWritePaths=/etc/sysmin only, so the service cannot write the env file of a project outside /etc/sysmin; a failed first step also left the project paused with rollback-failed"
			if ! workaround_rw; then
				failed 'the unit drop-in could not be applied'
				return 1
			fi
			workaround "drop-in /etc/systemd/system/sysmin.service.d/e2e-project-dirs.conf with ReadWritePaths=/srv, service restarted, vault unlocked with ctrl+l in the interface, project resumed; $tag stays marked as rolled back, so the next attempt uses a new tag"
			;;
		*compose-pull*)
			if ! printf '%s\n' "$log" | grep -q 'no basic auth credentials'; then
				failed "compose-pull failed: $err"
				return 1
			fi
			deviation "expected: the deploy pulls with the registry credential of the image (e2e-registry); actual: run $id ended with $result, compose-pull failed with 'no basic auth credentials': the vault credential is used to list tags, but docker compose pull gets no login; the rollback pulls the previous tag as well and fails the same way, which pauses the project"
			if ! workaround_login; then
				failed 'docker login for the service user failed'
				return 1
			fi
			workaround 'docker login to the test registry as the service user (password on stdin; stored in /var/lib/sysmin/.docker/config.json outside the vault), project resumed, next attempt with a new tag'
			;;
		*)
			failed "unexpected failure: $err"
			return 1
			;;
		esac
		out=$(cli deploy resume demo-web --format compact 2>&1) || true
		evidence "deploy resume: state=$(kv "$out" state)"
	done
	failed 'no deploy succeeded'
	return 1
}

s04_broken_tag_rollback() {
	before=$(running_tag demo-web web)
	if ! push_image web 1.2.0 "$E2E_CHANGELOG_IMAGE_REPO" fail; then
		failed 'web:1.2.0 could not be pushed'
		return 1
	fi
	evidence "pushed web:1.2.0 whose /health answers 500; demo-web runs $before"
	out=$(cli deploy run-now demo-web --format compact 2>&1) || true
	evidence "run-now: status=$(kv "$out" status) result=$(kv "$out" result) failed_step=$(kv "$out" failed_step)"
	if ! line=$(wait_run demo-web 1.2.0); then
		failed 'no run to 1.2.0 was listed'
		return 1
	fi
	id=$(field "$line" 1) result=$(field "$line" 5) step=$(field "$line" 9) err=$(field "$line" 10)
	log=$(run_log demo-web "$id")
	evidence "run $id: result=$result failed_step=$step error=$err; log: logs/demo-web-$id.log"
	evidence "run log:
$(printf '%s\n' "$log" | grep -E 'rollback|result=|-> ' | sed 's/^[^ ]* //')"
	[ "$result" = rolled-back ] || failed "the run ended with $result, not rolled-back"
	case $step in *check-http*) ;; *) failed "the failed step is $step, not the check-http step" ;; esac
	now=$(running_tag demo-web web)
	page=$(docker exec "$host" curl -fsS http://127.0.0.1:18081/ 2>&1) || true
	out=$(cli deploy status demo-web --format compact 2>&1) || true
	evidence "after the run: container runs $now, it answers '$page', deploy status: paused=$(kv "$out" paused) sources=$(kv "$out" sources)"
	[ "$now" = "$before" ] && [ "$page" = "web $before" ] || failed "the stack does not run $before again"
	if poll 20 hook_has deploy.rolled_back 1.2.0; then
		evidence "webhook receiver: $(docker exec "$prefix-hooks" cat /data/requests.jsonl | grep -F deploy.rolled_back | tail -n 1 | sed 's/.*"body":"\(.*\)","headers".*/\1/')"
	else
		failed 'the webhook receiver got no deploy.rolled_back message'
	fi
}

s05_adopt_stack() {
	if ! push_image api 1.0.0 "$E2E_CHANGELOG_IMAGE_REPO" || ! push_image worker 1.0.0 "$E2E_CHANGELOG_GIT_REPO"; then
		failed 'the stack images could not be pushed'
		return 1
	fi
	mkdir -p "$sc_dir/files/demo-shop"
	cat >"$sc_dir/files/demo-shop/compose.yaml" <<'EOF'
services:
  api:
    image: registry.sysmine2e.test/demo/api:${API_TAG}
  worker:
    image: registry.sysmine2e.test/demo/worker:${WORKER_TAG}
EOF
	cat >"$sc_dir/files/demo-shop/compose.override.yaml" <<'EOF'
services:
  api:
    ports:
      - "127.0.0.1:18082:8080"
    environment:
      DEMO_NAME: shop-api
EOF
	printf 'API_TAG=1.0.0\nWORKER_TAG=1.0.0\n' >"$sc_dir/files/demo-shop/.env"
	if ! start_stack /srv/demo-shop demo-shop compose.yaml compose.override.yaml .env; then
		failed 'the demo-shop stack did not start'
		return 1
	fi
	before=$(stack_state demo-shop)
	evidence "running before the adoption (name, id, started):
$before"

	out=$(cli project adopt /srv/demo-shop --dry-run --format compact 2>&1) || true
	evidence "adopt --dry-run:
$out"
	out=$(cli project adopt /srv/demo-shop --format compact 2>&1) && rc=0 || rc=$?
	evidence "adopt:
$out"
	if [ "$rc" != 0 ]; then
		failed 'the adoption was not stored'
		return 1
	fi
	printf '%s\n' "$out" | grep -q '^image|api|registry.sysmine2e.test/demo/api|api|API_TAG|1.0.0|' &&
		printf '%s\n' "$out" | grep -q '^image|worker|registry.sysmine2e.test/demo/worker|worker|WORKER_TAG|1.0.0|' ||
		failed 'the proposal does not read both versions from the env file'
	printf '%s\n' "$out" | grep -q 'compose_files=/srv/demo-shop/compose.yaml,/srv/demo-shop/compose.override.yaml' ||
		failed 'the proposal does not keep the override file'
	file=$(kv "$(cli project show demo-shop --format compact 2>&1)" file)
	modes=$(printf '%s' "$file" | grep -o 'mode: [a-z]*' | sort | uniq -c | tr -s ' ' | tr '\n' ';')
	evidence "stored project: version rule modes:$modes"
	[ "$modes" = ' 2 mode: manual;' ] || failed 'the adopted images are not both in manual mode'

	out=$(cli project edit demo-shop --image-credential api=e2e-registry --image-credential worker=e2e-registry --format compact 2>&1) && rc=0 || rc=$?
	evidence "the registry needs a login, so the adopted images get the credential before the approval: state=$(kv "$out" state)"
	[ "$rc" = 0 ] || failed "project edit: $out"
	if ! tui_approve_all; then
		tui_save approve-failed
		failed 'approving in Access did not succeed'
		return 1
	fi
	tui_save approved
	out=$(cli deploy run-now demo-shop --format compact 2>&1) || true
	evidence "run-now after the approval: status=$(kv "$out" status)"
	sleep 3
	after=$(stack_state demo-shop)
	out=$(cli deploy status demo-shop --format compact 2>&1) || true
	evidence "deploy status: sources=$(kv "$out" sources) last_run=$(kv "$out" last_run)"
	[ "$(kv "$out" sources)" = 'api 1.0.0; worker 1.0.0' ] || failed 'the service does not report the versions from the env file'
	hist=$(cli deploy history demo-shop --format compact 2>&1 | sed 1d)
	evidence "running after the approval and a check:
$after
deploy history: ${hist:-empty}"
	[ -z "$hist" ] || failed 'the adopted project ran'
	[ "$before" = "$after" ] || failed 'containers were recreated or restarted'
}

s06_manual_update() {
	if ! push_image api 1.1.0 "$E2E_CHANGELOG_IMAGE_REPO" || ! push_image api 1.2.0 "$E2E_CHANGELOG_IMAGE_REPO"; then
		failed 'the newer api images could not be pushed'
		return 1
	fi
	out=$(cli deploy run-now demo-shop --format compact 2>&1) || true
	st=$(cli deploy status demo-shop --format compact 2>&1) || true
	evidence "pushed api:1.1.0 and api:1.2.0; run-now: status=$(kv "$out" status); deploy status: sources=$(kv "$st" sources)"
	tui_keys Escape 2
	if ! tui_wait 'Projects ('; then
		failed 'the Projects area did not open'
		return 1
	fi
	n=0
	until tui_screen | grep -qE '│ > +demo-shop '; do
		n=$((n + 1))
		[ "$n" -le 5 ] || break
		tui_keys Down
	done
	tui_save projects
	tui_keys u
	if ! tui_wait 'Release notes demo-shop' 20; then
		tui_save no-notes
		failed 'u did not open the update with release notes'
		return 1
	fi
	tui_save update
	for text in '1.0.0 → 1.2.0' 'Demo image 1.2.0' 'Adds the invented health endpoint.' 'Fixes an invented crash on empty input.'; do
		if tui_has "$text"; then
			evidence "the update screen shows '$text'"
		else
			failed "the update screen does not show '$text'"
		fi
	done
	tui_keys y
	sleep 1
	tui_admin
	if ! tui_wait 'run succeeded' 60; then
		tui_save no-result
		failed 'the update did not report a successful run'
	fi
	tui_save result
	tui_keys Escape
	line=$(last_run demo-shop 1.2.0)
	evidence "history: $line"
	[ "$(field "$line" 2)" = manual ] && [ "$(field "$line" 5)" = succeeded ] || failed 'no successful manual run to 1.2.0'
	api=$(running_tag demo-shop api) worker=$(running_tag demo-shop worker)
	page=$(docker exec "$host" curl -fsS http://127.0.0.1:18082/ 2>&1) || true
	evidence "api runs $api and answers '$page', worker runs $worker"
	[ "$api" = 1.2.0 ] && [ "$page" = 'shop-api 1.2.0' ] && [ "$worker" = 1.0.0 ] || failed 'the stack does not run the chosen versions'
}

stepwise_done() {
	[ "$(running_tag demo-major app)" = 3.0.0 ]
}

s07_stepwise_major() {
	for v in 1.0.0 1.1.0 2.0.0 2.1.0 3.0.0; do
		if ! push_image major "$v" "$E2E_CHANGELOG_GIT_REPO"; then
			failed "major:$v could not be pushed"
			return 1
		fi
	done
	mkdir -p "$sc_dir/files/demo-major"
	cat >"$sc_dir/files/demo-major/compose.yaml" <<'EOF'
services:
  app:
    image: registry.sysmine2e.test/demo/major:${MAJOR_TAG}
EOF
	printf 'MAJOR_TAG=1.0.0\n' >"$sc_dir/files/demo-major/.env"
	cat >"$sc_dir/files/demo-major.yaml" <<'EOF'
schema: 1
name: demo-major
dir: /srv/demo-major
template: image-tag
env_file: /srv/demo-major/.env
compose_files:
  - /srv/demo-major/compose.yaml
compose_project: demo-major
checks:
  - type: check-container
    services:
      - app
images:
  - name: app
    ref: registry.sysmine2e.test/demo/major
    env_var: MAJOR_TAG
    services:
      - app
    credential: e2e-registry
    version_rule:
      mode: auto
      max_update: major
      major_steps: stepwise
EOF
	if ! start_stack /srv/demo-major demo-major compose.yaml .env; then
		failed 'the demo-major stack did not start'
		return 1
	fi
	out=$(hfile "$sc_dir/files/demo-major.yaml" sudo sysmin project add demo-major --file - --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "project add: $out"
		return 1
	fi
	if ! tui_approve_all; then
		tui_save approve-failed
		failed 'approving in Access did not succeed'
		return 1
	fi
	# The source test reads the approved state, so it follows the approval; a check of the service may have
	# deployed the first step meanwhile, which the history below shows as well.
	out=$(cli source test demo-major --format compact 2>&1) || true
	line=$(printf '%s\n' "$out" | grep '^app|')
	evidence "pushed major 1.0.0 1.1.0 2.0.0 2.1.0 3.0.0; source test: current=$(field "$line" 5) available=$(field "$line" 6) target=$(field "$line" 7)"
	if [ "$(field "$line" 5)" = 1.0.0 ] && [ "$(field "$line" 7)" != 1.1.0 ]; then
		failed 'the first target is not the highest version of the current major'
	fi
	n=0
	until stepwise_done; do
		n=$((n + 1))
		[ "$n" -le 6 ] || break
		out=$(cli deploy run-now demo-major --format compact 2>&1) || true
		evidence "run-now: status=$(kv "$out" status) result=$(kv "$out" result) changes=$(kv "$out" changes)"
	done
	runs=$(cli deploy history demo-major --format compact 2>&1 | sed 1d | awk -F '|' '$5 == "succeeded" { print $6 }' | tr '\n' ';')
	evidence "successful runs in order: $runs; app runs $(running_tag demo-major app)"
	[ "$runs" = 'app 1.0.0 -> 1.1.0;app 1.1.0 -> 2.1.0;app 2.1.0 -> 3.0.0;' ] ||
		failed 'the runs do not go 1.0.0 -> 1.1.0 -> 2.1.0 -> 3.0.0'
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

# mask replaces every secret of the test values in the files of the record.
mask() {
	for value in "$E2E_REGISTRY_PASSWORD" "$E2E_GIT_PASSWORD" "$E2E_VAULT_PASSPHRASE"; do
		[ -n "$value" ] || continue
		grep -rlF -- "$value" "$sc_dir" | while read -r f; do
			awk -v s="$value" '{ while ((i = index($0, s)) > 0) $0 = substr($0, 1, i - 1) "***" substr($0, i + length(s)); print }' \
				"$f" >"$f.masked" && mv "$f.masked" "$f"
		done
	done
}

run_scenario() {
	. "$work/testenv"
	if [ -z "${E2E_VAULT_PASSPHRASE:-}" ]; then
		E2E_VAULT_PASSPHRASE=e2e-test-passphrase-$(openssl rand -hex 12)
		printf 'E2E_VAULT_PASSPHRASE=%s\n' "$E2E_VAULT_PASSPHRASE" >>"$work/testenv"
	fi
	sc_dir=$work/scenario
	[ ! -e "$sc_dir" ] || fail "the scenario ran in this environment already; start again with: e2e/run.sh down && e2e/run.sh up"
	mkdir -p "$sc_dir/screens" "$sc_dir/logs" "$sc_dir/files" "$sc_dir/steps"
	results=$sc_dir/results.md
	: >"$sc_dir/summary"
	{
		printf '# sysmin end-to-end scenario\n\n'
		printf 'Started: %s\nsysmin: %s\nHost: %s (Debian 12, systemd, Docker %s)\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
			"$(docker exec "$host" sysmin version --format compact | sed 's/^version=//')" "$hostname_" \
			"$(docker exec "$host" docker version --format '{{.Server.Version}}')"
		printf 'Commands starting with $ run on the host as %s, # as root to prepare the scenario, tui: in the terminal interface.\n' "$admin"
	} >"$results"

	printf '%s\n' "$scenario_steps" | while IFS='|' read -r fn title; do
		[ -n "$fn" ] || continue
		run_step "$fn" "$title" </dev/null
	done

	{
		printf '\n## Summary\n\n'
		sed 's/^/    /' "$sc_dir/summary"
	} >>"$results"
	mask
	cat "$results"
	log "scenario record: $results"
	! grep -q ' FAIL$' "$sc_dir/summary" || fail 'at least one step failed'
}

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
s08_git_project|A git tag project without an image runs its own steps
s09_webhook|A signed webhook starts the check at once, a wrong signature is refused
s10_notifications|The webhook channel received the events without secrets
s11_restart_locked|After a restart the vault is locked, nothing deploys until it is unlocked
s12_restart_auto_unlock|With automatic unlock the vault opens and deploys after a restart
s13_self_update|Update to the newer signed release
s14_uninstall|Uninstall keeps configuration, vault and running stacks
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

# wait_run PROJECT TAG [SECONDS] waits for a finished run to TAG, whoever started it, and prints its history
# line.
wait_run() {
	sc_note "poll: sysmin deploy history $1 until a run to $2 is listed (up to ${3:-90}s)"
	poll "${3:-90}" has_run "$1" "$2" || return 1
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
    restart: unless-stopped
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

s03_new_tag_deploy() {
	tag=1.1.0
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
	now=$(running_tag demo-web web)
	page=$(docker exec "$host" curl -fsS http://127.0.0.1:18081/ 2>&1) || true
	env=$(docker exec "$host" cat /srv/demo-web/.env)
	out=$(cli deploy status demo-web --format compact 2>&1) || true
	evidence "container runs $now, it answers '$page', .env holds $env, deploy status: sources=$(kv "$out" sources) last_run=$(kv "$out" last_run)"
	evidence "run log:
$(printf '%s\n' "$log" | grep -E 'result=|-> |compose-pull' | sed 's/^[^ ]* //')"
	[ "$result" = succeeded ] || failed "the run ended with $result: $err"
	[ "$now" = "$tag" ] && [ "$page" = "web $tag" ] || failed "the stack does not run $tag after the run"
	if poll 20 hook_has deploy.succeeded "$tag"; then
		evidence "the webhook receiver got deploy.succeeded for $tag"
	fi
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
    restart: unless-stopped
  worker:
    image: registry.sysmine2e.test/demo/worker:${WORKER_TAG}
    restart: unless-stopped
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
    restart: unless-stopped
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

# --- git project, webhooks, restarts, update and uninstall ------------------------------------------------

site_dir=/srv/demo-site
hook_url=http://127.0.0.1:8787/hooks/demo-site

# git_release TAG commits site/VERSION with the tag to main on the git server, tags that commit and pushes
# both, the way a developer releases.
git_release() {
	sc_note "# on the git server: commit site/VERSION=$1 to main, tag it $1 and push both"
	docker exec -i "$prefix-git" sh -s "$1" >/dev/null 2>&1 <<'EOF'
set -e
tag=$1 at=@
export GIT_AUTHOR_NAME='E2E Release' GIT_COMMITTER_NAME='E2E Release'
export GIT_AUTHOR_EMAIL="release${at}sysmine2e.test" GIT_COMMITTER_EMAIL="release${at}sysmine2e.test"
umask 022
rm -rf /tmp/release
git clone -q /srv/git/stacks.git /tmp/release
cd /tmp/release
mkdir -p site
printf '%s\n' "$tag" >site/VERSION
printf '/deployed.txt\n' >.gitignore
git add -A
git commit -q -m "Release $tag"
git tag -a "$tag" -m "Release $tag"
git push -q origin main "refs/tags/$tag"
EOF
}

# site_clone clones the repository into the project directory at v1.0.0 the way an administrator does: the
# git password goes from stdin into a credential file that exists only for the clone.
site_clone() {
	sc_note "# printf '%s\\n' \"\$E2E_GIT_PASSWORD\" | sh -c '<temporary credential file>; git clone --branch v1.0.0 $E2E_GIT_URL $site_dir; <file removed>; chown -R sysmin:sysmin $site_dir'"
	printf '%s\n' "$E2E_GIT_PASSWORD" | docker exec -i "$host" sh -c '
		IFS= read -r p
		set -e
		f=$(mktemp)
		trap "rm -f $f" EXIT
		printf "https://%s:%s@%s\n" "$1" "$p" "$(printf "%s" "$2" | cut -d / -f 3)" >"$f"
		git -c credential.helper= -c "credential.helper=store --file=$f" -c advice.detachedHead=false \
			clone -q --branch v1.0.0 "$2" "$3"
		chown -R sysmin:sysmin "$3"' sh "$E2E_GIT_USER" "$E2E_GIT_URL" "$site_dir" >/dev/null 2>&1
}

# site_tag prints the tag the working copy has checked out.
site_tag() {
	docker exec -u sysmin "$host" git -C "$site_dir" describe --tags --exact-match 2>&1
}

# site_file [webhook] prints the project file of demo-site, with its webhook when asked. The long poll
# interval keeps polling out of the webhook step.
site_file() {
	cat <<'EOF'
schema: 1
name: demo-site
dir: /srv/demo-site
poll_interval: 1h
steps:
  - type: git-checkout
    tag: "{version}"
  - type: command
    run: cp site/VERSION deployed.txt && echo built {version}
    timeout: 1m
notify:
  channels:
    - e2e-hook
  events:
    - update-succeeded
    - update-failed
git:
  url: https://git.sysmine2e.test/stacks.git
  credential: e2e-git
  version_rule:
    mode: auto
    tag_filter: ^v\d+\.\d+\.\d+$
EOF
	[ "${1:-}" != webhook ] || printf 'webhook:\n  kind: hmac\n  secret: demo-site-hook\n'
}

# hook_post VAR BODY signs the body file with the secret in the named variable, the way a git forge signs a
# webhook (HMAC-SHA256 in X-Sysmin-Signature), posts it to the listener on the host and prints the status.
hook_post() {
	sig=$(eval "printf '%s\n' \"\${$1}\"" | "$work/ctx/fakes" sign "$2") || return 1
	sc_note "\$ curl -X POST -H 'X-Sysmin-Signature: sha256=<HMAC-SHA256 of the body with \$$1>' --data-binary @${2#"$sc_dir/"} $hook_url"
	docker exec -i -u "$admin" "$host" curl -sS -o /dev/null -w '%{http_code}' -X POST \
		-H 'Content-Type: application/json' -H "X-Sysmin-Signature: $sig" --data-binary @- "$hook_url" <"$2" 2>/dev/null
}

listener_up() {
	[ "$(docker exec "$host" curl -s -o /dev/null -w '%{http_code}' -X POST "$hook_url")" != 000 ]
}

# hook_bodies prints the bodies of the messages the webhook receiver got, one JSON object per line.
hook_bodies() {
	docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null |
		sed -n 's/.*"body":"\(.*\)","headers".*/\1/p' | sed 's/\\"/"/g' | grep -v '^{"check":true}$'
}

locked_reported() {
	[ "$(hook_bodies | grep -c '"event":"vault.locked"')" -gt "$1" ]
}

# audit_lines PATTERN prints time, action, result and error code of the audit log entries that match.
audit_lines() {
	docker exec "$host" sh -c 'cat /var/lib/sysmin/logs/*.jsonl' 2>/dev/null | grep -F -- "$1" |
		sed -E 's/.*"time":"([^"]*)".*"action":"([^"]*)".*"result":"([^"]*)"(,"error_code":"([^"]*)")?.*/\1 \2 \3 \5/'
}

host_running() {
	state=$(docker exec "$host" systemctl is-system-running 2>/dev/null || true)
	[ "$state" = running ] || [ "$state" = degraded ]
}

# restart_host restarts the host container, which stands for a reboot, and waits for systemd, the inner
# Docker and the control socket of sysmin.
restart_host() {
	sc_note "# docker restart $host (stands for a reboot of the host)"
	docker restart -t 30 "$host" >/dev/null || return 1
	poll 120 host_running && poll 60 docker exec "$host" docker info && poll 60 cliq status
}

# stacks lists name, image and state of every container on the host.
stacks() {
	docker exec "$host" docker ps -a --format '{{.Names}} {{.Image}} {{.State}}' | sort
}

all_running() {
	! stacks | grep -qv ' running$'
}

# containers lists name, id and start time of every running container on the host.
containers() {
	docker exec "$host" sh -c "docker ps -q | xargs -r docker inspect -f '{{.Name}} {{.Id}} {{.State.StartedAt}}' | sort"
}

s08_git_project() {
	if ! git_release v1.0.0 || ! site_clone; then
		failed 'the repository could not be released and cloned into the project directory'
		return 1
	fi
	evidence "working copy $site_dir (owner sysmin) on $(site_tag), origin $(docker exec -u sysmin "$host" git -C "$site_dir" remote get-url origin)"
	out=$(hsecret "$admin" E2E_GIT_PASSWORD sudo sysmin credential set e2e-git --kind git-https \
		--user "$E2E_GIT_USER" --from-stdin --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "credential set: $out"
		return 1
	fi
	evidence "credential set e2e-git (token on stdin): state=$(kv "$out" state)"
	site_file >"$sc_dir/files/demo-site.yaml"
	out=$(hfile "$sc_dir/files/demo-site.yaml" sudo sysmin project add demo-site --file - --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "project add: $out"
		return 1
	fi
	evidence "project add demo-site (git source, steps git-checkout and command): state=$(kv "$out" state)"
	out=$(cli approvals show demo-site --format compact 2>&1) || true
	free=$(printf '%s\n' "$out" | grep '|true$')
	evidence "approvals show demo-site, the change marked as a free command:
$free"
	printf '%s\n' "$free" | grep -q '^demo-site|new|steps\[1\]\.run|' || failed 'the command step is not marked as a free command'
	tui_keys Escape 4
	if tui_wait 'free command'; then
		tui_save approval
		evidence 'Access shows the open approval with "⚠ free command" and asks to read the command line before approving'
	else
		failed 'Access does not mark the free command'
	fi
	if ! tui_approve_all; then
		tui_save approve-failed
		failed 'approving in Access did not succeed'
		return 1
	fi
	out=$(cli source test demo-site --format compact 2>&1) || true
	line=$(printf '%s\n' "$out" | grep '^git|')
	evidence "after the approval: source test: status=$(field "$line" 4) current=$(field "$line" 5)"
	[ "$(field "$line" 5)" = v1.0.0 ] || failed 'the service does not read v1.0.0 from the working copy'

	if ! git_release v2.0.0; then
		failed 'v2.0.0 could not be pushed'
		return 1
	fi
	out=$(cli deploy run-now demo-site --format compact 2>&1) || true
	evidence "pushed tag v2.0.0; run-now: status=$(kv "$out" status) result=$(kv "$out" result) changes=$(kv "$out" changes)"
	if ! line=$(wait_run demo-site v2.0.0); then
		failed 'no run to v2.0.0 was listed'
		return 1
	fi
	id=$(field "$line" 1) result=$(field "$line" 5)
	log=$(run_log demo-site "$id")
	tag=$(site_tag)
	built=$(docker exec "$host" cat "$site_dir/deployed.txt" 2>&1)
	evidence "run $id: result=$result; working copy on $tag, deployed.txt holds $built; log: logs/demo-site-$id.log"
	evidence "run log:
$(printf '%s\n' "$log" | grep -E ' (run|result=)|built |-> ' | sed 's/^[^ ]* //')"
	[ "$result" = succeeded ] && [ "$tag" = v2.0.0 ] && [ "$built" = v2.0.0 ] ||
		failed 'the working copy is not on v2.0.0 after the run'
	printf '%s\n' "$log" | grep -q 'type=git-checkout result=ok' && printf '%s\n' "$log" | grep -q 'type=command result=ok' ||
		failed 'the log does not show both steps succeeding'
}

s09_webhook() {
	out=$(hsecret "$admin" E2E_WEBHOOK_SECRET sudo sysmin credential set demo-site-hook --kind secret \
		--from-stdin --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "credential set: $out"
		return 1
	fi
	evidence "credential set demo-site-hook (secret on stdin): state=$(kv "$out" state)"
	site_file webhook >"$sc_dir/files/demo-site-webhook.yaml"
	out=$(hfile "$sc_dir/files/demo-site-webhook.yaml" sudo sysmin project edit demo-site --file - --format compact 2>&1) && rc=0 || rc=$?
	if [ "$rc" != 0 ]; then
		failed "project edit: $out"
		return 1
	fi
	evidence "project edit demo-site adds webhook kind hmac, secret demo-site-hook: state=$(kv "$out" state)"
	if ! tui_approve_all; then
		tui_save approve-failed
		failed 'approving in Access did not succeed'
		return 1
	fi
	sc_note "poll: curl -X POST $hook_url until the listener answers"
	if ! poll 30 listener_up; then
		failed 'the webhook listener did not start on 127.0.0.1:8787 (the default)'
		return 1
	fi
	evidence 'the listener runs on 127.0.0.1:8787, the default address, once the approved project has a webhook'
	if ! git_release v2.1.0; then
		failed 'v2.1.0 could not be pushed'
		return 1
	fi
	st=$(cliq deploy status demo-site --format compact 2>&1) || true
	evidence "pushed tag v2.1.0; poll interval 1h, last check $(kv "$st" last_check)"
	printf '{"ref":"refs/tags/v2.1.0","repository":{"name":"stacks"}}' >"$sc_dir/files/hook-v2.1.0.json"
	E2E_WRONG_SECRET=not-the-webhook-secret
	code=$(hook_post E2E_WRONG_SECRET "$sc_dir/files/hook-v2.1.0.json") || true
	sleep 3
	evidence "signed with a wrong secret: HTTP $code; a run to v2.1.0 afterwards: $(last_run demo-site v2.1.0 | grep -c .)"
	[ "$code" = 401 ] || failed 'the request with a wrong signature was not refused with 401'
	! has_run demo-site v2.1.0 || failed 'a request with a wrong signature started a run'
	sent=$(date +%s)
	code=$(hook_post E2E_WEBHOOK_SECRET "$sc_dir/files/hook-v2.1.0.json") || true
	evidence "signed with the secret at $(date -u -d "@$sent" +%Y-%m-%dT%H:%M:%SZ): HTTP $code"
	[ "$code" = 202 ] || failed 'the signed request was not accepted with 202'
	if ! line=$(wait_run demo-site v2.1.0 30); then
		failed 'the signed request started no run to v2.1.0'
		return 1
	fi
	started=$(field "$line" 3)
	delay=$(($(date -d "$started" +%s) - sent))
	evidence "run $(field "$line" 1): action=$(field "$line" 2) started=$started (${delay}s after the request, no run-now) result=$(field "$line" 5); working copy on $(site_tag)"
	[ "$delay" -ge -1 ] && [ "$delay" -le 10 ] || failed 'the run did not follow the request at once'
	[ "$(field "$line" 5)" = succeeded ] && [ "$(site_tag)" = v2.1.0 ] || failed 'the run did not check out v2.1.0'
	evidence "audit log (time, action, result, error code; the first entry is the unsigned probe of the listener):
$(audit_lines '"action":"webhook.' | tail -n 3)"
	audit_lines '"action":"webhook.' | tail -n 3 | grep -q 'webhook.reject failure invalid-signature' ||
		failed 'the audit log does not record the refused signature'
}

s10_notifications() {
	file=$(kv "$(cliq project show demo-shop --format compact 2>&1)" file)
	{
		printf '%b' "$file"
		printf 'notify:\n  channels:\n    - e2e-hook\n  events:\n    - update-available\n'
	} >"$sc_dir/files/demo-shop-notify.yaml"
	out=$(hfile "$sc_dir/files/demo-shop-notify.yaml" sudo sysmin project edit demo-shop --file - --format compact 2>&1) && rc=0 || rc=$?
	evidence "demo-shop (manual) subscribes to update-available on e2e-hook: state=$(kv "$out" state)"
	if [ "$rc" != 0 ] || ! tui_approve_all; then
		tui_save approve-failed
		failed "the notify settings of demo-shop could not be stored and approved: $out"
		return 1
	fi
	if ! push_image worker 1.1.0 "$E2E_CHANGELOG_GIT_REPO"; then
		failed 'worker:1.1.0 could not be pushed'
		return 1
	fi
	out=$(cli deploy run-now demo-shop --format compact 2>&1) || true
	evidence "pushed worker:1.1.0; run-now demo-shop: status=$(kv "$out" status) (manual mode, nothing deployed)"
	sc_note 'poll: e2e/run.sh requests until update.available for demo-shop arrived'
	poll 20 hook_has update.available demo-shop || true
	sc_note 'e2e/run.sh requests'
	bodies=$(hook_bodies)
	evidence "bodies of the messages the webhook receiver got:
$bodies"
	for want in deploy.succeeded:demo-web deploy.rolled_back:demo-web deploy.succeeded:demo-site update.available:demo-shop; do
		printf '%s\n' "$bodies" | grep -F "\"event\":\"${want%%:*}\"" | grep -qF "\"project\":\"${want#*:}\"" ||
			failed "no ${want%%:*} message for ${want#*:}"
	done
	raw=$(docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null)
	leaks=0
	for value in "$E2E_REGISTRY_PASSWORD" "$E2E_GIT_PASSWORD" "$E2E_VAULT_PASSPHRASE" "$E2E_WEBHOOK_SECRET"; do
		printf '%s' "$raw" | grep -qF -- "$value" && leaks=$((leaks + 1))
	done
	evidence "none of the four secrets of the run occurs in a request: $leaks found; header names sent: $(printf '%s\n' "$raw" | grep -F X-Sysmin-Event | sed 's/.*"headers":{\([^}]*\)}.*/\1/' | grep -o '"[A-Za-z-]*":' | tr -d '":' | sort -u | tr '\n' ' ')"
	[ "$leaks" = 0 ] || failed 'a secret occurs in a notification'
}

s11_restart_locked() {
	out=$(cli project edit demo-site --poll-interval 1m --format compact 2>&1) || true
	if ! tui_approve_all; then
		tui_save approve-failed
		failed "the poll interval of demo-site could not be approved: $out"
		return 1
	fi
	evidence 'demo-site polls every minute from now on (edited on the command line, approved in Access)'
	evidence "containers before the restart:
$(stacks)"
	reported=$(hook_bodies | grep -c '"event":"vault.locked"')
	if ! restart_host; then
		failed 'the host or sysmin did not come back after the restart'
		return 1
	fi
	t0=$(date +%s)
	out=$(cli status --format compact 2>&1) || true
	evidence "after the restart: sysmin.service $(docker exec "$host" systemctl is-active sysmin), docker.service $(docker exec "$host" systemctl is-active docker); status: vault=$(kv "$out" vault) unlock=$(kv "$out" unlock)"
	[ "$(kv "$out" vault)" = locked ] || failed 'the vault is not locked after the restart'
	poll 30 all_running || true
	evidence "containers after the restart (restart policy unless-stopped):
$(stacks)"
	all_running || failed 'not every container runs again after the restart'
	sc_note 'poll: e2e/run.sh requests until a new vault.locked message arrived'
	if poll 30 locked_reported "$reported"; then
		evidence "webhook receiver: $(hook_bodies | grep '"event":"vault.locked"' | tail -n 1)"
	else
		failed 'no vault.locked message after the restart'
	fi
	if ! git_release v2.2.0; then
		failed 'v2.2.0 could not be pushed'
		return 1
	fi
	out=$(cli deploy run-now demo-site --format compact 2>&1) || true
	evidence "pushed tag v2.2.0; run-now while locked: status=$(kv "$out" status) skipped=$(kv "$out" skipped)"
	[ "$(kv "$out" skipped)" = locked ] || failed 'run-now did not report the locked vault'
	printf '{"ref":"refs/tags/v2.2.0","repository":{"name":"stacks"}}' >"$sc_dir/files/hook-v2.2.0.json"
	code=$(hook_post E2E_WEBHOOK_SECRET "$sc_dir/files/hook-v2.2.0.json") || true
	evidence "signed webhook while locked: HTTP $code (000: the listener does not run while the vault is locked)"
	# The planner places the first poll of each project up to five minutes after the start; waiting past it
	# covers that poll and five poll intervals of demo-site.
	sc_note 'wait until 320 seconds after the restart, the vault still locked'
	while [ $(($(date +%s) - t0)) -lt 320 ]; do
		sleep 5
	done
	out=$(cli deploy run-now demo-site --format compact 2>&1) || true
	st=$(cli deploy status demo-site --format compact 2>&1) || true
	evidence "$(($(date +%s) - t0))s after the restart: run-now: status=$(kv "$out" status) skipped=$(kv "$out" skipped); deploy status: locked=$(kv "$st" locked) sources=$(kv "$st" sources) last_run=$(kv "$st" last_run); runs to v2.2.0: $(last_run demo-site v2.2.0 | grep -c .)"
	! has_run demo-site v2.2.0 || failed 'demo-site deployed while the vault was locked'
	[ "$(site_tag)" = v2.1.0 ] || failed 'the working copy changed while the vault was locked'
	if ! tui_start; then
		failed 'the terminal interface did not start'
		return 1
	fi
	tui_save locked
	tui_has 'vault locked' || failed 'the interface does not show the locked vault'
	if ! tui_unlock; then
		tui_save unlock-failed
		failed 'unlocking with ctrl+l did not succeed'
		return 1
	fi
	unlocked=$(date +%s)
	tui_save unlocked
	evidence "vault unlocked with ctrl+l in the interface at $(date -u -d "@$unlocked" +%H:%M:%S)"
	if line=$(wait_run demo-site v2.2.0 100); then
		evidence 'demo-site deployed v2.2.0 on its own after the unlock'
	else
		st=$(cliq deploy status demo-site --format compact 2>&1) || true
		deviation "expected: after the unlock demo-site deploys v2.2.0 within its poll interval of one minute; actual: no check within 100s after the unlock (last_check=$(kv "$st" last_check), from before the restart). The planner schedules a poll with the interval of the approved project only once a check has read the approved state (internal/engine/planner.go, interval and Run); after a start with a locked vault the first poll is skipped (internal/engine/run.go, check returns SkipLocked) and the next one is due after the default interval of one hour (config.DefaultPollInterval)"
		out=$(cli deploy run-now demo-site --format compact 2>&1) || true
		workaround "run-now after the unlock: status=$(kv "$out" status) result=$(kv "$out" result) changes=$(kv "$out" changes)"
		line=$(wait_run demo-site v2.2.0 30) || true
	fi
	evidence "history: $line; working copy on $(site_tag)"
	[ "$(field "$line" 5)" = succeeded ] && [ "$(site_tag)" = v2.2.0 ] || failed 'demo-site did not deploy v2.2.0 after the unlock'
}

s12_restart_auto_unlock() {
	out=$(hsecret "$admin" E2E_VAULT_PASSPHRASE sudo sysmin vault bind --format compact 2>&1) && rc=0 || rc=$?
	evidence "vault bind (passphrase on stdin): unlock=$(kv "$out" unlock) credential=$(kv "$out" credential)
warning: $(kv "$out" warning)"
	if [ "$rc" != 0 ]; then
		failed "vault bind: $out"
		return 1
	fi
	evidence "$(hroot 'ls -l /var/lib/sysmin/vault/key.cred /var/lib/sysmin/vault/key.cred.mode' 2>&1 | awk '{ print $1, $3, $NF }')"
	# The service stops first, so the tag waits for the first poll after the restart and no earlier poll.
	hroot 'systemctl stop sysmin'
	if ! git_release v2.3.0; then
		failed 'v2.3.0 could not be pushed'
		return 1
	fi
	if ! restart_host; then
		failed 'the host or sysmin did not come back after the restart'
		return 1
	fi
	t0=$(date +%s)
	out=$(cli status --format compact 2>&1) || true
	evidence "pushed tag v2.3.0 before the restart; after it, without any input: vault=$(kv "$out" vault) unlock=$(kv "$out" unlock)"
	[ "$(kv "$out" vault)" = unlocked ] && [ "$(kv "$out" unlock)" = host ] ||
		failed 'the vault is not unlocked with the host key after the restart'
	poll 30 all_running || failed "not every container runs again after the restart: $(stacks)"
	if tui_start && tui_wait 'automatic (host key)'; then
		tui_save overview
		evidence "overview: $(tui_screen | grep -E 'Vault|Unlock' | sed 's/^ *> *//; s/  */ /g' | tr '\n' ';')"
		tui_screen | grep -q '⚠ Unlock' || failed 'the overview shows no warning for the automatic unlock'
	else
		tui_save no-warning
		failed 'the overview does not show the automatic unlock'
	fi
	if line=$(wait_run demo-site v2.3.0 330); then
		started=$(field "$line" 3)
		evidence "run $(field "$line" 1): action=$(field "$line" 2) started=$started ($(($(date -d "$started" +%s) - t0))s after the service was back, no run-now) result=$(field "$line" 5); working copy on $(site_tag)"
		[ "$(field "$line" 5)" = succeeded ] && [ "$(site_tag)" = v2.3.0 ] || failed 'the run did not check out v2.3.0'
	else
		failed 'demo-site did not deploy v2.3.0 after the restart'
	fi
}

s13_self_update() {
	api=${E2E_RELEASES_URL%/releases}/repos/castrowithcee/qatlas-sysmin
	projects=$(cliq project list --format compact 2>&1)
	out=$(cli update --list --api-url "$api" --format compact 2>&1) || true
	evidence "update --list:
$out"
	sc_note '# on the release server: v0.2.0/checksums.txt.sig replaced by the signature of v0.1.0'
	docker exec "$prefix-releases" sh -c 'cd /srv/releases && cp v0.2.0/checksums.txt.sig /tmp/v0.2.0.sig && cp v0.1.0/checksums.txt.sig v0.2.0/checksums.txt.sig'
	out=$(cli update --api-url "$api" --to v0.2.0 --yes --format compact 2>&1) && rc=0 || rc=$?
	ver=$(docker exec "$host" sysmin version --format compact | sed 's/^version=//')
	evidence "with the signature of another release: exit $rc: $out; installed: $ver"
	[ "$rc" != 0 ] && [ "$ver" = v0.1.0 ] || failed 'a release with a wrong signature was installed'
	sc_note '# on the release server: the signature of v0.2.0 restored'
	docker exec "$prefix-releases" sh -c 'cp /tmp/v0.2.0.sig /srv/releases/v0.2.0/checksums.txt.sig'
	out=$(cli update --api-url "$api" --yes --format compact 2>&1) && rc=0 || rc=$?
	evidence "update: previous=$(kv "$out" previous) version=$(kv "$out" version) updated=$(kv "$out" updated) restarted=$(kv "$out" restarted)"
	if [ "$rc" != 0 ]; then
		failed "update: $out"
		return 1
	fi
	poll 60 cliq status || true
	ver=$(hrun sysmin version --format compact 2>&1 | sed 's/^version=//')
	st=$(cli vault status --format compact 2>&1) || true
	after=$(cliq project list --format compact 2>&1)
	evidence "sysmin version: $ver; sysmin.service $(docker exec "$host" systemctl is-active sysmin); vault status: vault=$(kv "$st" vault) unlock=$(kv "$st" unlock)"
	evidence "projects after the update (unchanged: $([ "$after" = "$projects" ] && echo yes || echo no)):
$after"
	[ "$ver" = v0.2.0 ] || failed 'sysmin version does not show v0.2.0'
	[ "$(kv "$st" vault)" = unlocked ] && [ "$(kv "$st" unlock)" = host ] || failed 'the vault is not open by the automatic unlock'
	[ "$after" = "$projects" ] || failed 'the projects changed'
}

s14_uninstall() {
	before=$(containers)
	out=$(cli uninstall --format compact 2>&1) && rc=0 || rc=$?
	evidence "uninstall without --yes and without a terminal: exit $rc: $out"
	[ "$rc" != 0 ] || failed 'uninstall ran without a confirmation'
	hroot 'cp /usr/local/bin/sysmin /root/sysmin-kept'
	out=$(cli uninstall --yes --format compact 2>&1) && rc=0 || rc=$?
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
	kept=$(docker exec "$host" sh -c 'ls -d /etc/sysmin/sysmin.yaml /etc/sysmin/projects/* /var/lib/sysmin/vault/* 2>&1')
	evidence "kept:
$kept"
	printf '%s\n' "$kept" | grep -q '/var/lib/sysmin/vault/secrets.age' && printf '%s\n' "$kept" | grep -q '/etc/sysmin/projects/demo-site.yaml' ||
		failed 'configuration or vault were removed without --purge'
	after=$(containers)
	page=$(docker exec "$host" curl -fsS http://127.0.0.1:18081/ 2>&1) || true
	evidence "running containers unchanged (name, id, start): $([ "$before" = "$after" ] && echo yes || echo no); demo-web answers '$page'"
	[ "$before" = "$after" ] || failed "the stacks changed:
$after"
	out=$(hroot '/root/sysmin-kept uninstall --purge --yes --format compact; rm -f /root/sysmin-kept' 2>&1) || true
	left=$(docker exec "$host" sh -c 'ls -d /etc/sysmin /var/lib/sysmin /srv/* 2>&1')
	evidence "uninstall --purge with a copy of the program kept before: purged=$(kv "$out" purged); what is left (the project directories belong to the administrator):
$left"
	printf '%s\n' "$left" | grep -qE '^/etc/sysmin$|^/var/lib/sysmin$' && failed '--purge left configuration or state'
	after=$(containers)
	evidence "running containers unchanged after --purge: $([ "$before" = "$after" ] && echo yes || echo no)"
	[ "$before" = "$after" ] || failed 'the stacks changed after --purge'
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
	for value in "$E2E_REGISTRY_PASSWORD" "$E2E_GIT_PASSWORD" "$E2E_VAULT_PASSPHRASE" "$E2E_WEBHOOK_SECRET"; do
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
	if [ -z "${E2E_WEBHOOK_SECRET:-}" ]; then
		E2E_WEBHOOK_SECRET=e2e-test-hook-$(openssl rand -hex 12)
		printf 'E2E_WEBHOOK_SECRET=%s\n' "$E2E_WEBHOOK_SECRET" >>"$work/testenv"
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

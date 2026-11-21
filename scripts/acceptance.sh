#!/usr/bin/env bash
set -euo pipefail
umask 077

contracts_only=false
packaged_plugin=
if [[ $# == 1 && $1 == --contracts-only ]]; then
    contracts_only=true
elif [[ $# == 2 && $1 == --packaged-plugin && -n $2 ]]; then
    packaged_plugin=$(cd -- "$2" && pwd -P)
elif (( $# != 0 )); then
    printf 'Usage: %s [--contracts-only | --packaged-plugin DIRECTORY]\n' "$0" >&2
    exit 2
fi

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
checkout=$(pwd -P)
if [[ -n $packaged_plugin ]]; then
    if [[ $packaged_plugin/ == "$checkout/"* || -e $packaged_plugin/.git ||
          ! -f $packaged_plugin/plugin.info.txt ]]; then
        printf 'Packaged plugin must be outside the harness and is not a checkout\n' >&2
        exit 1
    fi
fi
# This Compose variable is the source of the plugin bind mount in both modes.
PETAR_ACCEPTANCE_CHECKOUT=${packaged_plugin:-"$checkout"}
PETAR_ACCEPTANCE_UID=$(stat -c %u "$PETAR_ACCEPTANCE_CHECKOUT")
PETAR_ACCEPTANCE_GID=$(stat -c %g "$PETAR_ACCEPTANCE_CHECKOUT")
PETAR_ACCEPTANCE_PASSWORD=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
[[ ${#PETAR_ACCEPTANCE_PASSWORD} == 48 ]]
export PETAR_ACCEPTANCE_CHECKOUT PETAR_ACCEPTANCE_UID PETAR_ACCEPTANCE_GID PETAR_ACCEPTANCE_PASSWORD

tmp_root=$(cd -- "${TMPDIR:-/tmp}" && pwd -P)
if [[ "$tmp_root/" == "$checkout/"* || "$tmp_root/" == "$PETAR_ACCEPTANCE_CHECKOUT/"* ]]; then
    printf 'TMPDIR must be outside the checkout\n' >&2
    exit 1
fi
diagnostics=$(mktemp -d "$tmp_root/petar-php03.XXXXXXXXXXXX")
suffix=${diagnostics##*.}
project=petarphp03_${suffix,,}
compose=(docker compose --env-file /dev/null -p "$project" -f tests/acceptance/compose.yaml --profile browser)
stage=prepare
child=
resources=false

finish() {
    local rc=$? cleanup_rc=0
    trap - EXIT
    trap '' INT TERM
    if [[ -n $child ]]; then
        kill -TERM "$child" 2>/dev/null || :
        wait "$child" 2>/dev/null || :
    fi
    if (( rc != 0 )); then
        printf 'FAIL: stage=%s exit=%d; diagnostics: %s\n' "$stage" "$rc" "$diagnostics" >&2
        if $resources; then
            timeout -k 5s 15s "${compose[@]}" ps -a > "$diagnostics/compose-ps.log" 2>&1 || :
        fi
    fi
    if $resources; then
        timeout -k 5s 60s "${compose[@]}" down --volumes --remove-orphans --rmi local \
            > "$diagnostics/cleanup.log" 2>&1 || cleanup_rc=$?
    fi
    if (( cleanup_rc != 0 )); then
        printf 'FAIL: cleanup exit=%d; see %s/cleanup.log\n' "$cleanup_rc" "$diagnostics" >&2
        if (( rc == 0 )); then rc=$cleanup_rc; fi
    fi
    if (( rc == 0 )); then
        if "$contracts_only"; then
            printf 'CONTRACTS-ONLY PASSED (%s)\n' "$project"
        elif [[ -n $packaged_plugin ]]; then
            printf 'PACKAGED PLUGIN ACCEPTANCE PASSED (%s)\n' "$project"
        else
            printf 'ACCEPTANCE PASSED (%s)\n' "$project"
        fi
    fi
    exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

run_stage() {
    local rc=0 limit=$2
    stage=$1
    shift 2
    printf '== %s ==\n' "$stage"
    timeout -k 5s "$limit" "$@" > "$diagnostics/$stage.log" 2>&1 &
    child=$!
    wait "$child" || rc=$?
    child=
    cat "$diagnostics/$stage.log"
    return "$rc"
}

printf 'Compose project: %s\nDiagnostics: %s\n' "$project" "$diagnostics"
run_stage pull 300s "${compose[@]}" pull --policy missing wiki fixture
resources=true
if ! "$contracts_only"; then
    run_stage build 600s "${compose[@]}" build browser
fi
run_stage startup 90s "${compose[@]}" up -d --no-build --wait --wait-timeout 60 wiki fixture

stage=wiki-id
PETAR_TEST_WIKI_CONTAINER=$(timeout -k 5s 15s "${compose[@]}" ps -q wiki)
if [[ ! $PETAR_TEST_WIKI_CONTAINER =~ ^[0-9a-f]{64}$ ]]; then
    printf 'Expected one running wiki container ID for %s\n' "$project" >&2
    exit 1
fi
export PETAR_TEST_WIKI_CONTAINER
printf 'Wiki container: %s\n' "$PETAR_TEST_WIKI_CONTAINER"
stage=wiki-mount
plugin_mount=$(timeout -k 5s 15s docker inspect --type container --format \
    '{{range .Mounts}}{{if eq .Destination "/config/dokuwiki/lib/plugins/petar"}}{{.Type}} {{.RW}} {{.Source}}{{end}}{{end}}' \
    "$PETAR_TEST_WIKI_CONTAINER")
if [[ $plugin_mount != 'bind false '* || ! ${plugin_mount#bind false } -ef $PETAR_ACCEPTANCE_CHECKOUT ]]; then
    printf 'Wiki plugin mount must bind the selected source read-only\n' >&2
    exit 1
fi
printf 'Read-only plugin mount: %s\n' "${plugin_mount#bind false }"
if [[ -z $packaged_plugin ]]; then
    run_stage contracts 180s ./scripts/quality-gate.sh
fi
if ! "$contracts_only"; then
    # Require the fixture test explicitly; the real-backend drill is separate.
    run_stage chromium 60s "${compose[@]}" run --rm --no-deps -T browser \
        ./node_modules/.bin/playwright test --grep \
        'ai sees ready form, journal and selected job results on one page'
fi

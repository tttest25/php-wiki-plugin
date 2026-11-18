#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
readonly plugin_root=/config/dokuwiki/lib/plugins/petar

printf '%s\n' '== Git diff (working tree and index) =='
git diff --check
git diff --cached --check

printf '%s\n' '== PHP lint (dokuwiki) =='
# Contract tests load PHP from this path, so do not test a different checkout.
mount=$(docker inspect --type container --format \
    '{{range .Mounts}}{{if eq .Destination "/config/dokuwiki/lib/plugins/petar"}}{{.Type}} {{.Source}}{{end}}{{end}}' dokuwiki)
if [[ $mount != 'bind '* ]] || [[ ! ${mount#bind } -ef $PWD ]]; then
    printf 'dokuwiki must bind-mount this checkout at %s\n' "$plugin_root" >&2
    exit 1
fi
git ls-files -z --cached --others --exclude-standard -- '*.php' |
    while IFS= read -r -d '' file; do
        [[ -f $file ]] || continue
        docker exec dokuwiki php -l "$plugin_root/$file"
    done

printf '%s\n' '== JavaScript syntax =='
git ls-files -z --cached --others --exclude-standard -- '*.js' |
    while IFS= read -r -d '' file; do
        [[ -f $file ]] || continue
        node --check -- "$file"
    done

printf '%s\n' '== Go vet (contracts) =='
GO111MODULE=off go vet ./tests/contract
printf '%s\n' '== Contract tests (race detector) =='
GO111MODULE=off go test -count=1 -race -timeout 120s ./tests/contract

printf '%s\n' 'QUALITY GATE PASSED'

#!/usr/bin/env bash
set -euo pipefail
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
if [[ $# == 3 && $1 == --verify-only ]]; then
    exec python3 "$scripts/plugin-release.py" verify "$2" "$3"
fi
if [[ $# != 2 ]]; then
    echo "Usage: $0 [--verify-only] ARCHIVE SHA256SUMS" >&2
    exit 2
fi
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
python3 "$scripts/plugin-release.py" verify "$1" "$2" "$scratch"
bash "$scripts/acceptance.sh" --packaged-plugin "$scratch/petar"

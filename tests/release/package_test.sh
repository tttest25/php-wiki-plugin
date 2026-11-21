#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.."
package=$PWD/scripts/package-plugin.sh
check=$PWD/scripts/check-plugin-release.sh
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
version=v0.1.0
asset=petar-$version.tar.gz
sha=$(git rev-parse HEAD)
mkdir "$tmp"/{a,b}

reject() {
    local diagnostic=$1
    shift
    if "$@" >"$tmp/rejected.log" 2>&1 || ! grep -Fq -- "$diagnostic" "$tmp/rejected.log"; then
        cat "$tmp/rejected.log" >&2
        printf 'Expected rejection: %s (%s)\n' "$diagnostic" "$*" >&2
        exit 1
    fi
}

bash "$package" "$sha" "$version" "$tmp/a"
git clone -q --shared --no-hardlinks . "$tmp/checkout"
(
    cd "$tmp/checkout"
    git checkout --quiet --detach "$sha"
    printf 'local change\n' >action.php
    touch extra.txt
    bash "$package" "$sha" "$version" "$tmp/b"
)
cmp "$tmp/a/$asset" "$tmp/b/$asset"
cmp "$tmp/a/SHA256SUMS" "$tmp/b/SHA256SUMS"
(cd "$tmp/a" && sha256sum --check --strict SHA256SUMS)
bash "$check" --verify-only "$tmp/a/$asset" "$tmp/a/SHA256SUMS"
mkdir "$tmp/unpacked"
python3 scripts/plugin-release.py verify "$tmp/a/$asset" "$tmp/a/SHA256SUMS" "$tmp/unpacked"

python3 - "$tmp/a/$asset" "$tmp/checkout" "$tmp/unpacked" <<'PY'
from pathlib import Path
import subprocess
import sys
import tarfile

expected = {
    'action.php', 'syntax.php', 'PetarClient.php', 'script.js', 'style.css',
    'plugin.info.txt', 'conf/default.php', 'conf/metadata.php',
    'lang/en/lang.php', 'lang/en/settings.php', 'lang/ru/lang.php',
    'lang/ru/settings.php',
}
unpacked = Path(sys.argv[3])
assert {path.relative_to(unpacked).as_posix() for path in unpacked.rglob('*') if path.is_file()} == {
    'petar/' + name for name in expected
}
with tarfile.open(sys.argv[1], 'r:gz') as archive:
    assert {item.name for item in archive} == {'petar/' + name for name in expected}
    for item in archive:
        name = item.name[len('petar/'):]
        git = ['git', '-C', sys.argv[2]]
        mode = subprocess.check_output([*git, 'ls-tree', 'HEAD', '--', name]).split()[0]
        assert item.isfile() and item.mode == (int(mode, 8) & 0o777)
        data = subprocess.check_output([*git, 'show', f'HEAD:{name}'])
        assert archive.extractfile(item).read() == data
        assert (unpacked / item.name).read_bytes() == data
        assert (unpacked / item.name).stat().st_mode & 0o777 == item.mode
PY

for case in missing extra symlink traversal duplicate mode corrupt wrong-name; do
    mkdir "$tmp/$case"
    python3 - "$tmp" "$asset" "$case" <<'PY'
import hashlib
import io
from pathlib import Path
import sys
import tarfile

root, asset, case = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
source = root / 'a' / asset
target = root / case / asset
if case in {'corrupt', 'wrong-name'}:
    target.write_bytes(source.read_bytes() + (b'!' if case == 'corrupt' else b''))
else:
    with tarfile.open(source, 'r:gz') as src, tarfile.open(target, 'w:gz') as dest:
        for member in src:
            if member.name == 'petar/lang/ru/lang.php':
                if case == 'missing':
                    continue
                if case == 'symlink':
                    member.type, member.linkname, member.size = tarfile.SYMTYPE, '/tmp/elsewhere', 0
                if case == 'mode':
                    member.mode = 0o777
            dest.addfile(member, src.extractfile(member) if member.isfile() else None)
        names = {'extra': 'petar/extra.txt', 'traversal': 'petar/../escape', 'duplicate': 'petar/action.php'}
        if case in names:
            info = tarfile.TarInfo(names[case])
            info.size, info.mode = 1, 0o644
            dest.addfile(info, io.BytesIO(b'x'))
digest = hashlib.sha256((source if case == 'corrupt' else target).read_bytes()).hexdigest()
(target.parent / 'SHA256SUMS').write_text(f'{digest}  {"other.tar.gz" if case == "wrong-name" else asset}\n')
PY
    case "$case" in
        corrupt) diagnostic='SHA-256 mismatch' ;;
        wrong-name) diagnostic='SHA256SUMS must name' ;;
        *) diagnostic='archive members' ;;
    esac
    # Use the real install entry point: an invalid archive must fail before Docker.
    reject "$diagnostic" bash "$check" "$tmp/$case/$asset" "$tmp/$case/SHA256SUMS"
done
printf '%s\n' 'PLUGIN PACKAGE CONTRACT PASSED'

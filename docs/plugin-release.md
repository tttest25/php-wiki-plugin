# Installing and releasing `petar`

The release includes `petar-vMAJOR.MINOR.PATCH.tar.gz` and `SHA256SUMS`.
The release notes give the full source commit. The archive contains only
the DokuWiki runtime files, all under `petar/`. Git, Go, the test suite
and a plugin checkout are not needed to install it.

## Install from a release

1. Open the release for the intended version in this repository. Download
   the archive and `SHA256SUMS` into a new directory. Use both files from
   that release; a checksum is an integrity check, not a separate signature.
2. From that directory run `sha256sum --check --strict SHA256SUMS`. Stop if
   the check fails or names a different archive. Inspect the file list with
   `tar -tvzf petar-v0.1.0.tar.gz` (replace the version with the downloaded
   one). Expect only the 12 regular files in the
   [release plan](../.opencode/tasks/04_php_plugin_release.md), under
   `petar/`, with no duplicate paths or links.
3. As a non-root user, extract into a new empty directory:
   `tar --extract --gzip --no-same-owner --file petar-v0.1.0.tar.gz --directory /path/to/empty-directory`.
   Mount `/path/to/empty-directory/petar` read-only at
   `/config/dokuwiki/lib/plugins/petar` in Docker. Keep the writable wiki
   configuration outside the plugin directory. Set `plugin.petar.petar_url`
   in DokuWiki to the backend base URL without `/api/v1`; grant use to
   appropriate `ai` or `admin` users. Check `{{petar}}` and `{{petar>jobs}}`
   in an isolated wiki before using the release elsewhere.

## Build and verify

From the source tree, use Bash, Python 3.8+, Git, Docker Compose v2,
GNU coreutils, Node.js and Go:

```bash
bash scripts/acceptance.sh
bash tests/release/package_test.sh
out=$(mktemp -d)
bash scripts/package-plugin.sh "$(git rev-parse HEAD)" v0.1.0 "$out"
bash scripts/check-plugin-release.sh "$out/petar-v0.1.0.tar.gz" "$out/SHA256SUMS"
```

The first acceptance uses the source checkout; the second uses only the
validated archive as the wiki's plugin. Both use disposable Compose
projects with no published ports. `--verify-only` checks the checksum and
archive format without Docker; it does not replace browser acceptance.
To repeat the published archive smoke without a checkout, copy
`scripts/{check-plugin-release.sh,plugin-release.py,acceptance.sh}` and
`tests/acceptance/` from the tested source into a separate harness directory,
preserving the paths, and pass the downloaded files to the check script.

## Publish

Before creating the first `vMAJOR.MINOR.PATCH` tag, confirm with the
repository owner that GitHub rules restrict creation of `v*` tags to
trusted release operators and prevent tag updates and deletion. Create
a new tag only on a commit from the published default branch (`master`
or `main`). Actions tests the tag and publishes the verified archive
and checksum from that run. Check that
the release lists the expected commit and that the assets are
downloadable without authentication for a public release.

If a job fails before creating a draft, diagnose it before rerunning on
the same tag. If a draft exists, the workflow refuses to overwrite it:
the maintainer must compare the draft, tag and tested assets before any
manual recovery. Do not replace assets of a published release or move
its tag. If the source needs changes, use a new version.

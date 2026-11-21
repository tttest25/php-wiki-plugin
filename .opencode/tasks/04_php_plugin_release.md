# DEPLOY-04 — verify and release the DokuWiki plugin

Add CI for PRs, branch pushes and new version tags. Preserve the
checkout quality gate and its isolated DokuWiki/fixture Chromium smoke.
Test installation independently by checking the archive SHA-256 and
running the same fixture smoke on a fresh wiki with only the extracted
`petar/` directory mounted read-only. No PHP or JavaScript runtime
change is part of this step.

Archive only these Git blobs, as ordinary `0644` files under `petar/`:

- `action.php`, `syntax.php`, `PetarClient.php`, `script.js`, `style.css`,
  `plugin.info.txt`
- `conf/default.php`, `conf/metadata.php`
- `lang/en/lang.php`, `lang/en/settings.php`, `lang/ru/lang.php`,
  `lang/ru/settings.php`

Build twice from the same commit and compare bytes, including when a
checkout has local alterations. Reject malformed archives, extra or
missing files, duplicate names, links and invalid checksums before
installation. The publish job receives exactly the archive and checksum
tested by CI at the tag's commit; PR jobs do not have publishing rights.

Require independent review and maintainer approval before commit, push
and release; run acceptance again on the downloaded release assets.
See [operator instructions](../../docs/plugin-release.md) for commands
and recovery policy.

# PHP-03: isolated DokuWiki acceptance

From a clean plugin checkout run:

```sh
./scripts/acceptance.sh
```

This creates a new Compose project on an internal network, runs the standard
quality gate against its DokuWiki, and runs one Chromium test. The browser logs in
as a temporary `ai` user, checks that the form is ready and its Submit button is
enabled, and verifies the seeded jobs row. It checks both real GETs to the wiki
AJAX endpoint and makes no submission. There is no host port, Browser MCP,
database, live backend, or external tool invocation.

`./scripts/acceptance.sh --contracts-only` skips the Playwright build and browser.
Use `./scripts/quality-gate.sh` on its own for the usual browser-free gate
against the `dokuwiki` container (or set `PETAR_TEST_WIKI_CONTAINER` explicitly).

## Requirements

Tested on Linux x86_64 (WSL2), with local Docker Engine 28.1.1, Compose v2.35.1,
Git 2.25.1, Bash 5.0.17, Node.js 20.20.2, Go 1.23.2 and GCC 9.4.0. Go needs
CGO and a C compiler for `-race`. The runner uses GNU coreutils, including
`timeout` and `stat`. The local Docker daemon must see the checkout at the
same absolute path, and the checkout must be readable by its owner in the
container. Initial use needs registry access (`lscr.io`, Docker Hub, Microsoft
Container Registry and `registry.npmjs.org`). No host npm packages or browser
are required.

The fixture images and official Chromium image are pinned by digest. The
browser build uses `npm ci` with the tracked lockfile and the matching Playwright
1.60.0 release. The checkout is mounted read-only at the same path used by the
plugin gate. Wiki configuration, users and pages live in the throwaway
`/config` volume; cache and sessions are disposable too. Only the image pull
and browser build need external network access.

To validate Compose interpolation without starting anything:

```sh
PETAR_ACCEPTANCE_CHECKOUT="$(pwd -P)" \
PETAR_ACCEPTANCE_UID="$(stat -c %u .)" \
PETAR_ACCEPTANCE_GID="$(stat -c %g .)" \
PETAR_ACCEPTANCE_PASSWORD=placeholder \
  docker compose --env-file /dev/null -f tests/acceptance/compose.yaml \
  --profile browser config --quiet
```

## Diagnosing failures

The runner prints the project name and an owner-only diagnostics directory under
`${TMPDIR:-/tmp}` (keep `TMPDIR` outside the checkout). Logs are grouped by stage.
`chromium.log` identifies the failed assertion and reports the AJAX methods/statuses
and browser error categories without dumping cookies, request bodies, console
message text, or authentication traces. `compose-ps.log` records container state
on failure. The runner keeps the original failure status and also treats a failed
cleanup as an error. The dependency images and build cache can be reused.

To check cleanup, substitute the project name printed by the runner; all three
commands should print nothing:

```sh
label="label=com.docker.compose.project=<printed-project>"
docker ps -aq --filter "$label"
docker network ls -q --filter "$label"
docker volume ls -q --filter "$label"
```

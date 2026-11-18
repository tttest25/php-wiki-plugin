#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == pre-init ]]; then
    # The nested bind creates /config/dokuwiki/lib/plugins before LSIO init,
    # so LSIO's first-run move would otherwise skip the built-in plugins.
    [[ ! -e /config/petar-bootstrap-ok && ! -e /app/www/public/lib/plugins/petar ]]
    cp -a /app/www/public/lib/plugins/. /config/dokuwiki/lib/plugins/
    exec /init
fi

cd /config/dokuwiki
[[ ! -e conf/local.php && ! -e conf/users.auth.php && ! -e conf/acl.auth.php ]]
test "$(readlink -f /app/www/public/lib/plugins/petar)" = /config/dokuwiki/lib/plugins/petar

cat > conf/local.php <<'PHP'
<?php if (!defined('DOKU_INC')) die();
$conf['title'] = 'Petar acceptance fixture';
$conf['lang'] = 'en';
$conf['useacl'] = 1;
$conf['authtype'] = 'authplain';
$conf['disableactions'] = 'register';
$conf['savedir'] = '/app/www/public/data';
$conf['plugin']['petar']['petar_url'] = 'http://fixture:8080';
PHP
php -l conf/local.php

{
    head -n 3 conf/acl.auth.php.dist
    printf '*\t@ALL\t0\npetar_smoke\t@ALL\t1\npetar_smoke\t@ai\t1\n'
} > conf/acl.auth.php

hash=$(php -r 'echo password_hash(getenv("PETAR_ACCEPTANCE_PASSWORD"), PASSWORD_BCRYPT);')
{
    head -n 3 conf/users.auth.php.dist
    printf 'petar_smoke:%s:Acceptance AI:petar@example.invalid:ai\n' "$hash"
} > conf/users.auth.php

cat > data/pages/petar_smoke.txt <<'WIKI'
====== Petar acceptance fixture ======

{{petar}}

{{petar>jobs}}
WIKI

chown abc:abc conf/local.php conf/acl.auth.php conf/users.auth.php data/pages/petar_smoke.txt
chmod 600 conf/local.php conf/acl.auth.php conf/users.auth.php
touch /config/petar-bootstrap-ok

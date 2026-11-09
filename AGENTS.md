# Coding & Engineering Rules: DokuWiki Plugin `petar` (BFF Proxy)

## 1. Environment & Infrastructure
- **OS & Shell:** Ubuntu 20.04 WSL (Zsh).
- **Project Root Host:** `/opt/php-wiki-plugin` (mounted into container `dokuwiki` at `/config/dokuwiki/lib/plugins/petar`).
- **Target Container:** `dokuwiki` (`lscr.io/linuxserver/dokuwiki:latest`, PHP 8.5.12, Alpine 3.24).
- **Backend Service:** `http://petar_app:8080` (or service alias `http://app:8080` on `petar_shared_net`, IP `172.21.0.3`).
- **Plugin Base Name:** `petar` (strictly lowercase).

## 2. Backend API Contract (`petar_app`)
1. **Readiness Probe (`GET /readyz`):**
   - Returns `200 OK` `{"status":"ready"}` or `503 Service Unavailable` `{"status":"unavailable", "error":"..."}`.
2. **Create Input Event (`POST /api/v1/inputs`):**
   - Request JSON schema:
     ```json
     {
       "source": "dokuwiki",
       "payload": {
         "tool": "yt-dlp | fabric | echo-test",
         "message": "<string>"
       }
     }
     ```
   - Allowed tools whitelist: `yt-dlp`, `fabric`, `echo-test`.
   - Response (`201 Created`):
     ```json
     {
       "input_id": "<uuid>",
       "process_id": "<uuid>",
       "status": "new"
     }
     ```
3. **Future Status/Result Endpoint (Extensibility):**
   - Structure `action.php` router with an `action` parameter (`readyz`, `submit`, and reserved `status` for future `GET` polling by `process_id` / `input_id`).
   - In `script.js`, store returned `input_id` and `process_id` in DOM `data-*` attributes of the result container to support future status polling.
4. **Tracing & Headers:**
   - Always generate and send `X-Request-ID` (UUIDv4) and return it to the frontend.
   - Always send `X-Wiki-User` (`REMOTE_USER`).
   - Send `X-API-Key` ONLY if `api_secret` config is non-empty (backend key validation is currently disabled).
   - Never expose `/api/v1/jobs/{id}/status` to the frontend (reserved for host `spool-runner.sh`).

## 3. DokuWiki & PHP 8.5+ Architecture & Security Standards
- **File Guard:** Every PHP file MUST begin with `if (!defined('DOKU_INC')) die();`.
- **No Standalone Entrypoints:** Route all frontend requests via `DOKU_BASE + 'lib/exe/ajax.php'` (`call=plugin_petar`) handled by `AJAX_CALL_UNKNOWN` in `action.php`.
- **CSRF Validation:** Validate every POST request in `action.php` via `checkSecurityToken()`.
- **Group ACL (`@ai` only):**
  - Check `$INPUT->server->str('REMOTE_USER')` is non-empty.
  - Verify user belongs to group `ai` (or `admin`) via:
    ```php
    global $USERINFO;
    $groups = (array)($USERINFO['grps'] ?? []);
    if (!in_array('ai', $groups, true) && !in_array('admin', $groups, true)) { ... }
    ```
  - Return `403 Forbidden` JSON if unauthorized.
- **HTTP Client:** Use strictly `\dokuwiki\HTTP\DokuHTTPClient` (`sendRequest()`). Never use `curl_*` or `file_get_contents()`.
- **Page Cache:** Set `$renderer->info['cache'] = false;` in `syntax.php` when rendering `getSecurityToken()`.
- **Logging:** Never write log files into the plugin directory. Use `\dokuwiki\Logger::error('petar', $message, __FILE__, __LINE__)` or `dbglog()`.

## 4. Workflow & Validation Protocol
1. **Atomic Commits:** Strictly 1 logical step = 1 commit.
2. **Syntax Check Command:** Validate every modified PHP file via Docker container:
   ```bash
   docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/<file>.php
   ```
3. **Cache Purge Command:** After updating `script.js`, `style.css`, or config metadata:
   ```bash
   docker exec dokuwiki touch /config/dokuwiki/conf/local.php
   ```

# Стандарты кодирования, безопасности и воркфлоу — плагин `petar`

Настоящий документ обязателен к прочтению перед каждым шагом разработки.
Любое несоответствие — блокер для коммита.

## 1. Окружение и инфраструктура

- **OS/Shell:** Ubuntu 20.04 WSL (Zsh).
- **Корень проекта (хост):** `/opt/php-wiki-plugin`, смонтирован в контейнер
  `dokuwiki` в `/config/dokuwiki/lib/plugins/petar`.
- **Контейнер:** `dokuwiki` (`lscr.io/linuxserver/dokuwiki:latest`,
  PHP 8.5.12, Alpine 3.24).
- **Backend:** `http://petar_app:8080` (алиас `http://app:8080`, сеть
  `petar_shared_net`, IP `172.21.0.3`, настраивается через конфиг).
- **Имя плагина:** `petar`, строго lowercase (в файлах, namespace, конфигах).

## 2. Стандарты PHP (8.5+)

1. **File Guard.** Каждый PHP-файл начинается строго с:
   `<?php if (!defined('DOKU_INC')) die();`
2. **Запрет standalone-эндпоинтов.** Все запросы фронтенда идут только через
   `DOKU_BASE . 'lib/exe/ajax.php'` с `call=plugin_petar`, обрабатываемый
   хуком `AJAX_CALL_UNKNOWN` в `action.php`. Собственные `$_GET`-роутеры,
   прямые PHP-скрипты плагина — запрещены.
3. **Только `\dokuwiki\HTTP\DokuHTTPClient`.** Запрещены `curl_*`,
   `file_get_contents()`, `fopen` по URL и любые иные HTTP-транспорты.
   Используется только `sendRequest()`.
4. **Конфигурация:** Параметры (`petar_url`, `api_secret`, `timeout`) читаются
   через `$this->getConf(...)` в плагинах и передаются в конструктор
   `PetarClient`. Запрещено хардкодить URL и таймаут внутри клиента.
5. **CSRF.** Каждый POST в `action.php` валидируется `checkSecurityToken()`.
   Токен рендерится в `syntax.php` и передаётся из `script.js` как `sectok`.
6. **ACL — только группы `ai` и `admin`:**
   - Проверяется как в контроллере `action.php` для AJAX, так и при рендере в
     `syntax.php`:
   - `$INPUT->server->str('REMOTE_USER')` непуст;
   - `$USERINFO['grps']` (приведённый к `(array)`) содержит `'ai'` или
     `'admin'` строгим сравнением `in_array(..., true)`;
   - Иначе — в `action.php` возвращается `403 Forbidden` JSON, в `syntax.php`
     выводится локализованное сообщение об отказе в доступе без формы и без
     `getSecurityToken()`.
7. **Отключение кэша страницы.** В `syntax.php` при рендере формы (где
   используется `getSecurityToken()`):
   `$renderer->info['cache'] = false;`
8. **Логирование.** Только `\dokuwiki\Logger::error('petar', $message, __FILE__, __LINE__)`
   или `dbglog()`. Файлы логов в каталоге плагина — запрещены.
9. **Современные базовые классы DokuWiki:**
   - `action.php` наследует `\dokuwiki\Extension\ActionPlugin`.
   - `syntax.php` наследует `\dokuwiki\Extension\SyntaxPlugin`.
   - Устаревшие `DokuWiki_*_Plugin` не используются.
10. **Стиль кода:** типизированные сигнатуры (PHP 8.5), строгое сравнение
    (`===`, `in_array(..., true)`), никаких суперглобалов (`$_POST`/`$_GET`)
    вне DokuWiki API (`$INPUT`), минимальные изменения существующего кода.

## 3. Безопасность сетевого слоя

- Заголовки только через `PetarClient`: `X-Request-ID` (UUIDv4),
  `X-Wiki-User` (серверный `REMOTE_USER`), `X-API-Key` — только если
  параметр `$apiSecret` не пуст.
- Белый список `tool`: `yt-dlp`, `fabric`, `echo-test` — строгое сравнение.
- Backend не доверяется: ответы декодируются с проверкой `json_decode(..., true)`
  и обязательной проверкой полей до использования.
- `/api/v1/jobs/{id}/status` не экспонируется фронтенду никогда
  (зарезервировано для `spool-runner.sh`).
- Секреты не попадают в вывод клиенту и в логи.

## 4. Обязательный воркфлоу на КАЖДОМ шаге

1. **Перед началом шага** агент обязан прочитать:
   `docs/architecture.md`, `docs/rules.md`, `docs/todo.md`.
2. **One Step = One Commit.** Строго один логический шаг из `docs/todo.md`
   = один коммит. Никаких дополнительных микро-коммитов (включая обновление
   `docs/todo.md` — отметка `[x]` для шага входит в тот же самый атомарный коммит
   шага).
3. **Синтаксическая проверка** каждого изменённого PHP-файла:
   ```bash
   docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/<file>.php
   ```
4. **Сброс кэша DokuWiki** после изменений в `script.js`, `style.css`
   или метаданных конфигурации:
   ```bash
   docker exec dokuwiki touch /config/dokuwiki/conf/local.php
   ```
5. **Изменения документации** (архитектура/правила) — только после согласования
   с пользователем в режиме `plan`.

## 5. Git-дисциплина

- Коммиты и пуш — только с явного подтверждения пользователя.
- Формат сообщения коммита — указан в `docs/todo.md` для каждого шага.
- В коммит шага входят только файлы, перечисленные в этом шаге `docs/todo.md`
  (вместе с обновлением `docs/todo.md`).

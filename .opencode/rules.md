# Контракт агента — плагин `petar` (DokuWiki BFF Proxy)

## 0. Главное правило (обязательный порядок работы)

Перед началом ЛЮБОЙ задачи агент обязан прочитать три документа и строго им
следовать:

1. `docs/architecture.md` — архитектура, схема BFF-потока, структура пакетов,
   API `PetarClient`.
2. `docs/rules.md` — стандарты кодирования, безопасности и воркфлоу.
3. `docs/todo.md` — атомарный план: текущий шаг, его файлы, критерии
   готовности и текст коммита.

После выполнения шага агент отмечает его в `docs/todo.md`
(`[ ]` → `[x]`, с хешем коммита) — **внутри того же самого атомарного коммита
шага** (никаких отдельных микро-коммитов для документации).

## 1. Сводка контракта (кратко, детали — в docs/)

- **Плагин:** `petar` (strict lowercase). **Backend:** настраиваемый `petar_url`
  (по умолчанию `http://petar_app:8080`).
- **Структура пакетов:** сервисный слой только в `classes/PetarClient.php`,
  namespace `dokuwiki\plugin\petar\PetarClient` (автозагрузка DokuWiki);
  контроллер в `action.php` (`\dokuwiki\Extension\ActionPlugin`);
  синтаксис в `syntax.php` (`\dokuwiki\Extension\SyntaxPlugin`); фронтенд —
  `script.js` + `style.css`; конфиг — `conf/`; локализация — `lang/`
  (`ru`, `en`).
- **Поток:** `script.js` → `lib/exe/ajax.php` (`call=plugin_petar`) →
  `action.php` (`AJAX_CALL_UNKNOWN`, CSRF, ACL `@ai`/`@admin`) →
  `PetarClient` → backend. Standalone-эндпоинтов нет.
- **HTTP:** только `\dokuwiki\HTTP\DokuHTTPClient`. Запрещены `curl_*` и
  `file_get_contents()`. Заголовки: `X-Request-ID` (UUIDv4), `X-Wiki-User`,
  `X-API-Key` (только если `api_secret` не пуст). Белый список инструментов:
  `yt-dlp`, `fabric`, `echo-test`.
- **Безопасность:** File Guard в каждом PHP
  (`if (!defined('DOKU_INC')) die();`); CSRF `checkSecurityToken()` на POST;
  ACL только `ai`/`admin` на уровне AJAX и рендера синтаксиса (403 JSON или
  текст ошибки доступа без формы); `$renderer->info['cache'] = false`
  в `syntax.php`; логи только через `\dokuwiki\Logger::error('petar', ...)` —
  никаких лог-файлов в каталоге плагина.
- **Воркфлоу:** One Step = One Commit (отметка в `docs/todo.md` входит в коммит
  шага); `php -l` через
  `docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/<file>.php`;
  после правок `script.js`/`style.css`/конфиг-метаданных —
  `docker exec dokuwiki touch /config/dokuwiki/conf/local.php`.
- **Git:** коммиты/пуш только по явному подтверждению пользователя.

Изменения архитектуры или правил — только через согласование в режиме `plan`.

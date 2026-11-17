# План разработки плагина `petar` — атомарный чек-лист

Порядок обязателен. Перед каждым шагом прочитать `docs/architecture.md` и
`docs/rules.md`. После шага — `php -l` для каждого PHP-файла шага и отметка
`[x]` с уникальным subject в том же атомарном коммите; SHA определяется через
`git log --format='%H %s'`. Пользователь явно согласовал это правило и создание
пяти коммитов вместо невозможного self hash.

---

## Шаг 0 — Инициализация документации и контракта агента [x]

**Файлы шага:**
- `docs/architecture.md` (новый)
- `docs/rules.md` (новый)
- `docs/todo.md` (новый, с отметкой Шага 0 как `[x]`)
- `AGENTS.md` (перезапись — краткий контракт-индекс)
- `.opencode/rules.md` (новый, идентичен `AGENTS.md`)

**Критерии готовности:**
- Все 5 файлов существуют; `AGENTS.md` ≡ `.opencode/rules.md` (байт-в-байт).
- `AGENTS.md` обязывает читать `docs/architecture.md`, `docs/rules.md`,
  `docs/todo.md` перед любой задачей.
- PHP-файлы на этом шаге не создаются → `php -l` не требуется.

**Команда проверки:** визуальная сверка с чатом-согласованием.

**Коммит:** `docs: add architecture, rules, todo and agent contract`

---

## Шаг 1 — Метаданные, конфигурация и локализация [x]

**Файлы шага:**
- `plugin.info.txt`
- `conf/default.php`
- `conf/metadata.php`
- `lang/en/settings.php`
- `lang/ru/settings.php`
- `lang/en/lang.php`
- `lang/ru/lang.php`
- `docs/todo.md` (обновление отметки Шага 1 в тот же коммит)

**Содержание:**
- `plugin.info.txt`: `base petar`, имя/описание/автор/дата/требования ядра.
- `conf/default.php`: параметры `petar_url` (`http://petar_app:8080`),
  `api_secret` (`''`), `timeout` (`15`) (+ guard `DOKU_INC`).
- `conf/metadata.php`: метаданные конфигурации для админки DokuWiki.
- `lang/*/settings.php`, `lang/*/lang.php`: строки RU и EN.

**Критерии готовности:**
- Все файлы созданы, File Guard в каждом PHP.
- Конфигурационные параметры присутствуют и корректны.
- Конфигурация плагина появляется в админке DokuWiki, кэш сброшен.

**Команды проверки:**
```bash
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/conf/default.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/conf/metadata.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/en/settings.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/ru/settings.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/en/lang.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/ru/lang.php
docker exec dokuwiki touch /config/dokuwiki/conf/local.php
```

**Коммит:** `feat: add plugin metadata, config and localization`

---

## Шаг 2 — Сервисный слой: API-клиент [x]

**Файлы шага:**
- `PetarClient.php`
- `docs/todo.md` (обновление отметки Шага 2 в тот же коммит)

**Содержание (по `docs/architecture.md` §4):**
- `namespace dokuwiki\plugin\petar;`, `final class PetarClient`.
- Конструктор `__construct(string $baseUrl, string $apiSecret = '', int $timeout = 15)`.
- Инкапсуляция `\dokuwiki\HTTP\DokuHTTPClient`.
- `checkReadiness(): array` — `GET /readyz`.
- `submitInput(string $tool, string $message, string $user, ?string $requestId = null): array`
  — `POST /api/v1/inputs`, белый список `ALLOWED_TOOLS`, заголовки
  `X-Request-ID` / `X-Wiki-User` / опциональный `X-API-Key`.
- `getStatus(string $id): array` — зарезервированная заглушка
  (бросает `RuntimeException('Not implemented')`).
- Логирование через `\dokuwiki\Logger::error('petar', ...)`.

**Критерии готовности:**
- File Guard присутствует; класс проходит автозагрузку DokuWiki.
- Негативные тесты (через `dbglog`): невалидный `tool` → исключение;
  пустой `api_secret` → заголовок `X-API-Key` не отправляется.

**Команда проверки:**
```bash
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/PetarClient.php
```

**Коммит:** `feat: add PetarClient service layer (BFF API client)`

---

## Шаг 3 — Серверный контроллер BFF-прокси [x]

**Файлы шага:**
- `action.php`
- `docs/todo.md` (обновление отметки Шага 3 в тот же коммит)

**Содержание:**
- Наследование `\dokuwiki\Extension\ActionPlugin`.
- Регистрация хука `AJAX_CALL_UNKNOWN`.
- Роутинг `action=readyz|submit` (резерв `status`).
- Чтение конфига через `$this->getConf(...)` и создание `PetarClient`.
- CSRF через `checkSecurityToken()` для POST → `403` JSON.
- ACL `@ai`/`@admin` → `403` JSON.
- Делегирование в `PetarClient`, JSON-ответ + `X-Request-ID`.

**Критерии готовности:**
- File Guard; аноним → `403`; пользователь без группы `ai`/`admin` → `403`;
  POST без `sectok` → `403`; `readyz` возвращает JSON backend.
- Использование современного базового класса `\dokuwiki\Extension\ActionPlugin`.

**Команда проверки:**
```bash
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/action.php
```

**Коммит:** `feat: add BFF proxy controller in action.php`

---

## Шаг 4 — Синтаксический плагин и UI-разметка [x]

**Файлы шага:**
- `syntax.php`
- `docs/todo.md` (обновление отметки Шага 4 в тот же коммит)

**Содержание:**
- Наследование `\dokuwiki\Extension\SyntaxPlugin`.
- Паттерн `{{petar}}`.
- Проверка ACL (`REMOTE_USER` + группы `ai`/`admin`) при рендере: при отказе
  выводится локализованное сообщение об ошибке доступа без формы и без
  `getSecurityToken()`.
- Рендер XHTML: форма (select tool, textarea, submit), скрытое поле `sectok`
  через `getSecurityToken()`.
- **Обязательно:** `$renderer->info['cache'] = false;`.

**Критерии готовности:**
- File Guard; страница с `{{petar}}` проверяет ACL и рендерит форму;
  присутствует уникальный `sectok`; кэш страницы отключён.
- Использование современного базового класса `\dokuwiki\Extension\SyntaxPlugin`.

**Команда проверки:**
```bash
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/syntax.php
```

**Коммит:** `feat: add syntax plugin with ACL check, form markup and cache disable`

---

## Шаг 5 — Клиентская логика и стили [x]

**Статус:** выполнен; ссылка — subject `feat: add frontend submit logic and styles`
в том же атомарном коммите, SHA — через `git log`. Технический промежуточный снимок
реконструирован; это не точная копия исторических исходников. Клиент ожидает
плоский ответ; развёртывание серверного конверта относится к следующему Шагу 6.
Проверка: `node --check script.js`. Снимок не развёрнут, кэш DokuWiki не изменялся.
Baseline 01 не является самостоятельно развёртываемой версией: полная
интеграционная приёмка требует Шага 6; критерии ниже сохраняют целевой контракт.

**Файлы шага:**
- `script.js`
- `style.css`
- `docs/todo.md` (обновление отметки Шага 5 в тот же коммит)

**Содержание:**
- `script.js`: на submit формы — `fetch(DOKU_BASE + 'lib/exe/ajax.php',
  {call: 'plugin_petar', action: 'submit', sectok, tool, message})`;
  предварительный `action=readyz` для блокировки кнопки; рендер результата
  в контейнер с `data-input-id` и `data-process-id` (из ответа backend);
  вывод `X-Request-ID` в консоль/DOM для отладки.
- `style.css`: стили формы, контейнера результата, состояний
  loading/error/success.

**Критерии готовности:**
- Отправка формы из браузера возвращает `{"input_id","process_id","status"}`
  и рендерит их в DOM; атрибуты `data-input-id` / `data-process-id` присутствуют.
- После обновления файлов выполнен `touch conf/local.php`.

**Команда проверки:**
```bash
docker exec dokuwiki touch /config/dokuwiki/conf/local.php
```

**Коммит:** `feat: add frontend submit logic and styles`

---

## Шаг 6 — Интеграционные исправления E2E [x]

Выполнен; уникальный subject указан ниже и входит в тот же коммит, SHA — через
`git log`. Технический промежуточный снимок реконструирован, не является
точной копией исторического dirty tree. Шаги A/B/C сюда не входят.
Файлы: перенос classes/PetarClient.php → PetarClient.php, action.php,
syntax.php, docs/architecture.md, docs/rules.md, docs/todo.md,
AGENTS.md, .opencode/rules.md. Изменения .gitignore исключены.
Клиент загружается из корня; контроллер раскрывает data-конверт;
lexer использует штатную категорию substitution и приоритет 305.
Коммит: `fix: E2E integration fixes (lexer priority, autoload path, response envelope)`.

## Phase 1 — A. Канонический payload [x]

Выполнен в атомарном коммите с subject ниже; SHA — через `git log`.
Реализован только шаг A, B/C ещё впереди.
Файлы: PetarClient.php, action.php, tests/contract/client_test.go,
docs/architecture.md, docs/todo.md.
Критерии: source=dokuwiki, echo/message, yt-dlp/url, fabric/text/pattern;
совместимость старого вызова, строгий 201/UUID/status=new, безопасные ошибки.
Тесты: `GO111MODULE=off go test -count=1 -race ./tests/contract`.
Readiness с единым ID и новый контроллер остаются шагом B.
Коммит: `fix: map plugin tool inputs to canonical payloads`.

## Журнал выполненных шагов

| Шаг | Коммит | Дата | Примечания |
|-----|--------|------|------------|
| 0   | 6a6364c| 2026-11-10 | `[x]` Инициализация документации |
| 1   | 02acc57| 2026-11-10 | `[x]` Метаданные, конфигурация и локализация |
| 2   | b44fa95| 2026-11-10 | `[x]` Сервисный слой: API-клиент (`PetarClient.php`) |
| 3   | 1d88a41| 2026-11-10 | `[x]` Серверный контроллер BFF-прокси (`action.php`) |
| 4   | 29a7998| 2026-11-10 | `[x]` Синтаксический плагин и UI-разметка (`syntax.php`) |
| 5   | `feat: add frontend submit logic and styles` | — | `[x]` SHA через git log; реконструированный baseline 01 |
| 6   | `fix: E2E integration fixes (lexer priority, autoload path, response envelope)` | — | `[x]` SHA через git log |
| A   | `fix: map plugin tool inputs to canonical payloads` | — | `[x]` SHA через git log; без B/C |

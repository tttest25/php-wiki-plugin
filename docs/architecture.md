# Архитектура плагина `petar` (BFF Proxy для DokuWiki)

## 1. Назначение

Плагин `petar` — backend-for-frontend (BFF) прокси между DokuWiki и внутренним
сервисом `petar_app` (настраиваемый через конфиг `petar_url`, по умолчанию
`http://petar_app:8080` / `http://app:8080` на сети `petar_shared_net`, IP
`172.21.0.3`). Плагин не содержит бизнес-логики обработки задач: он валидирует,
аутентифицирует и маршрутизирует запросы от авторизованных пользователей группы
`@ai` (или `@admin`) к backend API и возвращает результаты в браузер.

Цели архитектуры:

- **Единая точка входа**: только `DOKU_BASE + 'lib/exe/ajax.php'`
  (`call=plugin_petar`), обрабатываемый современным плагином расширений
  `\dokuwiki\Extension\ActionPlugin`.
- **Разделение ответственности**: DokuWiki-хуки в `action.php`, сетевая логика —
  только в сервисном слое `PetarClient.php`.
- **Расширяемость**: зарезервированные маршруты/методы под будущий
  status/result-поллинг.
- **Безопасность**: CSRF, ACL `@ai`/`@admin` на уровне контроллера и рендера
  синтаксиса, безопасный HTTP-клиент, отсутствие standalone-эндпоинтов.

## 2. Схема потока запроса (BFF Proxy)

```
Браузер
  │  POST/GET  DOKU_BASE + lib/exe/ajax.php?call=plugin_petar&action=...
  ▼
lib/exe/ajax.php (ядро DokuWiki, точка входа единственная)
  │  событие AJAX_CALL_UNKNOWN, параметр call = plugin_petar
  ▼
action.php  (класс action_plugin_petar extends \dokuwiki\Extension\ActionPlugin)
   ├── единый UUIDv4 request ID до ACL/CSRF
   ├── checkSecurityToken()          // CSRF, для всех POST
  ├── ACL: REMOTE_USER + группы @ai/@admin   // 403 Forbidden JSON
  ├── инициализация PetarClient(petar_url, api_secret, timeout) из getConf()
  ├── маршрутизация по параметру action:
   │     readyz  → PetarClient::checkReadiness(requestId?)
    │     submit  → PetarClient::submitInput(tool, message, user, requestId?, pattern?)
   │     jobs    → PetarClient::listJobs(user, status?, cursor?, requestId?)
  │     status  → ЗАРЕЗЕРВИРОВАНО (будущий GET-поллинг; НЕ /api/v1/jobs/{id}/status)
  └── формирование JSON-ответа клиенту (+ X-Request-ID в ответ)
  ▼
PetarClient.php  (namespace dokuwiki\plugin\petar, класс PetarClient)
  │  \dokuwiki\HTTP\DokuHTTPClient (sendRequest), БЕЗ curl_*/file_get_contents
  │  заголовки: X-Request-ID (UUIDv4), X-Wiki-User, X-API-Key (только если api_secret != '')
  │  валидация tool по белому списку: yt-dlp | fabric | echo-test
  ▼
backend petar_app (параметризованный URL)
  GET  /readyz          → 200 {"status":"ready"} | 503 {"status":"unavailable","error":"..."}
   POST /api/v1/inputs   → 201 {"input_id","process_id","status":"new"}
   GET  /api/v1/jobs    → 200 [job, ...]
```

Критические правила потока:

- Браузер **никогда** не обращается к backend напрямую.
- Браузеру **не экспонируется** `/api/v1/jobs/{id}/status` (зарезервировано для
  хост-скрипта `spool-runner.sh`).
- Каждый ответ прокси включает `X-Request-ID` (сгенерированный или проброшенный),
  для сквозной трассировки.

## 3. Дерево директорий и файлов проекта

Корень проекта (хост): `/opt/php-wiki-plugin`,
смонтирован в контейнер `dokuwiki` как
`/config/dokuwiki/lib/plugins/petar`.

```
/opt/php-wiki-plugin/
├── AGENTS.md                  # Главный контракт для агента (индекс документации)
├── .opencode/
│   └── rules.md               # Идентично AGENTS.md
├── docs/
│   ├── architecture.md        # Данный файл: архитектура, поток, структура пакетов
│   ├── rules.md               # Стандарты кодирования, безопасности, воркфлоу
│   └── todo.md                # Атомарный чек-лист разработки (обновляется в каждом шаге)
│
├── plugin.info.txt            # Метаданные плагина (base: petar, строго lowercase)
├── action.php                 # Контроллер BFF: AJAX_CALL_UNKNOWN, CSRF, ACL, роутинг
├── syntax.php                 # ACL и рендер формы {{petar}} или журнала {{petar>jobs}}
├── script.js                  # Клиентская логика: fetch → ajax.php, рендер результата
├── style.css                  # Стили формы и контейнера результата
│
├── PetarClient.php            # Сервисный слой (namespace dokuwiki\plugin\petar)
│
├── conf/
│   ├── default.php            # Значения по умолчанию (petar_url, api_secret, timeout)
│   └── metadata.php           # Метаданные конфигурации для админки DokuWiki
│
└── lang/
    ├── en/
    │   ├── settings.php       # Строки конфигурации (EN)
    │   └── lang.php           # UI-строки плагина (EN)
    └── ru/
        ├── settings.php       # Строки конфигурации (RU)
        └── lang.php           # UI-строки плагина (RU)
```

## 4. Сервисный слой: `PetarClient.php`

### 4.1. Общие положения

- Namespace: `dokuwiki\plugin\petar`, класс: `PetarClient`.
- Автозагрузка — штатный автолоадер DokuWiki: сегменты namespace маппятся 1:1
  в путь от корня плагина (`dokuwiki\plugin\petar\PetarClient` →
  `lib/plugins/petar/PetarClient.php`; каталог `classes/` НЕ участвует),
  без `require`/`include` вручную.
- Единственный способ HTTP — инкапсулированный `\dokuwiki\HTTP\DokuHTTPClient`
  (метод `sendRequest()`). Запрещены `curl_*`, `file_get_contents()` и любые
  иные транспорты.
- Конфигурация (`petar_url`, `api_secret`, `timeout`) передается в конструктор
  клиента из контроллера `action.php` (получающего их через `$this->getConf(...)`).
- Ошибки не глотаются: исключения/массивы с полем `error` фиксируются через
  `\dokuwiki\Logger::error('petar', $msg, __FILE__, __LINE__)`;
  лог-файлы в каталоге плагина запрещены.

### 4.2. Трассировка и заголовки

Метод формирования заголовков (приватный хелпер):

- `X-Request-ID`: UUIDv4. Контроллер сохраняет валидный входной ID, иначе
  генерирует новый через `random_bytes()` до ACL/CSRF. Один ID используется
  в JSON `request_id`, заголовке ответа и запросе backend; sectok не является ID.
  Клиент валидирует переданный ID, при отсутствии генерирует новый.
- `X-Wiki-User`: значение `$INPUT->server->str('REMOTE_USER')`, проброшенное
  из `action.php` (клиент не может его подменить).
- `X-API-Key`: отправляется **только если** `$apiSecret` не пуст;
  backend-проверка ключа в настоящий момент отключена, но контракт сохраняется.

### 4.3. Белый список инструментов

```php
private const ALLOWED_TOOLS = ['yt-dlp', 'fabric', 'echo-test'];
```

- `tool` валидируется строго (`in_array($tool, self::ALLOWED_TOOLS, true)`).
- Невалидный `tool` → исключение `InvalidArgumentException`, в `action.php`
  маппится в `400 Bad Request`.

### 4.4. Публичный API класса

| Метод | HTTP backend | Описание |
|---|---|---|
| `checkReadiness(?string $requestId = null): array` | `GET /readyz` | Успех только при HTTP 200 и объекте с `status=ready`; возвращает нормализованный конверт. BFF отображает неготовность как 503 `status=unavailable`, без сырой ошибки backend. |
| `submitInput(string $tool, string $message, string $user, ?string $requestId = null, ?string $pattern = null): array` | `POST /api/v1/inputs` | Маппинг канонического payload выполняется только в клиенте. Возвращает конверт `success,http_status,request_id,data,error`; `data` содержит проверенные UUID input/process и `status=new`. |
| `listJobs(string $user, ?string $status = null, ?string $cursor = null, ?string $requestId = null): array` | `GET /api/v1/jobs` | Общая страница без фильтра по пользователю. Возвращает `jobs` и `next_cursor`, при сбое — безопасный 502 через контроллер. |
| `getStatus(string $id): array` | `GET /api/v1/inputs/{id}` (**ЗАРЕЗЕРВИРОВАНО**) | Заготовка под будущий поллинг статуса/результата по `process_id`/`input_id`. В Шаге 2 реализуется как метод-заглушка (выбрасывает `RuntimeException` или возвращает структуру-заглушку), не экспонируется во фронтенд до отдельного согласования. |

Сигнатуры фиксированы настоящим документом; изменения — только через согласование
в режиме `plan`.

### 4.5. Контракты backend API

`POST /api/v1/inputs`:

- Запрос: `source=dokuwiki`; payload зависит от инструмента:
  - `echo-test`: `{"tool":"echo-test","message":"<string>"}`
  - `yt-dlp`: `{"tool":"yt-dlp","url":"https://example.org/video"}`
  - `fabric`: `{"tool":"fabric","text":"<string>","pattern":"summarize"}`
  BFF по-прежнему принимает `message`; для fabric добавлено необязательное поле
  `pattern`, отсутствующее означает `summarize`. Пустое/нестроковое значение
  отклоняется; имя соответствует `[A-Za-z0-9_-]{1,128}`.
  Нерелевантные поля не отправляются, текст echo/fabric сохраняется без trim.
  URL — абсолютный HTTP/HTTPS с host (не гарантия защиты от SSRF).
- Ответ `201 Created`: `{"input_id":"<uuid>","process_id":"<uuid>","status":"new"}`
  UUID не ограничены v4 (поддерживается v7). Прямой HTTP 400 от backend →
  безопасный отказ `400 Input rejected` без передачи тела ответа. Другой код,
  неверная структура или потеря ответа → безопасный 502; локальная ошибка
  пользовательского ввода → 400.

`GET /readyz`:

- `200 OK`: `{"status":"ready"}`; `503`: `{"status":"unavailable","error":"..."}`.

`GET /api/v1/jobs`:

- Только `limit=21` и необязательные `status` (`pending`, `dispatched`,
  `in_progress`, `completed`, `failed`) и `cursor` (UUID, включая v7).
  Запрос без тела, переходы по редиректам запрещены.
- Только HTTP 200 и JSON-массив валидных объектов; `[]` — пустой список.
  В BFF выдаются строго `job_id` (`id`), `process_id`, `tool` (`tool_name`),
  `status`, `created_at`, `updated_at`. UUID и даты RFC3339 проверяются.
- Порядок backend сохраняется, строки не группируются по процессу. При 21-й
  строке показываются первые 20; исключающий курсор — ID 20-й строки. При 0–20
  строках `next_cursor: null`. Успешный BFF-ответ добавляет `request_id` и
  `Cache-Control: no-store`. Пагинация не является снимком обновляемого журнала.

## 5. Разделение ответственности

### `action.php` (контроллер)

Отвечает **только** за:

1. Регистрацию хука `AJAX_CALL_UNKNOWN` (наследуя `\dokuwiki\Extension\ActionPlugin`).
2. Проверку `call == 'plugin_petar'`.
3. Маршрутизацию по параметру `action` (`readyz`/`jobs` только GET, `submit` только POST,
   иначе 405 с Allow). `status` и неизвестные действия → 404 без вызова backend.
4. CSRF: `checkSecurityToken()` для каждого POST → `403` JSON при провале.
5. ACL: `$INPUT->server->str('REMOTE_USER')` непуст + `$USERINFO['grps']`
   содержит `ai` или `admin` → иначе `403 Forbidden` JSON.
6. Чтение конфигурации через `$this->getConf(...)` (`petar_url`, `api_secret`,
   `timeout`) и инстанцирование `PetarClient`.
7. Извлечение входных параметров через `$INPUT`/`get`/`post`/`server`, отклонение
   массивов вместо приведения к строке, передачу в `PetarClient`.
8. Формирование JSON-ответа: `Content-Type: application/json`,
   `X-Request-ID` (проброшенный или новый UUIDv4), корректные HTTP-коды
   (200/400/403/502 по результату).

НЕ содержит: HTTP-клиент, curl, бизнес-правила валидации backend-данных,
разбор ответов `petar_app` (только передача результата `PetarClient` клиенту).

### `PetarClient.php` (сервисный слой)

Содержит: всё сетевое взаимодействие, заголовки, белый список `tool`,
парсинг/валидацию ответов backend, логирование ошибок.

### `syntax.php`

Содержит: наследование `\dokuwiki\Extension\SyntaxPlugin`, проверку ACL
(`REMOTE_USER` + группы `ai`/`admin`) непосредственно перед рендером
(при отсутствии прав выводится локализованное сообщение об ошибке доступа без формы
и без `getSecurityToken()`), парсинг точных макросов `{{petar}}` и `{{petar>jobs}}`,
рендер формы с `getSecurityToken()` или пустого журнала без токена. В обоих
режимах, включая отказ ACL, `$renderer->info['cache'] = false;`. Без сетевой логики.

### `script.js` / `style.css`

`script.js`: `fetch` к `DOKU_BASE + lib/exe/ajax.php` (`call=plugin_petar`,
`action=readyz|submit|jobs`), отправка `sectok` только для submit, рендер результата. Полученные
`input_id` и `process_id` сохраняются в DOM-атрибуты контейнера результата:
`data-input-id` и `data-process-id` (под будущий поллинг `status`).
`style.css`: только стили, без логики.

Каждый виджет имеет уникальные id и независимое состояние. Кнопка submit
заблокирована уже в HTML и доступна только после успешной readiness.
Форма переключает подписи/подсказки для message и показывает pattern для fabric.
Есть ручная перепроверка readiness; fetch ограничен 15 секундами через
AbortController. Отмена ожидания не гарантирует отмену создания задания:
автоматических повторов submit нет. Все UI-строки RU/EN передаются через
экранированные data-атрибуты; вывод данных через textContent.

Каждый журнал загружает первую страницу без фильтра, затем поддерживает пять
статусов, «Обновить» и «Ещё». Смена фильтра/обновление сбрасывают курсор;
«Ещё» дописывает строки. Счётчик поколения на виджет не даёт старому ответу
изменить текущие строки, курсор или управление.

## 6. Расширяемость (резервы)

- Маршрут `action=status` в `action.php` — зарезервирован, не активируется
  до отдельного согласования.
- `PetarClient::getStatus()` — метод-заглушка.
- `data-input-id` / `data-process-id` в DOM — якоря будущего фронтенд-поллинга.

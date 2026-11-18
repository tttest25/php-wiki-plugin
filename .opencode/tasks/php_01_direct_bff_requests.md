# PHP-01 — прямые запросы BFF без редиректов

`PetarClient` будет обращаться только к исходному URL backend: ответ с перенаправлением не станет успешной проверкой готовности или созданием задания. Прямые 200 для readiness и 201 для submit сохранят прежние ответы; журнал продолжит работать с уже установленным запретом.

## Выбор и границы

Плагин: `/opt/php-wiki-plugin`. Источник — существующий открытый PHP-01 в `docs/todo.md:309` и `docs/php_wiki_jobs_roadmap.md:5`. Зависимостей нет. Особый приоритет первой итерации PHP-04 уже отработан (`dec77ad`); PHP-03 также закрыт (`fcb1867`). Их флажки в исходной карте отстают от todo и истории. PHP-02 (классификация исхода POST), новые маршруты, UI и Go backend вне шага.

Файлы атомарного шага в репозитории плагина: `PetarClient.php`, `tests/contract/client_test.go`, `docs/todo.md`, `.opencode/tasks/php_01_direct_bff_requests.md` (локальная копия для рабочей ссылки todo). Внешняя архивная копия по поручению пользователя — `/opt/go-pet/.opencode/tasks/php_01_direct_bff_requests.md`; не включать изменения Go-репозитория в коммит плагина. Не захватывать существующие посторонние изменения `docs/rules.md` и `.playwright-mcp/`.

## Проверено при планировании

- `PetarClient.php::checkReadiness` и `submitInput` создают `DokuHTTPClient` без ограничения переходов; сравнивают только полученный после `sendRequest` статус с 200/201. Контроллер `action.php` уже преобразует ошибку readiness в 503 `unavailable`, исключение backend при submit — в безопасный 502.
- В работающем `dokuwiki` проверен исходник `/app/www/public/inc/HTTP/HTTPClient.php`: лимит по умолчанию 3; при 301/302/303/307/308 с `Location` клиент проверяет лимит перед новым запросом. Для 307/308 сохраняет метод. Значение 0 останавливает первый переход.
- `listJobs` уже ставит `max_redirect = 0` и `debug = false`. В `tests/contract/client_test.go` тест `TestJobsClient` проверяет эти настройки, 302 и прямой успех. `clientFixture` фиксирует `max_redirect`, но сам не переходит по `Location`: подача одного 302 в эту заглушку **не** доказывает отсутствие второго запроса. `TestBFFBranches` в `action_test.go` уже покрывает штатные ответы и отображение ошибок.
- RED/GREEN не запускались: ниже прогноз для новых тестов, а не протокол прогона.

## TDD и приёмка

1. **Первый тест — `TestDirectBFFRequests/ready_redirect`.** В `client_test.go` вызвать настоящий `PetarClient::checkReadiness` со штатным транспортом после bootstrap DokuWiki. Внутри тестового wiki-контейнера поднять временный loopback HTTP-стенд: `/readyz` отдаёт 302 и `Location` на локальный маршрут, который считает обращения и возвращает валидный 200 `{"status":"ready"}`. Проверить одно обращение к исходному URL, ноль ко второму и `success=false`. Использовать свободный порт, ограниченное ожидание старта, отдельную health-пробу, завершение сервера и очистку временных файлов при любом исходе; ориентир для запуска PHP-сервера — `TestBFFHTTPHeaders`. Не использовать адрес реального backend или хостовые порты. Ожидаемый RED — второй маршрут вызван и readiness успешна; ошибка запуска стенда не считается RED. GREEN — установить `max_redirect=0` до `sendRequest` в `checkReadiness`.
2. **`TestDirectBFFRequests/submit_redirect`.** На `/api/v1/inputs` принять POST, ответить 307 с `Location` на маршрут, возвращающий валидный 201 с UUID и `status=new`. Проверить ровно один исходный POST, ноль обращений к цели любым методом и `RuntimeException` с кодом 502 без тела upstream. Ожидаемый RED — цель посещена и submit принят. GREEN — применить тот же лимит до отправки в `submitInput`.
3. **Положительные контроли** `ready_direct`, `submit_direct` на том же стенде: прямые 200/201 дают `success=true`, `status=ready` либо исходные UUID и `status=new`, сохраняется переданный request ID. Ожидаются зелёными без дополнительной реализации. На зелёных тестах вынести создание транспорта, таймаут и нулевой лимит в один приватный метод `PetarClient`, использовать во всех трёх методах, сохранив маршрутные заголовки и локальную настройку debug журнала. Проверить `TestJobsClient` и `TestBFFBranches`; если нарушены прежние статусы, заголовки или JSON, исправить рефакторинг, не менять контракт. Проверки конечного статуса оставить: даже не обрабатываемый транспортом код 3xx не является 200/201.

## Запуск и завершение

Из `/opt/php-wiki-plugin`; Git, Bash, Go с `-race`, Node.js, Docker и Compose, работающий `dokuwiki` с этим checkout в `/config/dokuwiki/lib/plugins/petar`. Для полного smoke нужны образы стенда PHP-03. Для шага не нужны PostgreSQL, DSN, миграции или реальные задания: контрактные запросы только на loopback, полный smoke использует одноразовый Compose и фикстуры.

1. `GO111MODULE=off go test -count=1 -v ./tests/contract -run '^TestDirectBFFRequests$/^ready_redirect$'` — после написания первого теста, затем после исправления. Убедиться, что именованный подтест действительно выполнен.
2. `GO111MODULE=off go test -count=1 -v ./tests/contract -run '^TestDirectBFFRequests$/^submit_redirect$'` — тот же порядок.
3. `GO111MODULE=off go test -count=1 -v ./tests/contract -run '^(TestDirectBFFRequests|TestJobsClient|TestBFFBranches)$'` — все сценарии после рефакторинга.
4. `docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/PetarClient.php`; `./scripts/quality-gate.sh` — обязательный локальный gate плагина (включая diff, lint, JS, vet и полный race-набор).
5. `TMPDIR=/tmp/opencode ./scripts/acceptance.sh` — полная изолированная приёмка со smoke; сохранить фактические результаты и путь диагностики при сбое.
6. Обязательный независимый `review`: передать diff и критерии, проверить отсутствие второго запроса, сохранение заголовков/статусов после общей настройки транспорта и достоверность сетевого теста. Устранить существенные замечания, повторить затронутые проверки. Архивировать неизменный текст плана под тем же именем; при необходимости только дописать в конце «Фактически выполнено» с существенными отклонениями и результатами.
7. После приёмки отметить существующую строку `[x]`, добавить уникальный subject `fix: disable redirects for BFF backend requests`. Показать итоговый diff и фактические результаты пользователю; только после явного Human Sign-off — один атомарный коммит с todo. Push требует отдельного разрешения.

## Строка для `docs/todo.md`

Заменить существующую строку PHP-01, не добавлять вторую. Оставить открытой до приёмки:

- [ ] **PHP-01: Прямые запросы BFF без редиректов.** Readiness и submit не переходят по Location; цель не получает запрос, 3xx не становится успехом, прямые 200/201 и политика журнала сохранены. План: [.opencode/tasks/php_01_direct_bff_requests.md](../.opencode/tasks/php_01_direct_bff_requests.md).

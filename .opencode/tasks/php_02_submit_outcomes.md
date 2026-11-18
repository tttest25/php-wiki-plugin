# PHP-02 — Отличать отказ от неизвестного исхода submit

Прямой HTTP 400 от backend для валидно отправленного запроса будет означать для пользователя «ввод отклонён, исправьте его». Если ответ потерян, форма предупредит, что задание могло быть создано; повторного POST без действия пользователя не будет.

## Почему сейчас и где граница

`docs/todo.md:302–321` и `docs/php_wiki_jobs_roadmap.md`, пункт PHP-02. Это первый открытый согласованный шаг. В обзорной карте чекбоксы отстают: PHP-01, PHP-03 и PHP-04 закрыты в todo и истории (`7ea4a25`, `fcb1867`, `dec77ad`). Прямой транспорт и изолированный стенд доступны; T22–T27 для PHP-02 не требуются. Один шаг плагина: только классификация submit, её отображение и доказательства. Без изменений Go, БД, ACL/CSRF, readyz/jobs, повторов или нового поля протокола.

## Исходное состояние (проверено чтением, не прогоном)

- `PetarClient.php::submitInput()` делает один прямой POST; `!$success || $status !== 201` даёт `RuntimeException` (502), независимо от того, получен ли HTTP 400. Успех требует валидного 201/new с двумя UUID. В `action.php::handleAjax()` уже есть безопасный путь для контролируемого `InvalidArgumentException` → 400; runtime → фиксированный 502. Заголовок ответа и JSON связаны одним request ID.
- `script.js::initialize()` уже различает 5xx/исключение и прочие ошибки. При таймауте или 502 показывает `err_submit_uncertain`, снимает busy, блокирует submit до readiness. Для 400 используется общее `err_request` («Проверьте ввод и готовность сервиса»), а не явное сообщение об отказе. `syntax.php` передаёт словарь виджету; тексты в `lang/{ru,en}/lang.php`.
- `tests/contract/client_test.go::runDirectBFF` использует настоящий DokuHTTPClient и HTTP-фикстуру, протоколирует полученные запросы. `action_test.go::actionPHP` исполняет настоящий контроллер и клиент с подменой транспорта. `TestUIBehavior` запускает `ui_behavior.js`: уже проверены timeout, предупреждение при 502, отсутствие автоповтора. Новые RED ниже — прогноз, а не результаты.

## TDD и приёмка (команды ниже)

1. **Начать с `TestDirectBFFRequests/submit_rejected`** в `tests/contract/client_test.go`. В router перед общим 201 добавить для `/api/v1/inputs` режим `submit_rejected`: статус 400, тело с маркером `PRIVATE_UPSTREAM_BODY`; оставить реальный HTTP. Проверить ровно `POST /api/v1/inputs`, отсутствие успеха, код исключения 400 и фиксированный текст `Input rejected` без маркера. Сейчас будет 502. GREEN: в `submitInput()` после `sendRequest` распознать полученный прямой статус 400 до общей обработки неуспеха; не определять исход по содержимому тела или одному boolean транспорта. На свежем клиенте без редиректов использовать `InvalidArgumentException('Input rejected', 400)` для этого узкого случая. Не использовать `$client->error`, backend message или пользовательский ввод в исключении/логах. Прочие сбои оставить в прежней ветке. Публичный API класса не расширяется.
2. **Граница AJAX:** добавить `TestSubmitOutcomes` в `tests/contract/action_test.go`: `actionPHP` с валидным POST, известным UUIDv4 и backend 400 с приватным телом → HTTP 400, JSON с `error=Input rejected`, тем же `request_id`, без приватного текста/ID задания. Для транспортной потери задать фикстуре `backend_ok=false`, код 0, приватный маркер (подключить уже существующий `DokuHTTPClient::$ok` к параметру теста) → 502 с фиксированным `Backend request failed`, тем же ID; проверить отсутствие маркера и в захваченных `Logger::$messages`. Использовать разные маркеры в теле и сообщении транспорта, если моделируются оба. Сверить заголовок и JSON существующим `TestBFFHTTPHeaders`, включив backend 400 в его таблицу. До п. 1 отказ ошибочно 502; после него контроллер уже умеет отдать нужные ответы, не менять catch ради теста. Сохранить тесты невалидного 201, 3xx, локальной валидации, ACL/CSRF.
3. **Интерфейс:** в `tests/contract/ui_behavior.js` на готовом виджете отправить один submit, вернуть BFF 400 с приватным маркером. Требовать явный локализованный текст отказа с предложением исправить ввод (для обеих локалей, отдельный ключ `err_submit_rejected`), отсутствие маркера, предупреждения о неизвестном исходе и ID задания; submit доступен для ручной коррекции. Сравнить число POST до/после обработки ответа. Тестировать наличие непустого ключа, а не равенство двух `undefined`. RED: сейчас общее `err_request`. GREEN: добавить ключ в RU/EN и список словаря в `syntax.php`; в `script.js` выбрать его только для 400, оставив прочие 4xx с прежним сообщением. Локальный BFF 400 также означает отказ ввода, не обещать пользователю, что запрос дошёл до backend. Существующие timeout/502 должны по-прежнему показывать только предупреждение; ручная перепроверка делает GET, а не POST. Прямой 201 без изменений. Не выводить `data.error`.
4. Синхронизировать только описание submit в `docs/architecture.md` §4.5: прямой 400 — исключение из прежнего «другой код → 502». Это уточнение контракта согласованного PHP-02, а не общее проксирование статусов. Изменяемые файлы: перечисленные выше, `docs/todo.md` и `.opencode/tasks/php_02_submit_outcomes.md` плагина. Архив для передачи в `/opt/go-pet/.opencode/tasks/` отдельно, без Go-коммита. Не включать существующие пользовательские изменения `docs/rules.md` и `.playwright-mcp/`.

## Проверки Build по порядку

Из `/opt/php-wiki-plugin`. Для точечных тестов нужны Git, Go с `-race`, Node, Bash, Docker и запущенный `dokuwiki` с этим checkout по `/config/dokuwiki/lib/plugins/petar`; для итоговой приёмки также Docker Compose и возможность сборки образов. Backend имитируется; не задавать DATABASE_URL, не подключать Go-сервис или рабочую/тестовую PostgreSQL. Сначала прочитать три обязательных документа, проверить `git status --short`.

```bash
export GO111MODULE=off
go test -count=1 -v ./tests/contract -run '^TestDirectBFFRequests$/^submit_rejected$'
# Повторить ту же команду после минимального исправления.
go test -count=1 -v ./tests/contract -run '^(TestSubmitOutcomes|TestBFFHTTPHeaders|TestDirectBFFRequests)$'
go test -count=1 -v ./tests/contract -run '^TestUIBehavior$'
# Повторить после добавления локализованного состояния.
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/PetarClient.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/syntax.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/ru/lang.php
docker exec dokuwiki php -l /config/dokuwiki/lib/plugins/petar/lang/en/lang.php
docker exec dokuwiki touch /config/dokuwiki/conf/local.php
TMPDIR=/tmp/opencode ./scripts/acceptance.sh
```

При дополнительных PHP-правках lint каждого файла по `docs/rules.md`; при устаревшем кэше использовать описанную там процедуру. Последняя команда включает `./scripts/quality-gate.sh` (diff, PHP/JS, vet, полный contract с race) и Chromium smoke в одноразовом Compose-проекте. Нужны успешный код и `ACCEPTANCE PASSED`, не только contracts-only. Зафиксировать фактические RED/GREEN, результаты и блокеры, не заменять непрошедшую проверку ожидаемым результатом.

## Учёт и завершение

В `docs/todo.md` обновить **существующую** строку PHP-02 (не добавлять вторую):

> - [ ] **PHP-02: Отличать отказ от неизвестного исхода.** Прямой backend 400 → безопасный отказ ввода; потеря ответа → предупреждение без повторного POST. Приёмка: BFF 400/502 с request ID, RU/EN без сырой ошибки, один POST. План: [.opencode/tasks/php_02_submit_outcomes.md](../.opencode/tasks/php_02_submit_outcomes.md).

Сохранить по указанной ссылке точную копию этого плана; такую же копию под тем же именем сохранить в `/opt/go-pet/.opencode/tasks/`. Исходный текст неизменен; при необходимости только дописать после него «Фактически выполнено» с существенными отклонениями и результатами.

**Независимый review обязателен:** карта требует его для каждого шага; проверить различение реального HTTP 400 и потери ответа, отсутствие утечек/ретраев и неизменность остальных статусов. После успешных проверок Build сам вызывает review с diff и критериями, устраняет существенные замечания и повторяет затронутые проверки. Затем показать пользователю diff и фактические результаты, дождаться Human Sign-off. Только после приёмки закрыть пункт `[x]` с уникальным subject `fix: distinguish rejected and uncertain submissions` в одном атомарном коммите; не коммитить без подтверждения и не пушить без отдельного разрешения.

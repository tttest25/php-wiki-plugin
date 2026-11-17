# PHP Phase 1 — итоговый отчёт приёмки

## Статус и границы

Реализация task 07 и согласованные ручные проверки PHP приняты пользователем.
Это не закрытие всех операционных сценариев task 08 или backend T16–T21.
Пользователь явно согласовал создание пяти атомарных коммитов: пять атомарных
шагов фиксируются по subject, SHA — через `git log --format='%H %s'`.
Коммиты выполняются серией из пяти шагов; приведённые ниже доказательства
проверки исходников и ручной приёмки получены до этой серии.
Отметка `[x]` и уникальный subject входят в тот же коммит вместо self hash.
Фактические SHA доступны только после создания коммитов; push этим не заявлен.
Ручные доказательства отчёта сохраняют исходное историческое состояние:
`29a7998` + dirty tree, а не выдают его за уже воспроизводимый Git-релиз.

Исходные revisions:

- php-wiki-plugin: `29a7998fc8890d4d75843e42529f8b3662c65d43` + dirty tree.
- go-pet: `9278dc8240cefcba11a617e3d50fa66c62a9c7ee` + прежние изменения
  `.gitignore`, `.opencode/opencode.json`.
- Task 07/08 находятся в ignored `.opencode/tasks/`; runtime-контракт и
  доказательства сохранены также в отслеживаемой документации.

## Реализованный контракт

- BFF сохраняет `tool`, `message`, `sectok`; добавлен `pattern`.
- PetarClient: echo → message, yt-dlp → url, fabric → text/pattern.
  Отсутствующий pattern означает summarize; текст echo/fabric не обрезается.
- Ответ backend: строго HTTP 201, UUID input/process (включая v7), status=new.
  Ошибки пользовательского ввода → 400; транспорта/ответа → безопасный 502.
- Единый UUIDv4 до ACL/CSRF в заголовке, JSON и запросе backend.
- DokuWiki INPUT, ACL ai/admin и CSRF; status не активирован; методы с Allow.
- Readiness: строго HTTP 200/status=ready; неготовность → 503.
- Уникальные независимые формы, начальный disabled, retry readiness,
  timeout 15 секунд без автоматического повторного submit, RU/EN.

## Автоматические проверки

При подготовке отчёта повторно выполнены, результат PASS:

```sh
GO111MODULE=off go test -count=1 -race -timeout 120s ./tests/contract
GO111MODULE=off go vet ./tests/contract
node --check script.js
node --check tests/contract/ui_behavior.js
git diff --check
```

PHP lint в dokuwiki: PetarClient.php, action.php, syntax.php,
lang/en/lang.php, lang/ru/lang.php — PASS.
HTTP fixture использует настоящий DokuWiki Input и проверяет реальные заголовки,
ранние отказы, методы и ошибки backend. Поведение JS проверено в Node.

На этапе реализации два запуска backend `./scripts/quality-gate.sh`
с `GOFLAGS=-count=1`, DB_DSN отдельной `petar_test_task07`:
оба `QUALITY GATE PASSED`. Логи `/tmp/opencode/task07/quality-gate.log` и
`/tmp/opencode/task07/quality-gate-final.log`.
При review отчёта найден и исправлен P2: tool с невалидным UTF-8 мог дать
пустое тело вместо JSON ошибки. Регрессионный HTTP-тест tool=%FF сначала
воспроизвёл HTTP 400/JSON EOF, затем прошёл после постоянного текста ошибки
unknown-tool и JSON_INVALID_UTF8_SUBSTITUTE в sendJson.
Повторные race-тесты/vet/lint прошли, независимое review закрыло замечание.
После PHP-исправления gate повторён на той же отдельной БД:
QUALITY GATE PASSED, лог `/tmp/opencode/task07/quality-gate-review.log`.
Скрипт ещё читает COUNT из petar_db и останавливает/возобновляет app;
Go integration использовал отдельную БД. Исправление lifecycle — T16.

## Ручная браузерная приёмка

Проверки выполнял пользователь в своём браузере:

- admin: форма, Ready, отправка трёх инструментов — успешно.
- ai: форма, Ready, отправка echo-test — успешно.
- Два виджета: независимость полей/выбора инструмента/readiness — успешно.
- Русский и английский интерфейсы — подтверждены.
- Обычный пользователь: вместо форм сообщение об отказе доступа.
- AJAX обычного пользователя: success=false, error=Access denied,
  request_id=`4833bcc2-702b-4134-bf38-8f03d37dffce`.
  Передано тело, но не снимок кода/заголовков этого вызова; HTTP 403 и
  заголовки отказов отдельно подтверждены автоматическими HTTP-тестами.

## Реальные E2E

Отправки сделаны из формы пользователем. Агент сверил payload в БД,
HTTP 201/Request ID в backend-логе, затем выполнил только выбранные задания
через функции существующего spool-runner.sh, без обхода общей очереди.
Это не проверка длительного режима --loop.

| Сценарий | Input ID | Process ID | Job ID | Итог |
|---|---|---|---|---|
| echo/admin | 01a1e6bc-f735-7eaf-9818-aa775d43b767 | 01a1e6bc-f740-7812-b17c-3f029dca5bdc | 01a1e6bc-f743-732e-b58a-8cd9b73eda2e | completed |
| yt-dlp прежний CLI | 01a1e6c0-2bd1-796b-b1c4-98e6ac68505f | 01a1e6c0-2bd1-7b44-8da6-1342b670bec4 | 01a1e6c0-2bd1-7e8c-9838-dad6a325d3f3 | failed |
| fabric/admin | 01a1e6c1-ba2a-7581-ace6-788ffff08d58 | 01a1e6c1-ba2a-7a34-b3d2-c701827a5cda | 01a1e6c1-ba2a-7faf-80d6-6f7594141e1e | completed |
| yt-dlp Docker/admin | 01a1e6d7-b814-7f95-9dba-398b33ac387d | 01a1e6d7-b815-7348-81b4-b858dc1faad9 | 01a1e6d7-b815-76b7-b7de-74d8d9897b87 | completed |
| echo/ai | 01a1e6da-ba10-75c5-8587-e51f595a2278 | 01a1e6da-ba10-7ba0-9bdd-d70462b5bd7d | 01a1e6da-ba11-70b1-a93b-b7024667d275 | completed |

Request IDs в том же порядке:

1. `52ee6737-c28c-499c-b0c9-c9ca40def96f`
2. `ef0ef0c1-59f0-4541-9814-6354c8cf8922`
3. `69e24718-cb3e-4ed9-988e-5479106cc4a2`
4. `7e53b144-7b7d-4c6e-875f-f3d5a72626b3`
5. `e39b4721-d000-41ef-8618-72c598f425b5`

У четырёх успешных заданий подтверждены completed job/process, аудит
pending → dispatched → in_progress → completed, sm_outputs.metadata и файл.
Артефакты: `/opt/go-pet/spool/out/<job_id>/result.json`;
DB хранит логический `/spool/out/<job_id>/result.json`.

- Echo: точные сообщения petar-e2e-001 и petar-e2e-ai-001.
- Fabric 1.4.576, summarize: разрешённый пользователем реальный вызов к модели
  по умолчанию, непустой output.md (1175 байт). Пользователь указал
  gemini-flash-lite-latest; агент не читал ключи/конфиги, фактическая модель
  в ответе отдельно не аттестована.
- Прежний yt-dlp 2024.03.10: YouTube HTTP 400/нет формата для pNVohL-3DOs;
  штатные failed job/process и аудит отказа.
- После переустановки пользователем: `/usr/local/bin/yt-dlp` — Bash-wrapper
  к jauderho/yt-dlp; /downloads соответствует /opt/docker-yt-dlp/downloads.
  Получены реальные метаданные 96HlJYdqGSg, duration=595, валидный JSON
  1231441 байт. Версия/digest образа не закреплены этим PHP-этапом.
  Runner записал stdout --dump-json в spool; субтитры не запрашивались.

## Ограничения

- Browser timeout/backend-down, CSRF и неизвестные маршруты вручную в
  пользовательской сессии не проверялись; соответствующее поведение покрыто тестами.
- Очистка контура, recovery/crash-window/неоднозначный callback и длительный
  loop-runner task 08 не выполнены. Старая очередь не очищалась/не запускалась.
- Runner требует T20: конечные ожидания, lock, честный missing CLI,
  сохранение неоднозначных заданий.
- Нет DAG, polling результата, exactly-once или субтитров.
- Секреты/cookies и сырые метаданные с временными URL не включаются в Git.

## Diff и согласование

До task 07 уже существовали незакоммиченные script/style, перенос клиента,
lexer/автозагрузка, изменения AGENTS/rules/docs/.gitignore. Это сохранено как
описание исходного dirty tree, к которому относятся ручные проверки и E2E выше,
а не как утверждение, что согласование шагов 5/6 всё ещё ожидается.

Согласованы пять атомарных шагов с уникальными subjects:

1. `feat: add frontend submit logic and styles`
2. `fix: E2E integration fixes (lexer priority, autoload path, response envelope)`
3. `fix: map plugin tool inputs to canonical payloads`
4. `fix: enforce BFF request tracing and input validation`
5. `fix: make tool forms readiness-aware and independently usable`

Ссылки `[x]` + subject в `docs/todo.md` входят в каждый соответствующий коммит;
SHA извлекаются через `git log` после его фактического создания, без вымышленных
хешей и без отдельного документационного коммита. Это обновление метаданных
не утверждает, что будущая фиксация уже выполнена.

Промежуточные снимки реконструированы, не являются точными историческими
исходниками. Baseline 01 отдельно не развёртывается: требуется интеграция
Шага 6. Шаг A не включает B/C, B не включает C; общие файлы разделены по
инкрементальным версиям. Историческая ручная приёмка не объявляется повторной
браузерной проверкой каждого реконструированного снимка.
Прежние .gitignore и backend-изменения остаются вне этой согласованной серии;
это установленная граница, не ожидающее решения исключение.

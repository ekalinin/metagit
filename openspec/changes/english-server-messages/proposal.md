# Proposal

## Why

Тексты, которые генерирует сервер metagit, сейчас на русском: ошибки API, сообщения в журнале событий, вывод git push. Репозиторий открыт для всех, `README.md` и лендинг уже показывают вывод push по-английски (`delivered`, `queued`, `error: ... (retry later)`), а код печатает `доставлено`, `в очереди`, `ошибка: ... (повтор позже)`. Решено, что сервер говорит только по-английски, а выбор языка касается только веб-интерфейса (отдельный change `ui-locale`).

## What Changes

- Все русские строки в Go-коде сервиса заменяются английскими:
  - ошибки API и валидации (`internal/api`, `internal/core`, `internal/repo`, `internal/replicate/url.go`, `internal/transfer`, `internal/archive`);
  - сообщения событий журнала проекта (архив при push, расхождение, ошибка доставки);
  - тексты ошибок доставки, которые попадают в `last_error` target'а;
  - вывод git push: статусы доставки, отказ в push для защищенных refs и служебного пространства, сообщения об архиве и о том, что включенных target'ов нет;
  - сообщения хука (`internal/hooks/client.go`, `internal/hooks/hooks.go`);
  - заглушка `internal/webui` для несобранного веб-интерфейса.
- Строки вывода push совпадают с примером в `README.md`: `delivered`, `queued`, `error: <текст> (retry later)`.
- Go-тесты проверяют английские тексты.
- Пример вывода push в `README.ru.md` и ожидаемый вывод в `docs/testing.md` становятся английскими.
- Спеки `mirror-replication` и `git-hosting` цитируют английские литералы.
- **BREAKING** для тех, кто разбирает вывод push или тексты ошибок API скриптами: тексты меняются. Коды ответов API и структура JSON не меняются.

### Non-goals

- Локализация веб-интерфейса и его собственных текстов (`web/`) - это change `ui-locale`.
- Выбор языка сервера, каталоги переводов в Go, коды ошибок в API.
- Перевод старых событий в базе: записи, созданные до изменения, остаются на русском.
- Перевод спецификаций, `README.ru.md` и прочей документации на английский. Меняются только цитаты вывода сервера.
- Логи сервера и CLI: они уже на английском.

## Capabilities

### New Capabilities

Нет.

### Modified Capabilities

- `mirror-replication`: требование "Статус доставки в выводе push" перечисляет английские строки вывода (`delivered`, `queued`, `error`, расхождение) и требует, чтобы вывод был на английском.
- `git-hosting`: сценарий "Несуществующий проект" цитирует ошибку как `repository not found`. Код уже отвечает этим текстом, меняется только формулировка спеки.

## Impact

- Go: `internal/api/api.go`, `internal/archive/archive.go`, `internal/core/core.go`, `internal/hooks/client.go`, `internal/hooks/hooks.go`, `internal/replicate/replicate.go`, `internal/replicate/url.go`, `internal/repo/repo.go`, `internal/transfer/transfer.go`, `internal/webui/webui.go` (81 строка).
- Go-тесты: `internal/app/archive_test.go`, `internal/app/replication_test.go`, `internal/api/api_test.go`, `internal/archive/archive_test.go`, `internal/e2e/e2e_test.go`, `internal/hooks/hooks_test.go`, `internal/transfer/transfer_test.go`.
- Документация: `README.ru.md` (пример вывода push), `docs/testing.md` (ожидаемый вывод в сценарии e2e).
- `web/` не меняется. Веб-интерфейс показывает текст ошибок и событий с сервера как есть, поэтому в русском интерфейсе эти тексты станут английскими. Фикстуры в тестах `web/src` с русскими текстами сервера остаются рабочими: они проверяют, что текст показывается, а не его язык.
- Схема базы, API-контракт (коды ответов, поля JSON) и зависимости не меняются.
- Ветка `refactor/english-server-messages`.

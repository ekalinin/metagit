# Tasks

Поведение уже реализовано в PR #32, change только описывает его в спеке.

## 1. Спецификация

- [x] 1.1 Сверить новое предложение требования "Повторы и статус доставки" и сценарий "Нет доступа на push, а доставлять нечего" с кодом и тестами PR #32: `reconcile` и `checkAccess` в `internal/replicate/replicate.go`, `TestNothingToPushChecksToken` (неверный токен в пустом проекте - "ошибка" с `Authentication failed`, refs на хостинге не меняются) и `TestNothingToPushChecksSSHKey` (ключ только для чтения - "ошибка" с текстом хостинга, на пустом хостинге не появляются refs) в `internal/app/replication_test.go`; проверка: `go test -count=1 -run 'TestNothingToPushChecksToken|TestNothingToPushChecksSSHKey' ./internal/app/` проходит
- [x] 1.2 Проверить формат change; проверка: `openspec validate nothing-to-push-access-check --strict` проходит, первые два сценария требования в дельте совпадают с `openspec/specs/mirror-replication/spec.md`

## 2. Документация

- [x] 2.1 Добавить строку сценария "Нет доступа на push, а доставлять нечего" в таблицу `mirror-replication` в `docs/testing.md` с тестами `TestNothingToPushChecksToken` и `TestNothingToPushChecksSSHKey`; проверка: каждый сценарий из `specs/mirror-replication/spec.md` этого изменения есть в таблице
- [x] 2.2 Общая проверка ветки; проверка: `make lint` проходит, `git diff --stat main` показывает только файлы этого change и `docs/testing.md`

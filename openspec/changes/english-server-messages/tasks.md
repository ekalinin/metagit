# Tasks

Каждая задача меняет строки в коде вместе с проверками этих строк в тестах, чтобы тесты проходили после каждой задачи. Формулировки - по design, разделы 2 и 3.

## 1. Вывод push и хуки

- [x] 1.1 Статусы доставки и сообщения после push в `internal/core/core.go` (`PostReceive`, `FormatResult`: `queued`, `delivered`, `delivered; diverged, ...`, `error: ... (retry later)`, архив, нет включенных target'ов) и проверки в тестах: `internal/app/replication_test.go:96,228,287,460,491`, `internal/app/archive_test.go:66`, `internal/e2e/e2e_test.go:253,291,313`, `internal/transfer/transfer_test.go:157` (положительная часть - `github: delivered`, отрицательная - `diverged` вместо `расхождение`); проверка: `go test ./internal/app/ ./internal/e2e/ ./internal/transfer/` проходит
- [x] 1.2 Отказ в push и проверка шаблонов в `internal/archive/archive.go` (строки 58, 61, 71, 79, 85, функция `describe`) и проверки в тестах: `internal/app/archive_test.go:121,129,143,167`, `internal/archive/archive_test.go:61,65`, `internal/api/api_test.go:256`, `internal/e2e/e2e_test.go:279`; проверка: `go test ./internal/archive/ ./internal/app/ ./internal/api/ ./internal/e2e/` проходит
- [x] 1.3 Сообщения хука в `internal/hooks/client.go:33,56,59` и `internal/hooks/hooks.go:167`, проверка в `internal/hooks/hooks_test.go:137`; проверка: `go test ./internal/hooks/` проходит

## 2. События и ошибки доставки

- [x] 2.1 Сообщения событий: `archive_push` в `internal/archive/archive.go:215`, `diverged` в `internal/replicate/replicate.go:619-630` (вид ref, действие, текст события), префикс `delivery error:` в `internal/replicate/replicate.go:353`; проверка: `go test ./internal/archive/ ./internal/replicate/ ./internal/app/` проходит, поиск кириллицы из задачи 4.1 не находит строк в этих местах
- [x] 2.2 Ошибки доставки, которые попадают в `last_error`, в `internal/replicate/replicate.go:410,420,500,513,668,677` и ошибки URL target'а в `internal/replicate/url.go:11-47`, проверки в тестах: `internal/app/replication_test.go:77`, `internal/api/api_test.go:222`; проверка: `go test ./internal/replicate/ ./internal/app/ ./internal/api/` проходит

## 3. Ошибки API и валидации

- [x] 3.1 Ошибки `internal/api/api.go` (18 строк: адрес API, Content-Type, сессия, `не найдено`, `уже существует`, внутренняя ошибка, JSON, вход, конфликты имен, `limit`, SSH-ключ, base64), проверка в `internal/api/api_test.go:49` (`invalid name or password`); проверка: `go test ./internal/api/` проходит
- [x] 3.2 Ошибки валидации в `internal/core/core.go:78-255` и `internal/repo/repo.go:18,21`, проверки в `internal/api/api_test.go:218,226`; проверка: `go test ./internal/core/ ./internal/repo/ ./internal/api/` проходит
- [x] 3.3 Ошибки экспорта и импорта в `internal/transfer/transfer.go` (12 строк), проверки в `internal/transfer/transfer_test.go:184,195` и `internal/api/api_test.go:413`; проверка: `go test ./internal/transfer/ ./internal/api/` проходит
- [x] 3.4 Заглушка несобранного интерфейса в `internal/webui/webui.go:30-31`: `lang="en"` и текст `The web interface is not built. Run <code>make build</code>.`; проверка: `go test ./internal/webui/` проходит

## 4. Проверка кода

- [x] 4.1 Убедиться, что в Go вне тестов не осталось кириллицы; проверка: `git ls-files '*.go' | grep -v _test.go | xargs perl -CSD -ne 'print "$ARGV:$.: $_" if /\p{Cyrillic}/; close ARGV if eof'` ничего не выводит (до изменения выводит 81 строку)
- [x] 4.2 Убедиться, что в Go-тестах кириллица осталась только в названиях сценариев в комментариях и во входных данных `internal/repo/repo_test.go:29`, и что ни одна отрицательная проверка не ищет русскую подстроку; проверка: тот же `perl` по `git ls-files '*_test.go'` выводит только строки с `// Scenario`, `// Scenarios`, продолжения этих комментариев и `repo_test.go:29`

## 5. Документация

- [x] 5.1 Пример вывода push в `README.ru.md:30-32` заменить английским, как в `README.md:30-32`; проверка: блок кода в обоих файлах совпадает построчно
- [x] 5.2 Ожидаемый вывод в шагах 3, 5, 6 и 7 `docs/testing.md:30-34`: `hosting1: delivered`, `hosting2: delivered`, `is protected`, `hosting1: delivered; diverged`, `hosting2: error`; проверка: `grep -n 'доставлено\|hosting2: ошибка\|"защищен"' docs/testing.md` ничего не находит

## 6. Итоговая проверка

- [x] 6.1 Полная проверка; проверка: `make lint` и `make test` проходят, `git diff --stat main` показывает только файлы из Impact в `proposal.md` и файлы этого change
- [x] 6.2 Ручная проверка вывода push на собранном бинарнике: push в проект с одним доступным и одним недоступным target'ом и force-push в защищенную ветку; проверка: в выводе `remote: metagit: <target>: delivered`, `remote: metagit: <target>: error: ... (retry later)` и `remote: metagit: refs/heads/main is protected (pattern refs/heads/main): non-fast-forward push is not allowed`, русских строк нет

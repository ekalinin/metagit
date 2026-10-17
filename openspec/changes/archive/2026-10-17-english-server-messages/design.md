# Design

## Context

Мотивация и объем - в `proposal.md`, требования - в `specs/mirror-replication/spec.md` и `specs/git-hosting/spec.md`.

- Русские строки есть в 10 файлах Go (81 строка, без комментариев). Комментарии в Go уже на английском.
- Часть текстов сервера уже английская: ошибки `internal/auth` (`invalid name or password`, `invalid input: ...`), ответы `internal/githttp` и `internal/gitssh` (`repository not found`), логи сервера, CLI.
- Тексты попадают к пользователю четырьмя путями:
  - JSON-ответ API `{"error": "..."}`, который веб-интерфейс показывает как есть;
  - поле `message` в таблице `events`, которое пишется один раз, при создании события;
  - поле `last_error` у target'а, которое перезаписывается при каждой попытке доставки;
  - вывод git push (`remote: metagit: ...`), который хук получает от сервиса и печатает в stderr.
- Go-тесты проверяют тексты через `strings.Contains` с русскими подстроками. В комментариях к тестам указаны русские названия сценариев из спек. Спеки остаются на русском, поэтому эти ссылки не меняются.

## Goals / Non-Goals

**Goals:**

- Ни одной русской строки в строковых литералах Go вне тестов.
- Каждая проверка текста в Go-тестах по-прежнему что-то проверяет: и положительная, и отрицательная.
- Термины в текстах сервера совпадают с `README.md`.

**Non-Goals:**

- Изменение структуры сообщений: набора параметров, порядка полей, префиксов. Это перевод 1:1.
- Автоматическая проверка в CI, что в Go не появилась кириллица.
- Изменения в `web/`, включая фикстуры тестов с русскими текстами сервера.

## Decisions

### 1. Прямая замена литералов, без каталога

Каждая строка переводится на месте: `errors.New`, `fmt.Errorf`, `invalid(...)`, `errorf(...)`, `writeError(...)`, `fmt.Sprintf` для событий и вывода push. Новых пакетов, ключей и функций нет.

Альтернатива: каталог сообщений с ключами в Go. Отклонено: второго языка на сервере не будет, каталог добавит косвенность без пользы.

### 2. Стиль и словарь

Стиль - как у существующих английских ошибок в `internal/auth`: со строчной буквы, без точки в конце, детали после двоеточия.

| Русский термин | Английский |
|---|---|
| target | target |
| хостинг | hosting |
| учетные данные | credentials |
| владелец | owner |
| проект | project |
| шаблон (защищенных refs) | pattern |
| защищен | protected |
| служебное пространство `refs/metagit/` | reserved namespace |
| архив, архивный ref | archive, archive ref |
| расхождение, разошлась | diverged |
| доставлено | delivered |
| в очереди | queued |
| повтор позже | retry later |
| ошибка доставки | delivery error |
| ветка, тег | branch, tag |

### 3. Тексты, которые видны в выводе push, событиях и документации

Эти тексты цитируются в `README.md`, `docs/testing.md` и спеках или проверяются тестами, поэтому их формулировки фиксируются здесь. Остальные строки (ошибки валидации и API) переводятся по словарю из раздела 2.

Вывод push (`internal/core/core.go`, `internal/archive/archive.go`, `internal/hooks`):

| Сейчас | Станет |
|---|---|
| `metagit: <target>: в очереди` | `metagit: <target>: queued` |
| `metagit: <target>: доставлено` | `metagit: <target>: delivered` |
| `metagit: <target>: доставлено; расхождение, состояние хостинга сохранено в <refs>` | `metagit: <target>: delivered; diverged, hosting state saved in <refs>` |
| `metagit: <target>: ошибка: <текст> (повтор позже)` | `metagit: <target>: error: <text> (retry later)` |
| `metagit: <ref>: прежнее состояние сохранено в <archive>` | `metagit: <ref>: previous state saved in <archive>` |
| `metagit: не удалось создать архив, он будет создан при следующем запуске: <err>` | `metagit: cannot create the archive, it will be created on the next start: <err>` |
| `metagit: включенных target'ов нет, изменения сохранены только в metagit` | `metagit: no enabled targets, the changes are saved only in metagit` |
| `metagit: <ref>: refs/metagit/ - служебное пространство, push в него запрещен` | `metagit: <ref>: refs/metagit/ is a reserved namespace, pushes to it are not allowed` |
| `metagit: <ref> защищен (шаблон <p>): <действие> запрещен` | `metagit: <ref> is protected (pattern <p>): <action> is not allowed` |
| действия: `удаление`, `перемещение тега`, `push, который не является fast-forward` | `deletion`, `tag move`, `non-fast-forward push` |
| `metagit: push отклонен, ни один ref не изменен` | `metagit: push rejected, no refs were changed` |
| `metagit: push не через metagit: репозиторий принимает изменения только через сервис metagit` | `metagit: push bypasses metagit: the repository accepts changes only through the metagit service` |
| `metagit: сервис недоступен, push отклонен: <err>` | `metagit: service unavailable, push rejected: <err>` |
| `metagit: не удалось получить статус доставки: <err>` | `metagit: cannot get the delivery status: <err>` |
| `metagit: внутренняя ошибка: <err>` | `metagit: internal error: <err>` |

Сообщения событий (`internal/archive/archive.go`, `internal/replicate/replicate.go`):

| Событие | Станет |
|---|---|
| `archive_push` | `<ref>: previous state <object> saved in <archive>` |
| `diverged` | `<branch\|tag> <ref> on target <name> diverged from metagit: hosting state <object> saved in <archive>, ref <action>` |
| действие в `diverged` | `is overwritten with the value from metagit` / `is deleted, as in metagit` |
| `delivery_error` | `delivery error: <text>` |

Префикс `delivery error:` дублирует метку события в веб-интерфейсе, но остается: убирать его - уже изменение структуры, а не перевод (раздел Non-Goals).

### 4. Тесты: заменить подстроки и проверить отрицательные проверки

Каждая русская подстрока в `strings.Contains` заменяется соответствующей английской из раздела 3 или из переведенной ошибки. Отдельно проверяются отрицательные проверки, то есть те, где тест падает, если подстрока найдена: после перевода русская подстрока в них никогда не найдется, и проверка станет пустой. Сейчас такая одна: `internal/transfer/transfer_test.go:157`, `strings.Contains(out, "расхождение")`. Она меняется на `diverged`.

Не меняются: русские названия сценариев в комментариях тестов и `"проект"` в `internal/repo/repo_test.go:29` - это недопустимое имя проекта как входные данные.

### 5. Документация

- `README.ru.md:30-32`: пример вывода push становится английским и совпадает с `README.md`. Текст вокруг остается русским.
- `docs/testing.md:30-34`: ожидаемый вывод в шагах e2e (`hosting1: delivered`, `hosting1: delivered; diverged`, `hosting2: error`).
- `README.md` и `site/index.html` уже показывают английский вывод, их не трогаем.

## Risks / Trade-offs

- [Старые события в базе остаются на русском: `message` пишется один раз] → Принимается, миграции нет. В журнале проекта события до обновления показываются по-русски, после - по-английски.
- [`last_error` target'а с русским текстом остается до следующей попытки доставки] → Принимается: поле перезаписывается при каждой попытке, ручная доставка обновляет его сразу.
- [В русском веб-интерфейсе ошибки API и сообщения событий будут на английском] → Принимается решением "сервер только на английском". Так уже работают ошибки `internal/auth` и `last_error` со stderr git.
- [Скрипты, которые разбирают вывод push или тексты ошибок API, перестанут находить русские строки] → Отмечено как **BREAKING** в proposal. Коды ответов API не меняются, по ним скрипты могут работать дальше.
- [Отрицательная проверка в тестах молча станет пустой] → Раздел 4: такие проверки ищутся и меняются явно.
- [Пропущенная строка] → Проверка в tasks: поиск кириллицы в строковых литералах Go вне тестов ничего не находит.

## Migration Plan

1. Мерж ветки `refactor/english-server-messages` и сборка нового бинарника.
2. Перезапуск `metagit serve`. Хуки в репозиториях вызывают тот же бинарник, отдельное обновление им не нужно. Миграций базы нет.

Откат: откатить коммит и пересобрать бинарник. События, записанные по-английски, останутся английскими.

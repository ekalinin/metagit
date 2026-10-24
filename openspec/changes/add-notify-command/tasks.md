# Tasks

## 1. Пакет `internal/notify`

- [x] 1.1 Тип `Event`, кодирование в JSON и переменные окружения по design, раздел 3: поля в `snake_case`, `refs` всегда массив, `time` в UTC RFC 3339, `METAGIT_REFS` через пробел, обрезка значений длиннее 64 КиБ (`METAGIT_REFS` - по целым refs), окружение `os.Environ()` без `METAGIT_SECRET_KEY`; проверка: табличные тесты JSON и окружения в `go test ./internal/notify` (пустые поля для `archive_push`, обрезка `METAGIT_MESSAGE` и `METAGIT_REFS`, нет `METAGIT_SECRET_KEY`, есть посторонняя переменная окружения)
- [x] 1.2 `Notifier`: `New`, `Start`, `Notify`, `Stop`, nil-безопасные методы, очередь на 1000 событий и одна горутина (design, раздел 2), запуск команды с таймаутом 30 секунд и `WaitDelay` 5 секунд, stderr до 4 КиБ, `Warn` при ненулевом коде, таймауте, переполнении очереди и неотправленных событиях при остановке (design, раздел 3); проверка: тесты с временным shell-скриптом - события приходят по порядку; команда с кодом 1 и строкой в stderr дает `Warn` с видом события, проектом и этой строкой, следующее событие обрабатывается; команда дольше таймаута (в тесте - короткий таймаут) прерывается с `Warn`; при переполнении `Warn` "notification dropped"; `Stop` дожидается очереди, а при зависшей команде прерывает ее и пишет число неотправленных событий; вызовы на nil-`Notifier` не падают; `go test -race ./internal/notify` проходит
- [x] 1.3 `Check(command)`: абсолютный путь, обычный файл с битом исполнения, текст ошибки с `--notify-command` и путем (design, раздел 4); проверка: тест - несуществующий файл, каталог и файл с правами 0644 дают ошибку с `--notify-command` и путем, исполняемый файл проходит

## 2. Настройка и сборка сервиса

- [x] 2.1 `config.Config.NotifyCommand`, флаг `--notify-command` и переменная `METAGIT_NOTIFY_COMMAND` в `internal/config`; проверка: тест `config.Load` - флаг, переменная, флаг важнее переменной, по умолчанию пусто; `metagit serve -h` показывает флаг
- [x] 2.2 `app.New`: `notify.Check` после `CheckTools`, создание и `Start` `Notifier` до `arch.Recover`, передача в `replicate.Options.Notifier` и `archive.Service.Notifier`; `App.Close` останавливает его после сервера хуков и до закрытия базы; `apptest.Options.NotifyCommand`; проверка: тест в `internal/app` - `app.New` с несуществующим и с неисполняемым файлом возвращает ошибку с `--notify-command` и путем (сценарий "Неверный путь"); существующие тесты `internal/app` без настройки проходят без изменений (сценарий "Без настройки")

## 3. Точки уведомлений

- [x] 3.1 `Replicator.deliver`: `delivery_error` при том же условии, что запись в журнал; `recovered`, если прежнее состояние было "ошибка"; `delivered` после `recovered`, если `run.changed` не пуст; `run.record` собирает измененные refs (design, раздел 5); проверка: интеграционные тесты в `internal/app` со скриптом, который дописывает JSON событий в файл - неверный токен и несколько попыток дают одно `delivery_error` с `message` без токена (сценарии "Ошибка доставки", "Ошибка доставки с токеном в URL запроса"); после исправления токена и push новой ветки приходят `recovered`, затем `delivered` с этой веткой в `refs` (сценарий "Восстановление"); push веток `main` и `dev` дает `delivered` с обоими refs в JSON и в `METAGIT_REFS` (сценарий "Данные доставки"); ручная доставка синхронизированного target'а не дает событий (сценарий "Нечего доставлять")
- [x] 3.2 `run.diverged`: `diverged` после записи события в журнал; проверка: интеграционный тест - ветка на хостинге изменена в обход metagit, после push приходит `diverged` с `refs/heads/main` и архивным ref из журнала (сценарий "Расхождение")
- [x] 3.3 `archive.Service.finish`: `archive_push` после коммита транзакции; проверка: интеграционный тест - force push в ветку `feature` дает `archive_push` с `refs/heads/feature`, архивным ref из журнала и пустым `target` (сценарий "Force push в ветку")
- [x] 3.4 Окружение команды в сервисе; проверка: интеграционный тест - при `METAGIT_SECRET_KEY` и посторонней переменной в окружении процесса теста команда видит постороннюю переменную и не видит ключа (сценарий "Ключ не передается"); push не ждет медленную команду: со скриптом, который спит дольше `--push-status-timeout`, вывод push приходит за обычное время (сценарий "Медленная команда")

## 4. Рецепты

- [x] 4.1 `examples/notify/webhook.sh`, `telegram.sh`, `email.sh` по design, раздел 6, с закомментированным примером пропуска `delivered`; проверка: тест на Go в `examples/notify` - `webhook.sh` отправляет JSON события в `httptest.Server` (пропуск без `curl`); `telegram.sh` вызывает `curl` с адресом Bot API, `chat_id` и текстом с проектом и `message` (поддельный `curl` в `PATH`); `email.sh` передает в `sendmail -t` письмо с `To`, темой с видом события и проектом и `message` в теле (поддельный `sendmail` в `PATH`); `sh -n` для всех трех скриптов проходит

## 5. Документация и лендинг

- [x] 5.1 `INSTALL.md` и `INSTALL.ru.md`: строка `--notify-command` в таблице настроек и раздел про уведомления (события, JSON и переменные, порядок и таймаут, рецепты из `examples/notify/`, секреты скрипта в `EnvironmentFile`); проверка: каждое утверждение раздела соответствует `specs/notifications/spec.md`, разделы двух файлов совпадают по содержанию
- [x] 5.2 `README.md` и `README.ru.md`: строка "Notifications" описывает команду уведомлений со ссылкой на раздел в `INSTALL.md`, из "Not in this version" убраны уведомления вне веб-консоли; проверка: оба файла изменены одинаково, `grep -n -i 'outside the web console' README.md` ничего не находит
- [x] 5.3 `site/index.html`: блок "Notifications" и список того, что не входит в версию, как в `README.md`; проверка: сценарии спеки `project-landing` о соответствии `README.md` - каждое утверждение блока есть в `README.md` или `INSTALL.md`, `grep -n '<script' site/index.html` ничего не находит
- [x] 5.4 Purpose в `openspec/specs/web-console/spec.md`: убрать утверждение, что веб-интерфейс - единственное место, где владелец получает уведомления; проверка: `openspec validate --specs --strict` проходит
- [x] 5.5 Раздел `notifications` и обновленная строка "Ошибка доставки с токеном в URL запроса" в таблице соответствия `docs/testing.md`; проверка: каждый сценарий из `specs/notifications/spec.md` есть в таблице с тестом из 1.x-4.x

## 6. Проверка ветки

- [x] 6.1 Общая проверка; проверка: `make lint`, `make test` и `go test -race ./internal/notify ./internal/replicate ./internal/archive ./internal/app` проходят, `openspec validate add-notify-command --strict` проходит
- [x] 6.2 Ручная проверка на собранном бинарнике; проверка: два `metagit serve` из `make go` - зеркало с `--notify-command examples/notify/webhook.sh` и `NOTIFY_WEBHOOK_URL` на локальный HTTP-приемник и второй экземпляр как SSH-хостинг target'а; приемник получает `delivery_error` после добавления target'а с неизвестным SSH-ключом, `recovered` и `delivered` после замены ключа, `diverged` после коммита на хостинге и push в metagit, `archive_push` после force push

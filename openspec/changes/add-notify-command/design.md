# Design

## Context

Мотивация и объем - в `proposal.md`, требования - в `specs/notifications/spec.md` и `specs/mirror-replication/spec.md`.

- Журнал событий проекта - таблица `events` (`internal/store/events.go`), виды `delivery_error`, `diverged`, `archive_push`. Запись события - обычный INSERT, подписчиков и колбэков нет.
- `Replicator.deliver` (`internal/replicate/replicate.go`) - единственное место, где сохраняется итог доставки на target. У него есть проект, target с прежним состоянием (`t.State`, `t.LastError`) и замаскированный текст ошибки. Событие `delivery_error` пишется, только если `t.State != StateError || t.LastError != msg`. Для успешной доставки событий нет.
- `run.reconcile` отправляет refs пачками. Ref, который хостинг принял, записывается через `run.record`. Если отправлять нечего, вызывается `checkAccess`, и refs на хостинге не меняются.
- `run.diverged` пишет событие `diverged` до того, как перезапись прошла, поэтому доставка с `diverged` может закончиться ошибкой.
- `archive.Service.finish` пишет `archive_push` в одной транзакции с архивным ref. Его вызывают `Complete` (после push) и `Recover` (при старте, в `app.New` до `Replicator.Start`).
- Настройки сервиса - флаги и `METAGIT_*` (`internal/config`), файла настроек нет. `METAGIT_SECRET_KEY` читается в `internal/cli/serve.go` и остается в окружении процесса.
- Внешние команды сейчас - только git и ssh через `gitcmd.Runner`: `exec.CommandContext`, таймаут, `WaitDelay` 5 секунд.
- `App.Close` останавливает `Replicator`, затем сервер хуков, затем закрывает базу.

## Goals / Non-Goals

**Goals:**

- Ни доставка, ни push не зависят от того, как быстро работает и чем завершается команда уведомлений.
- Без `--notify-command` код уведомлений не влияет на поведение и тесты.
- Точки уведомлений стоят рядом с местами, где уже пишется журнал событий, чтобы условия совпадали.

**Non-Goals:**

- Общая шина событий для других потребителей.
- Изменение схемы базы.

## Decisions

### 1. Пакет `internal/notify`

```go
type Event struct {
	Kind       string    // delivery_error, recovered, delivered, diverged, archive_push
	Time       time.Time
	Project    string
	Target     string
	Ref        string
	ArchiveRef string
	Refs       []string
	Message    string
}

func New(command string, log *slog.Logger) *Notifier
func Check(command string) (string, error) // проверка при старте, абсолютный путь
func (n *Notifier) Start()
func (n *Notifier) Notify(e Event)         // не блокирует
func (n *Notifier) Stop(wait time.Duration)
```

Методы `*Notifier` работают с nil-получателем и ничего не делают. Так `replicate` и `archive` вызывают `Notify` без проверок, а тестам и сервису без настройки ничего не нужно менять. `app.New` создает `Notifier` только при заданной команде.

Альтернатива: интерфейс или функция-колбэк в `replicate.Options` и `archive.Service`. Отклонено: получателей всего два, и оба передают один и тот же тип события; nil-безопасный тип проще и не требует заглушек.

Альтернатива: уведомлять из `Store.AddEvent`. Отклонено: `recovered` и `delivered` в журнал не пишутся, а `archive_push` пишется внутри транзакции, и уведомление ушло бы до коммита.

### 2. Очередь и исполнитель

- Буферизованный канал на 1000 событий и одна горутина, которая запускает команду для событий по очереди. Одна горутина дает порядок событий, в том числе `delivery_error` перед `recovered` и `recovered` перед `delivered`.
- `Notify` делает неблокирующую отправку в канал. Если канал полон, событие отбрасывается, а в лог пишется `Warn` "notification dropped" с видом события и проектом.
- `Stop(5 * time.Second)`: перестать принимать события (флаг под мьютексом, после него канал закрывается), подождать, пока горутина разберет очередь. Если за 5 секунд не успела, отменить контекст, чтобы прервать текущую команду, и записать в лог `Warn` с числом оставшихся событий.

Альтернатива: очередь в SQLite с курсором и повторами. Отклонено по объему (proposal, Non-goals). Ошибки и расхождения остаются в журнале событий в веб-консоли.

### 3. Запуск команды

- `exec.CommandContext` с контекстом на 30 секунд, без аргументов и без shell. `WaitDelay` 5 секунд, как у `gitcmd.Runner`, чтобы потомки команды, держащие stdout или stderr, не задерживали очередь.
- stdin - JSON события (`encoding/json`, поля в `snake_case` как в спеке, `refs` - всегда массив, `time` - `Time.UTC().Format(time.RFC3339)`).
- Окружение: `os.Environ()` без `METAGIT_SECRET_KEY` плюс переменные события. Значения длиннее 64 КиБ обрезаются (`METAGIT_REFS` - по целым refs), чтобы не упереться в ограничение размера одной строки окружения в Linux (128 КиБ).
- stdout отбрасывается. stderr собирается в буфер до 4 КиБ, первая непустая строка попадает в лог при ошибке.
- Ненулевой код или таймаут: `Warn` "notification command failed" с `event`, `project`, `target`, `err` и первой строкой stderr. Успех: `Debug`.

### 4. Настройка и проверка при старте

- `config.Config.NotifyCommand`, флаг `notify-command`, переменная `METAGIT_NOTIFY_COMMAND`, по умолчанию пусто.
- `app.New` после `CheckTools` вызывает `notify.Check`: путь приводится к абсолютному, `os.Stat` должен найти обычный файл с хотя бы одним битом исполнения. Ошибка: `--notify-command: <path> is not an executable file` (или текст ошибки `os.Stat`). Проверка в `app.New`, а не в `config.Load`, потому что `config` не обращается к файловой системе.

### 5. Точки уведомлений

| Событие | Место | Условие |
|---|---|---|
| `delivery_error` | `Replicator.deliver`, ветка ошибки | то же, что для записи события в журнал |
| `recovered` | `Replicator.deliver`, ветка успеха | `t.State == StateError` |
| `delivered` | `Replicator.deliver`, ветка успеха, после `recovered` | `len(run.changed) > 0` |
| `diverged` | `run.diverged` | после успешной записи события в журнал |
| `archive_push` | `archive.Service.finish` | после коммита транзакции |

- `run.changed` - refs, для которых `run.record` записал новое значение или удаление. В уведомлении список отсортирован.
- Тексты: `delivery_error`, `diverged`, `archive_push` - `Message` события журнала; `recovered` - `delivery to <target> works again`; `delivered` - `delivered <n> refs to <target>` (`1 ref` в единственном числе).
- Прерванная остановкой сервиса доставка не сохраняет состояние и не дает уведомлений.
- `Notifier` передается в `replicate.Options.Notifier` и в поле `archive.Service.Notifier`. `app.New` создает и запускает его до `arch.Recover`, чтобы события восстановления архива тоже уходили в команду.
- `App.Close`: `Replicator.Stop`, сервер хуков, затем `Notifier.Stop(5 * time.Second)`, затем база.

### 6. Рецепты `examples/notify/`

POSIX `sh`, `set -eu`, комментарии на английском, настройки - переменными окружения скрипта:

- `webhook.sh`: `curl -fsS --retry 3 --max-time 10 -H 'Content-Type: application/json' --data-binary @- "$NOTIFY_WEBHOOK_URL"` - JSON со stdin как есть.
- `telegram.sh`: `curl` на `https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendMessage` с `chat_id=$TELEGRAM_CHAT_ID` и текстом `metagit <project> (<target>): <message>`, без скобок для `archive_push`. Текст обрезается до 4000 байт: Telegram принимает не больше 4096 символов.
- `email.sh`: письмо через `sendmail -t` (или `$SENDMAIL`) на `$NOTIFY_EMAIL_TO`, необязательный `From` из `$NOTIFY_EMAIL_FROM`, тема `metagit: <event> in <project>`, в теле `message`, target, ref, архивный ref, refs и время.

В каждом скрипте закомментированный пример, как пропустить `delivered`, если уведомления о каждой доставке не нужны. Секреты скриптов (`TELEGRAM_BOT_TOKEN` и т.п.) задаются в том же `EnvironmentFile`, что и `METAGIT_SECRET_KEY`, - команда видит окружение сервиса без ключа.

### 7. Тесты

- `internal/notify`: команда - временный shell-скрипт, который пишет stdin и окружение в файлы. Проверяются JSON и переменные, отсутствие `METAGIT_SECRET_KEY`, порядок, обрезка длинных значений, ненулевой код и таймаут (запись в лог через `slog` в буфер), переполнение очереди, `Stop` с ожиданием и с прерыванием, nil-`Notifier`. Таймаут 30 секунд в тестах заменяется полем для тестов.
- `internal/config`: флаг и переменная. `internal/app`: старт с несуществующим и неисполняемым файлом.
- `internal/app`, интеграционные тесты: `apptest.Options.NotifyCommand` со скриптом, который дописывает JSON событий в файл. Сценарии из `specs/notifications/spec.md`: неверный токен (несколько попыток, одно `delivery_error`), исправленный токен с новой веткой (`recovered`, затем `delivered`), ручная доставка без изменений, расхождение, force push в ветку, маскировка токена в `message`.
- `examples/notify`: тест на Go в том же каталоге запускает скрипты с фиктивными событиями. `webhook.sh` - против `httptest.Server` (пропуск, если нет `curl`), `telegram.sh` и `email.sh` - с поддельными `curl` и `sendmail` в `PATH`, которые записывают аргументы и stdin.

## Risks / Trade-offs

- [Уведомление теряется при перезапуске сервиса или ошибке команды] → Принимается. `delivery_error`, `diverged` и `archive_push` остаются в журнале событий, рецепты повторяют отправку через `curl --retry`.
- [`delivered` на каждый push по каждому target'у - много сообщений] → Скрипт фильтрует события, в рецептах есть пример. Очередь ограничена 1000 событиями.
- [Зависшая команда задерживает следующие уведомления] → Таймаут 30 секунд и `WaitDelay`; при переполнении очереди события отбрасываются с записью в лог, а не копятся в памяти.
- [Команда видит окружение сервиса] → Она настраивается только на сервере, тем же администратором, что задает окружение. `METAGIT_SECRET_KEY` вычищается, учетные данные target'ов в окружение процесса не попадают.
- [Ошибка при одинаковом тексте не повторяется] → Так же, как в журнале: иначе повторы раз в 30 минут дали бы поток одинаковых сообщений. Новый текст ошибки дает новое уведомление.
- [Доставка с `diverged` может закончиться ошибкой] → Команда получит `diverged`, а затем `delivery_error`; это отражает то, что произошло на хостинге.

## Migration Plan

1. Мерж ветки и сборка нового бинарника. Миграций базы нет.
2. Без `--notify-command` после обновления ничего не меняется. Чтобы включить уведомления, положить скрипт на сервер, добавить флаг или `METAGIT_NOTIFY_COMMAND` и перезапустить сервис.

Откат: убрать флаг или вернуть прежний бинарник.

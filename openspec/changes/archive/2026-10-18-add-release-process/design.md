# Design

## Context

Мотивация и объем - в `proposal.md`, требования - в `specs/releases/spec.md` и `specs/project-landing/spec.md`.

- Тегов и релизов нет. Репозиторий `ekalinin/metagit` публичный, PR вливаются merge-коммитами, коммиты и заголовки PR в формате Conventional Commits.
- `make build` = `make web` (`npm ci`, `npm run build` в `web/`, результат в `internal/webui/dist/app`, каталог в `.gitignore`) + `make go` (`CGO_ENABLED=0 go build -o bin/metagit ./cmd/metagit`). Веб-интерфейс встраивается через `go:embed all:dist` (`internal/webui/webui.go`), без него бинарник показывает страницу "The web interface is not built".
- SQLite через `modernc.org/sqlite` на чистом Go, поэтому кросс-сборка не требует C toolchain.
- `.github/workflows/ci.yml`: `actions/checkout@v5`, `actions/setup-go@v6` (`go-version-file: go.mod`), `actions/setup-node@v5` (Node 24, кэш npm), затем `make lint`, `make build`, `make test`.
- Версии в бинарнике нет. Команды разбирает `switch` в `internal/cli/cli.go`, там же текст справки. Строку `metagit started` пишет `internal/cli/serve.go`.
- `Store.migrate()` (`internal/store/store.go`) читает `schema_migrations` и применяет неизвестные базе файлы `migrations/NNNN_*.sql` по возрастанию. Версии из базы, которых нет среди файлов, не проверяются. Миграция пока одна, `0001_init.sql`. Базу через `store.Open` открывают `app.New` (для `metagit serve`) и `metagit owner create`.
- `app.New` перед открытием базы перезаписывает хуки и `bin/askpass` с путем из `os.Executable()`, а порты `metagit serve` открывает только после успешного `app.New`.
- Последние версии на октябрь 2026: GoReleaser v2.18, `goreleaser/goreleaser-action` v7.

## Goals / Non-Goals

**Goals:**

- Один источник версии - git-тег. Номер версии нигде в репозитории не записывается.
- Релизный бинарник собирается теми же командами `make`, что и в CI, и проходит те же проверки.
- Перед тегом можно прогнать весь релиз локально, ничего не публикуя.

**Non-Goals:**

- Синхронизация `"version"` в `web/package.json` с тегом: пакет `private`, поле ни на что не влияет.
- Воспроизводимые сборки (одинаковые байты архивов при повторной сборке).
- Изменения в `Makefile` и `ci.yml`.

## Decisions

### 1. GoReleaser и `.goreleaser.yaml`

Набросок конфигурации:

```yaml
version: 2
project_name: metagit

before:
  hooks:
    - test -f LICENSE
    - make web

builds:
  - main: ./cmd/metagit
    binary: metagit
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X github.com/ekalinin/metagit/internal/cli.version={{ .Version }}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
    files: [LICENSE, README.md, INSTALL.md]

checksum:
  name_template: checksums.txt

changelog:
  use: git
  sort: asc
  filters:
    exclude:
      - '^docs\(openspec\)'
      - '^Merge '
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))??!?:.+$'
      order: 0
    - title: Fixes
      regexp: '^.*?fix(\(.+\))??!?:.+$'
      order: 1
    - title: Other
      order: 999

release:
  draft: false
```

- **Веб собирается один раз** в `before.hooks` через `make web`, а не в каждой сборке: результат не зависит от платформы. Флаги `CGO_ENABLED=0` и путь `./cmd/metagit` повторяют `make go`.
- **Имена архивов без версии** (`metagit_linux_amd64.tar.gz`), чтобы `releases/latest/download/<архив>` был постоянным адресом для `INSTALL.md` и лендинга. Версию скачанного файла показывает `metagit version`.
- **`test -f LICENSE` в `before.hooks`.** Шаблон из `files`, который ничего не нашел, GoReleaser пропускает без ошибки, и архивы собрались бы без лицензии. Хук останавливает сборку, если `LICENSE` нет.
- **Changelog из git.** `use: git` берет коммиты между тегами из локальной истории, поэтому checkout нужен с `fetch-depth: 0`. GoReleaser вызывает `git log` без `--no-merges` и сравнивает фильтры и группы с первой строкой сообщения коммита. Поэтому merge-коммиты исключаются фильтром `^Merge `, а коммиты из веток остаются: в них тип и scope, по которым идет группировка.
- **Пробный прогон в два шага.** `goreleaser release --snapshot --clean` собирает архивы и `checksums.txt` в `dist/`, но changelog в этом режиме не генерируется. Changelog проверяется прогоном `goreleaser release --skip=publish --clean` на локальном, еще не отправленном теге: он пишет `dist/CHANGELOG.md`, а токен GitHub при `--skip=publish` не нужен.
- `{{ .Version }}` - тег без `v`. `-s -w` убирают таблицу символов и DWARF; информация о сборке Go (`debug.ReadBuildInfo`) при этом остается.

Альтернатива - свой workflow поверх `make` и `gh release create`. Владелец выбрал GoReleaser: группировка changelog и пробный прогон `--snapshot` есть без самописного shell.

### 2. Workflow `release`

Новый `.github/workflows/release.yml`:

```yaml
name: release

on:
  push:
    tags: ['v*']

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - uses: actions/setup-node@v5
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: make lint
      - run: make test
      - uses: goreleaser/goreleaser-action@v7
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- Триггер только `push` тегов `v*`, как решил владелец. GoReleaser сам отклоняет тег, который не разбирается как SemVer.
- `make lint` и `make test` идут в том же job перед GoReleaser: тег мог прийти на коммит, который `ci.yml` еще не проверил или проверил с ошибкой.
- `contents: write` нужен для создания Release. Права даются только этому workflow, `ci.yml` остается с правами по умолчанию.
- Тег пушит мейнтейнер своими учетными данными, поэтому ограничение `GITHUB_TOKEN` (событие, созданное им, не запускает другие workflow) здесь не мешает.

### 3. Версия в бинарнике

В `internal/cli` новый файл с переменной `version = "dev"`, которую релиз заменяет через `-X`, и функцией, возвращающей строку версии:

```
version  vcs.revision  vcs.modified  ->  строка
0.1.0    3dc143a...    false             0.1.0 (3dc143a)
dev      3dc143a...    true              dev (3dc143a, modified)
dev      (нет)         -                 dev
```

- Коммит и признак изменений берутся из `debug.ReadBuildInfo()` (`vcs.revision`, `vcs.modified`) и для релиза, и для `make build`: `go build` в git-checkout записывает их сам. Отдельный `-X` для коммита не нужен, у коммита один источник.
- `metagit version` и `metagit --version` - один `case` в `switch` в `cli.go`, как `help`, `-h`, `--help`. Печатают `metagit ` + строка версии. В справке строка `version        show the metagit version`.
- В `serve.go` к `log.Info("metagit started", ...)` добавляется поле `"version"` с той же строкой.

Альтернатива: переменная в `package main` и передача версии в `cli.Run`. Отклонено: меняется сигнатура `Run` и все ее вызовы в тестах, а справка и команды уже живут в `internal/cli`.

### 4. Проверка версии схемы в `migrate()`

После чтения `schema_migrations` и списка файлов миграций, до применения первой миграции:

```
maxApplied = max(versions in schema_migrations)   // 0 for a new database
maxKnown   = max(NNNN of migrations/*.sql)
if maxApplied > maxKnown:
    error "database schema version <maxApplied> is newer than this metagit supports (<maxKnown>):
           install a newer metagit or restore the data directory from a backup"
```

- Проверка в `migrate()`, поэтому ее получают все команды, которые вызывают `store.Open`. `CREATE TABLE IF NOT EXISTS schema_migrations` на существующей базе ничего не меняет, а миграции не применяются, так что база остается прежней.
- `metagit serve` печатает ошибку через существующую строку `cannot start` и выходит с кодом 1 до `net.Listen`. `metagit owner create` печатает `error: ...` и выходит с кодом 1 до создания владельца.
- Текст ошибки на английском, как остальные сообщения сервера.
- Первый релиз `v0.1.0` уже содержит проверку, поэтому ее нет только у бинарников, собранных до этого изменения. Релизов таких бинарников нет.

Альтернатива: хранить в базе версию metagit и сравнивать версии бинарника. Отклонено: откат опасен только при новой схеме, а откат между релизами без новых миграций безопасен и должен работать.

### 5. Документация и лендинг

- **`INSTALL.md`.**
  - "Требования": Go и Node.js нужны только для сборки из исходников.
  - Новый раздел "Установка готового бинарника" перед "Сборкой": скачать архив своей платформы и `checksums.txt` с `releases/latest/download/`, проверить сумму (`sha256sum -c checksums.txt --ignore-missing` на Linux, команда для macOS проверяется при реализации), распаковать, `metagit version`.
  - В "Установке на сервер" бинарник берется из архива или из `bin/` после сборки.
  - Новые разделы "Обновление" (остановить сервис, скопировать каталог данных и мастер-ключ, если он хранится отдельно, заменить бинарник, запустить, проверить версию в логе) и "Откат" (остановить, вернуть копию каталога данных, сделанную перед обновлением, поставить прежний бинарник, запустить). В "Обновлении" объясняется, почему сервис останавливают до замены: хуки вызывают бинарник по тому же пути, и до перезапуска новый бинарник работал бы со старым сервисом. В "Откате" объясняется, что без копии каталога данных прежний бинарник может отказаться запускаться, если новая версия изменила схему базы.
- **`docs/releasing.md`** (на русском, как `docs/testing.md`):
  - предусловия: `LICENSE` в `main`, зеленый `ci` на коммите;
  - выбор номера по SemVer;
  - пробный прогон: `goreleaser check`, `goreleaser release --snapshot --clean` (архивы в `dist/`), затем локальный тег и `goreleaser release --skip=publish --clean` (changelog в `dist/CHANGELOG.md`);
  - push тега с `main` отдельной командой;
  - проверка релиза;
  - что делать при ошибке: если релиз не опубликован, исправить через PR, удалить тег локально и на GitHub и поставить заново; если опубликован, выпустить следующую patch-версию, тег не переставлять.
- **`.gitignore`**: `/dist/`, чтобы результат пробного прогона не попал в коммит и не делал рабочую копию измененной.
- **`README.md` и `README.ru.md`**:
  - под строкой выбора языка два бейджа shields.io: версия Go из `go.mod` и последний релиз со ссылкой на страницу релизов;
  - в разделе разработки ссылка на `docs/releasing.md`.
  
  Бейджи - внешние картинки, но это README, а не лендинг: требование лендинга о сторонних ресурсах их не касается.
- **`site/index.html`**: в "Quick start" после абзаца с требованиями предложение о готовых бинарниках для Linux и macOS (amd64, arm64) со ссылкой на `https://github.com/ekalinin/metagit/releases`. Утверждение опирается на новый раздел `INSTALL.md`. Шаг "Build" остается.
- **`docs/testing.md`**: раздел `releases` в таблице соответствия сценариев и проверки первого релиза в разделе разовых проверок.

## Risks / Trade-offs

- [GoReleaser отказывается работать с измененной рабочей копией, если `make lint` или `make test` оставят неигнорируемые файлы] → Перед первым тегом выполнить пробный прогон в чистом клоне. Ошибка безопасна: релиз не публикуется, тег можно поставить заново.
- [Release публикуется сразу, и ошибочный тег сразу виден пользователям] → Владелец выбрал публикацию без черновика. В `docs/releasing.md` описаны пробный прогон и действия при ошибке.
- [`INSTALL.md` и лендинг ссылаются на `releases/latest/download/` с момента мержа, а до первого тега там 404] → Тег `v0.1.0` ставится сразу после мержа (Migration Plan).
- [Одинаковые имена архивов у разных версий: скачанные файлы не отличить по имени] → Версию показывает `metagit version` и страница релиза. Цена постоянного адреса.
- [`vcs.modified` учитывает и неотслеживаемые файлы, например `.TODO.md`, и локальная сборка показывает `modified`] → Принимается: это сборка разработчика. Релиз собирается в чистом checkout CI.
- [Бинарник для macOS не подписан и не нотаризован] → `curl` не ставит файлу признак карантина, поэтому `INSTALL.md` скачивает архив через `curl`. Нотаризация вне объема.
- [`go install github.com/ekalinin/metagit/cmd/metagit@<версия>` собирает бинарник без веб-интерфейса, `dist/app` в `.gitignore`] → `INSTALL.md` этот способ не упоминает, такой бинарник показывает страницу о том, что интерфейс не собран.
- [Прежний бинарник, отказавшийся запускаться, уже успел перезаписать хуки своим путем] → При замене бинарника на том же месте путь не меняется. Новый бинарник перезаписывает хуки при своем старте.
- [Changelog первого релиза включает всю историю] → Принимается: предыдущего тега нет.
- [Регулярные выражения changelog не совпадут с сообщениями коммитов] → Прогон `--skip=publish` на локальном теге показывает changelog до публикации. Сценарий "Состав changelog" проверяется на нем.

## Migration Plan

1. Мерж ветки `chore/license` с `LICENSE` (MIT) в `main`.
2. Мерж ветки этого изменения в `main`. `pages.yml` публикует обновленный лендинг.
3. Пробный прогон в чистом клоне `main`: `--snapshot`, затем локальный тег `v0.1.0` и `--skip=publish`.
4. Push тега `v0.1.0` отдельной командой.
5. Проверка релиза: четыре архива и `checksums.txt`, состав changelog, скачивание с `releases/latest/download/` и `metagit version` на Linux и macOS.

Откат: удалить Release и тег на GitHub, откатить коммиты. Уже поставленные бинарники продолжают работать: у `v0.1.0` нет новых миграций.

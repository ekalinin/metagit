# Tasks

## 1. Версия в бинарнике

- [x] 1.1 Добавить в `internal/cli` переменную `version = "dev"` и функцию строки версии: коммит и признак изменений из `debug.ReadBuildInfo()` (`vcs.revision`, `vcs.modified`), форматирование отдельной чистой функцией (design, раздел 3); проверка: табличный тест форматирования (`0.1.0`+коммит → `0.1.0 (3dc143a)`, `dev`+коммит+изменения → `dev (3dc143a, modified)`, `dev` без коммита → `dev`) проходит в `go test ./internal/cli`
- [x] 1.2 Команды `version` и `--version` в `switch` в `internal/cli/cli.go` и строка `version` в справке; проверка: тест через `runCLI` - `version` и `--version` дают одинаковый вывод `metagit dev...` и код 0, `help` содержит `version` (сценарий "Флаг версии")
- [x] 1.3 Поле `version` в строке `metagit started` в `internal/cli/serve.go`; проверка: `internal/e2e` `TestEndToEnd` проверяет, что лог `metagit serve` содержит строку `metagit started` с `version=` (сценарий "Версия в логе")
- [x] 1.4 Проверить локальную сборку; проверка: сценарий "Локальная сборка с изменениями" - после `make build` в рабочей копии с незакоммиченными изменениями `./bin/metagit version` печатает `metagit dev (<первые 7 символов HEAD>, modified)`

## 2. База новее бинарника

- [x] 2.1 Проверка версии схемы в `Store.migrate()` до применения миграций, текст ошибки по design, раздел 4; проверка: тест в `internal/store/store_test.go` - база с записью в `schema_migrations` на 1 больше последней миграции при повторном `Open` дает ошибку с обеими версиями и обоими советами, содержимое `schema_migrations` не изменилось; существующие тесты `store` проходят
- [x] 2.2 Тесты команд на такой базе в `internal/cli`; проверка: `owner create` завершается с кодом 1, stderr содержит текст ошибки, владелец не создан (сценарий "Создание владельца на новой базе"); `serve` с `--http-addr 127.0.0.1:0 --ssh-addr 127.0.0.1:0` сразу завершается с кодом 1 и строкой `cannot start` с текстом ошибки, тест ограничен таймаутом контекста (сценарий "Старый бинарник после обновления")

## 3. Сборка релиза

- [x] 3.1 Создать `.goreleaser.yaml` по design, раздел 1 (`test -f LICENSE` и `make web` в `before.hooks`, 4 платформы, `CGO_ENABLED=0`, `-X github.com/ekalinin/metagit/internal/cli.version={{ .Version }}`, архивы `metagit_<os>_<arch>.tar.gz` с `LICENSE`, `README.md`, `INSTALL.md`, `checksums.txt`, changelog с группами и фильтрами, публикация без черновика); проверка: `goreleaser check` проходит
- [x] 3.2 Добавить `/dist/` в `.gitignore`; проверка: после пробного прогона `git status --porcelain` не показывает `dist/`
- [x] 3.3 Создать `.github/workflows/release.yml` по design, раздел 2 (`push` тегов `v*`, `contents: write`, checkout с `fetch-depth: 0`, setup-go и setup-node как в `ci.yml`, `make lint`, `make test`, `goreleaser/goreleaser-action@v7` с `version: '~> v2'` и `args: release --clean`); проверка: файл разбирается как YAML (`ruby -ryaml -e 'YAML.load_file(".github/workflows/release.yml")'`), триггер и права совпадают с design
- [x] 3.4 Пробный прогон `goreleaser release --snapshot --clean` в чистом клоне ветки, где есть `LICENSE` (после мержа `chore/license` и rebase на `main`); проверка: в `dist/` четыре архива `metagit_{linux,darwin}_{amd64,arm64}.tar.gz` и `checksums.txt`, `tar tzf` каждого архива показывает `metagit`, `LICENSE`, `README.md`, `INSTALL.md`, проверка сумм на macOS проходит; бинарник `darwin_arm64` печатает версию снапшота в `metagit version`, а его `metagit serve` открывает веб-консоль, а не страницу о несобранном интерфейсе (сценарии "Бинарник из архива", "Проверка контрольной суммы")
- [x] 3.5 Проверить хук `LICENSE`; проверка: в клоне без `LICENSE` `goreleaser release --snapshot --clean` завершается ошибкой на хуке `test -f LICENSE`
- [x] 3.6 Проверить changelog на локальном неотправленном теге: `git tag v0.1.0`, `goreleaser release --skip=publish --clean`, затем `git tag -d v0.1.0`; проверка: сценарий "Состав changelog" - в `dist/CHANGELOG.md` коммиты `feat` в группе "Features", `fix` в "Fixes", нет коммитов `docs(openspec)` и `Merge pull request`

## 4. Документация и лендинг

- [x] 4.1 `INSTALL.md` по design, раздел 5: требования (Go и Node.js только для сборки), раздел "Установка готового бинарника" перед "Сборкой" (`curl` с `releases/latest/download/`, проверка суммы для Linux и macOS, распаковка, `metagit version`), бинарник из архива или `bin/` в "Установке на сервер", разделы "Обновление" и "Откат"; проверка: команды раздела установки выполнены на macOS с архивом из пробного прогона 3.4 вместо скачивания, каждое утверждение разделов "Обновление" и "Откат" соответствует design, раздел 4 и `internal/app/app.go` (перезапись хуков при старте)
- [x] 4.2 Создать `docs/releasing.md` по design, раздел 5; проверка: команды пробного прогона совпадают с выполненными в 3.4 и 3.6, push тега отдельной командой, описаны оба случая ошибки (релиз не опубликован и опубликован)
- [x] 4.3 Бейджи версии Go и последнего релиза под строкой выбора языка и ссылка на `docs/releasing.md` в разделе разработки в `README.md` и `README.ru.md`; проверка: бейджи отображаются в предпросмотре markdown на GitHub, ссылка бейджа релиза ведет на `https://github.com/ekalinin/metagit/releases`, ссылка на `docs/releasing.md` открывается
- [x] 4.4 В `site/index.html` в "Quick start" после абзаца с требованиями предложение о готовых бинарниках для Linux и macOS (amd64, arm64) со ссылкой на `https://github.com/ekalinin/metagit/releases`; проверка: сценарий "Путь к установке" - ссылка на страницу релизов видна в быстром старте при `python3 -m http.server -d site`, утверждение есть в `INSTALL.md`, `grep -n '<script' site/index.html` ничего не находит
- [x] 4.5 Раздел `releases` в таблице соответствия сценариев `docs/testing.md` (тесты из 1.x и 2.x, пробные прогоны из 3.x, проверки первого релиза из 5.x; "Обновление" - существующие `TestMigrationsCreateTables` и `TestReopenDoesNotReapplyMigrations`) и обновленная строка "Путь к установке" в разделе `project-landing`; проверка: каждый сценарий из `specs/releases/spec.md` есть в таблице
- [x] 4.6 Общая проверка ветки; проверка: `make lint` и `make test` проходят, `openspec validate add-release-process --strict` проходит, `git diff --stat main` не показывает `Makefile`, `ci.yml` и `pages.yml`

## 5. Первый релиз (шаги владельца после мержа)

- [ ] 5.1 Влить ветку `chore/license` с `LICENSE` (MIT) в `main` до тега; проверка: `git show main:LICENSE` выводит текст MIT
- [ ] 5.2 Поставить тег `v0.1.0` на `main` и отправить его отдельной командой `git push origin v0.1.0`; проверка: сценарий "Тег версии публикует релиз" - workflow `release` успешен, Release `v0.1.0` опубликован (не черновик) с четырьмя архивами и `checksums.txt`
- [ ] 5.3 Проверить релиз на Linux и macOS по `INSTALL.md`; проверка: сценарии "Последний релиз по постоянному адресу", "Проверка контрольной суммы", "Версия релизного бинарника" - архив скачан с `releases/latest/download/`, сумма сходится, `metagit version` печатает `metagit 0.1.0 (<первые 7 символов коммита тега>)`
- [ ] 5.4 Проверить, что релизы не создаются без тега; проверка: сценарий "Push без тега" - в истории Actions у push в `main` без тега нет запусков `release`. Сценарий "Тесты не прошли" проверяется разбором workflow: `make test` идет до шага GoReleaser в том же job

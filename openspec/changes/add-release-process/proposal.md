# Proposal

## Why

У metagit нет ни одного релиза: чтобы поставить его на сервер, нужно собрать его из исходников с Go 1.26 и Node.js 24, а собранный бинарник не сообщает свою версию. С релизами появятся обновления и откаты, а сейчас старый бинарник молча запускается на базе, схему которой уже изменила более новая версия.

## What Changes

- Релиз по push тега `v*`. Новый workflow `release` запускает `make lint`, `make test` и GoReleaser. GoReleaser один раз собирает веб-интерфейс, затем бинарники для linux и darwin под amd64 и arm64, упаковывает каждый в tar.gz вместе с `LICENSE`, `README.md` и `INSTALL.md`, считает `checksums.txt`, составляет changelog из коммитов (группы feat, fix и остальное, без `docs(openspec)` и merge-коммитов) и сразу публикует GitHub Release.
- Версия в бинарнике: команда `metagit version` и флаг `--version`, поле `version` в строке лога `metagit started`. Релизная версия приходит из тега, у локальной сборки версия `dev` и коммит из информации о сборке Go.
- Защита от отката: metagit не запускается на базе, схема которой новее, чем знает бинарник, и сообщает, что делать.
- Документация:
  - в `INSTALL.md` установка готового бинарника идет перед сборкой из исходников, появляются разделы про обновление и откат;
  - инструкция для мейнтейнера, как выпустить релиз;
  - бейджи версии Go и последнего релиза в `README.md` и `README.ru.md`.
- Лендинг: ссылка на страницу релизов в блоке установки.
- Первый релиз - `v0.1.0`.

### Non-goals

- Docker-образ, Homebrew, пакеты deb и rpm.
- Windows.
- release-please, автоматический выбор номера версии, `CHANGELOG.md` в репозитории.
- Подпись артефактов (cosign), attestations, SBOM.
- Черновики релизов.
- Версия в веб-консоли и в API.
- Миграции вниз и автоматический откат схемы базы.
- Файл `LICENSE`: лицензия MIT добавляется отдельной веткой до первого тега.

## Capabilities

### New Capabilities

- `releases`: выпуск версий metagit - релиз из тега с архивами для платформ, контрольными суммами и changelog; версия бинарника; отказ запускаться на базе со схемой новее бинарника.

### Modified Capabilities

- `project-landing`: в содержание страницы добавляется ссылка на страницу релизов, сценарий "Путь к установке" включает ее.

## Impact

- Новые файлы: `.goreleaser.yaml`, `.github/workflows/release.yml`, `docs/releasing.md`.
- `internal/cli`: команда `version` и флаг `--version`, строка в справке, поле `version` в логе `metagit serve`.
- `internal/store/store.go`: проверка версии схемы в `migrate()`. Проверка действует для `metagit serve` и `metagit owner create`: обе команды открывают базу через `store.Open`.
- `INSTALL.md`, `README.md`, `README.ru.md`, `site/index.html`, `docs/testing.md`, `.gitignore` (`/dist/` пробного прогона GoReleaser).
- `Makefile`, `ci.yml` и `pages.yml` не меняются.
- Новая зависимость в CI: GoReleaser v2 через `goreleaser/goreleaser-action`. Локально GoReleaser нужен только для пробного прогона.
- Предусловие первого релиза: `LICENSE` в `main`, иначе GoReleaser не соберет архивы.

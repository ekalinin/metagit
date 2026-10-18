# Выпуск релиза

Релиз собирает workflow `release` (`.github/workflows/release.yml`) с GoReleaser (`.goreleaser.yaml`), когда в репозиторий приходит тег `v*`. Workflow запускает `make lint` и `make test`, затем GoReleaser собирает бинарники для linux и darwin (amd64, arm64), архивы с `LICENSE`, `README.md` и `INSTALL.md`, `checksums.txt`, changelog и сразу публикует GitHub Release.

Номер версии - это тег. В файлах репозитория он не записывается, в бинарник попадает при сборке.

## Перед релизом

- `LICENSE` есть в `main`: без него GoReleaser останавливается на хуке `test -f LICENSE`.
- Workflow `ci` на последнем коммите `main` прошел.
- Номер выбран по [SemVer](https://semver.org): `vMAJOR.MINOR.PATCH`.

Changelog строится из коммитов с прошлого тега: группы Features (`feat`), Fixes (`fix`) и Other. Коммиты `docs(openspec)` и merge-коммиты исключаются, а коммиты из веток попадают в changelog со своими сообщениями.

## Пробный прогон

Нужен GoReleaser v2 (`brew install goreleaser`). В чистом клоне `main`:

```sh
goreleaser check
goreleaser release --snapshot --clean      # архивы и checksums.txt в dist/
```

В режиме `--snapshot` changelog не строится. Чтобы увидеть его, поставьте тег только локально и соберите релиз без публикации:

```sh
git tag v0.1.0
goreleaser release --skip=publish,announce --clean
cat dist/CHANGELOG.md
git tag -d v0.1.0
```

## Выпуск

```sh
git switch main
git pull
git tag -a v0.1.0 -m v0.1.0
```

Push тега - отдельной командой:

```sh
git push origin v0.1.0
```

Затем:

1. Дождитесь успешного запуска workflow `release` в GitHub Actions.
2. На странице релиза проверьте четыре архива, `checksums.txt` и changelog.
3. Скачайте архив по `INSTALL.md` и проверьте `metagit version`.

## Если что-то пошло не так

- **Workflow упал, релиз не опубликован.** Исправьте причину через PR в `main`, затем удалите тег и поставьте его заново на новый коммит. Если GoReleaser успел создать Release, удалите и его на странице релизов.

  ```sh
  git push --delete origin v0.1.0
  git tag -d v0.1.0
  ```

- **Релиз опубликован с ошибкой.** Тег не переставляйте: его уже могли скачать. Выпустите следующую patch-версию, адрес `releases/latest/download/` перейдет на нее.

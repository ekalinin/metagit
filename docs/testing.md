# Тестирование

## Как запустить

```sh
make test                  # Go и веб-тесты
go test -race ./...        # Go-тесты с детектором гонок
METAGIT_TEST_LOG=1 go test ./internal/app/   # с журналом сервиса
```

## Уровни тестов

| Уровень | Где | Что используется |
|---|---|---|
| Модульные | пакеты `internal/*` | отдельные функции и типы |
| Интеграционные | `internal/app`, `internal/api`, `internal/transfer` | настоящий git, собранный бинарник metagit для хуков, сервис внутри процесса теста (`internal/apptest`), тестовый HTTPS-хостинг с `git http-backend` (`internal/testutil/fakehost`) |
| Сквозной | `internal/e2e` | `metagit serve` отдельным процессом, CLI, API и git, два SSH-хостинга |
| Веб | `web/src/**/*.test.ts(x)` | компонентные тесты (Vitest, jsdom, Testing Library), `fetch` подменен |

## Сквозной тест

Один тест, `TestEndToEnd` в `internal/e2e/e2e_test.go`. Он собирает бинарник `metagit`, запускает `metagit serve` отдельным процессом и работает с ним так же, как владелец: через CLI, веб-API и git. "Хостинги" - два SSH-сервера со своими репозиториями, которые принимают общий deploy-ключ.

SSH-хостинги выбраны потому, что `metagit serve` убирает переменные `GIT_*` из окружения git. Поэтому отдельному процессу нельзя передать переменную, с которой git принял бы самоподписанный сертификат тестового HTTPS-хостинга.

Шаги идут друг за другом:

1. **Создание владельца.** `metagit owner create` с паролем из stdin. Проверяется, что команда завершилась успешно.
2. **Вход и настройка через API.** Вход, git-токен, проект `foo`, защита `refs/heads/main`, два SSH-target'а (`hosting1`, `hosting2`) с приватным ключом.
3. **Обычный push.** `git push` веток `main` и `feature` по HTTP с токеном. В выводе push есть `hosting1: delivered` и `hosting2: delivered`, на обоих хостингах нужные коммиты.
4. **Force-push в незащищенную ветку.** `feature` переписана. Оба хостинга получают новый коммит, а в списке архива через API есть запись с источником `push` на прежний коммит.
5. **Force-push в защищенную ветку.** Push в `main` отклонен с сообщением `is protected`.
6. **Расхождение на хостинге.** Кто-то пушит коммит прямо в `hosting1`, затем владелец пушит в metagit другой коммит. В выводе `hosting1: delivered; diverged`, `main` на `hosting1` перезаписан значением из metagit, коммит с хостинга есть в архиве с источником `target`.
7. **Хостинг недоступен.** `hosting2` остановлен, отправлен новый коммит. Push все равно успешен, в выводе `hosting1: delivered` и `hosting2: error`, API показывает у `hosting2` состояние ошибки с текстом.
8. **Хостинг вернулся.** `hosting2` запущен на том же адресе, через API запущена ручная доставка. Target становится синхронизированным, на обоих хостингах последний `main`.
9. **Корректная остановка.** `serve` получает SIGTERM, завершается с кодом 0 в течение 30 секунд, база остается на диске.

Сквозной тест не покрывает перечисленное ниже - это проверяется в других местах:

- HTTPS-target'ы с токеном, автоматические повторы по таймеру, строка о медленном target'е в выводе push, созданные только на хостинге ветки, удаление ветки, теги, восстановление после сбоя и маскировка секретов: интеграционные тесты в `internal/app`. Они тоже используют настоящий бинарник и его хуки, но сервис работает внутри процесса теста.
- Экспорт и импорт: `internal/transfer`.
- API: `internal/api`.
- Веб-интерфейс: компонентные тесты в `web/src`. Проверка в настоящем браузере на двух экземплярах была разовым скриптом вне репозитория, поэтому она не входит ни в `make test`, ни в CI.

## Соответствие сценариев спецификаций тестам

Пакеты Go указаны относительно `internal/`, веб-тесты - относительно `web/src/`.

### git-hosting

| Сценарий | Тесты |
|---|---|
| Создание проекта | `repo`: `TestCreateEmptyRepository`; `api`: `TestProjectsAPI` (clone пустого проекта по HTTP) |
| Имя уже занято | `store`: `TestProjectsAndCascade`; `api`: `TestProjectsAPI` |
| Недопустимое имя | `repo`: `TestValidateName`; `api`: `TestProjectsAPI` |
| Push с действующим токеном | `githttp`: `TestCloneAndPushWithToken`, `TestLargePushUsesChunkedBody` |
| Запрос без токена или с неверным токеном | `githttp`: `TestRequestsWithoutValidToken`, `TestRevokedToken` |
| Несуществующий проект | `githttp`: `TestUnknownProject`; `gitssh`: `TestCloneAndPushOverSSH` |
| Push с зарегистрированным ключом | `gitssh`: `TestCloneAndPushOverSSH`; `app`: `TestFetchArchiveRefOverHTTPAndSSH` |
| Незарегистрированный ключ | `gitssh`: `TestUnregisteredKeyRejected` |
| Запрос оболочки | `gitssh`: `TestShellAndOtherCommandsRejected` |
| Force-push в защищенную ветку | `app`: `TestProtectedRefs`; `e2e`: `TestEndToEnd` |
| Удаление защищенной ветки | `app`: `TestProtectedRefs` |
| Перемещение защищенного тега | `app`: `TestProtectedRefs` |
| Fast-forward в защищенную ветку | `app`: `TestProtectedRefs` |
| Force-push в незащищенную ветку | `app`: `TestProtectedRefs` |
| Push в служебный ref | `app`: `TestProtectedRefs`; `archive`: `TestCheck` |

### mirror-replication

| Сценарий | Тесты |
|---|---|
| Первая доставка на новый target | `app`: `TestFirstDeliveryToNewTarget` |
| URL с секретом | `app`: `TestTargetURLWithSecretRejected`; `replicate`: `TestValidateURL`; `api`: `TestTargetsAPI` |
| Выключенный target | `app`: `TestDisabledTargetIsNotContacted` |
| Хостинг недоступен | `app`: `TestRetriesUntilHostIsBack`; `e2e`: `TestEndToEnd` |
| Ветка, созданная только на хостинге | `app`: `TestDeliveryTouchesOnlyMetagitRefs` |
| Удаление ветки в metagit | `app`: `TestDeliveryTouchesOnlyMetagitRefs` |
| Прочие пространства refs | `app`: `TestDeliveryTouchesOnlyMetagitRefs` |
| Force-push в незащищенную ветку | `app`: `TestDeliveryTouchesOnlyMetagitRefs`; `e2e`: `TestEndToEnd` |
| Несколько push подряд | `app`: `TestSeveralPushesEndWithLatestState` |
| Merge в веб-интерфейсе хостинга | `app`: `TestDivergedTargetIsArchivedAndOverwritten`; `api`: `TestStatusEventsAndArchiveAPI`; `e2e`: `TestEndToEnd` |
| Совпадающая ветка с таким же именем | `app`: `TestMatchingOrBehindRefsAreNotDivergence` |
| Ветка на хостинге отстает | `app`: `TestMatchingOrBehindRefsAreNotDivergence` |
| Временная ошибка хостинга | `app`: `TestRetriesUntilHostIsBack`, `TestDeliveryWithToken` |
| Хостинг снова доступен | `app`: `TestRetriesUntilHostIsBack` |
| Все target'ы ответили быстро | `app`: `TestPushOutputShowsDeliveryStatus` |
| Медленный target | `app`: `TestPushOutputShowsDeliveryStatus` |
| Просмотр target'а | `api`: `TestTargetsAPI`; `pages/TargetForm.test.tsx`: "on edit the stored secret is never shown and an empty field keeps it" |
| Ошибка доставки с токеном в URL запроса | `app`: `TestSecretsAreMaskedInErrors`; `secrets`: `TestMask` |

### ref-archive

| Сценарий | Тесты |
|---|---|
| Force-push в ветку | `app`: `TestDestructivePushCreatesArchive`; `e2e`: `TestEndToEnd` |
| Удаление ветки | `app`: `TestDestructivePushCreatesArchive` |
| Перемещение тега | `app`: `TestDestructivePushCreatesArchive` |
| Fast-forward | `app`: `TestDestructivePushCreatesArchive` |
| Коммит, сделанный на хостинге | `app`: `TestDivergedTargetIsArchivedAndOverwritten`; `archive`: `TestSaveTargetStateUsesUniqueNames`; `e2e`: `TestEndToEnd` |
| Архив не уходит на хостинги | `app`: `TestDeliveryTouchesOnlyMetagitRefs` |
| Сбой между push и архивом | `app`: `TestArchiveRecoveryAfterCrash` |
| Восстановление из архива | `app`: `TestFetchArchiveRefOverHTTPAndSSH` |

### settings-transfer

| Сценарий | Тесты |
|---|---|
| Экспорт | `transfer`: `TestExport`; `api`: `TestExportImportAPI`; `pages/SettingsPage.test.tsx`: "export sends the passphrase and downloads the file" |
| Восстановление на новом сервере | `transfer`: `TestImportRestoresSettingsAndKeepsHostingsSafe`; `api`: `TestExportImportAPI` |
| Неверный пароль | `transfer`: `TestImportIsAllOrNothing`; `api`: `TestExportImportAPI` |
| Конфликт имен | `transfer`: `TestImportIsAllOrNothing` |
| Импорт без push | `transfer`: `TestImportRestoresSettingsAndKeepsHostingsSafe` |
| Заполнение проекта из локальной копии | `transfer`: `TestImportRestoresSettingsAndKeepsHostingsSafe` |

### web-console

| Сценарий | Тесты |
|---|---|
| Запрос без сессии | `api`: `TestLoginAndSession`; `App.test.tsx`: "a private page without a session redirects to login and returns after login" |
| Неверный пароль | `api`: `TestLoginAndSession`; `auth`: `TestLoginSessionLogout`; `App.test.tsx`: "wrong password shows the server error" |
| Сервер без владельца | `api`: `TestServerWithoutOwner`; `App.test.tsx`: "owner_exists false shows the owner create instruction and no form" |
| Удаление проекта | `api`: `TestProjectsAPI`; `pages/ProjectsPage.test.tsx`: "deletion is enabled only after the exact project name is typed" |
| Неверное подтверждение | `api`: `TestProjectsAPI`; `pages/ProjectsPage.test.tsx`: "deletion is enabled only after the exact project name is typed" |
| Замена токена | `api`: `TestTargetsAPI`; `pages/TargetForm.test.tsx`: "on edit a typed token is sent" |
| Ручная доставка | `api`: `TestTargetsAPI`; `app`: `TestManualSyncDoesNotWaitForRetry`; `e2e`: `TestEndToEnd` |
| Добавление защиты | `api`: `TestProtectedTokensAndKeysAPI`; `pages/ProjectPage.test.tsx`: "adding a protected ref pattern sends it and shows it in the list" |
| Создание токена | `api`: `TestProtectedTokensAndKeysAPI`; `auth`: `TestTokens`; `pages/SettingsPage.test.tsx`: "a created token value is shown once and the list shows only name and dates" |
| Отзыв токена | `githttp`: `TestRevokedToken`; `api`: `TestProtectedTokensAndKeysAPI`; `pages/SettingsPage.test.tsx`: "revoking a token sends DELETE" |
| Расхождение в журнале | `api`: `TestStatusEventsAndArchiveAPI`; `pages/ProjectPage.test.tsx`: "event log and archive show readable labels and the git fetch command" |
| Ошибка доставки в статусе | `api`: `TestStatusEventsAndArchiveAPI`; `pages/ProjectPage.test.tsx`: "shows clone addresses and targets with state, last error and times" |
| Архив после force-push | `api`: `TestStatusEventsAndArchiveAPI`; `pages/ProjectPage.test.tsx`: "event log and archive show readable labels and the git fetch command" |
| Первое открытие | `App.test.tsx`: "a chosen mode is applied and saved, system removes it"; `theme.test.ts`: "a saved dark mode is read back, anything else means system"; цвет темы - проверка в браузере (см. ниже) |
| Ручной выбор сохраняется | `theme.test.ts`: "a chosen mode is saved and applied, system removes both"; `App.test.tsx`: "the saved mode is checked" |
| Системный режим следует за ОС | проверка в браузере (см. ниже) |
| Тема на странице входа | `App.test.tsx`: "the login page keeps the chosen mode after logout" |
| Сеть без доступа в интернет | проверка в браузере (см. ниже) |
| Состояние без цвета | `ui.test.tsx`: "a state is written as the only text of its badge", "a lamp is hidden from assistive technologies"; `pages/ProjectPage.test.tsx`: "every target row starts with a delivery line drawn for its state" |
| Браузер на русском | `App.test.tsx`: "a Russian browser gets the Russian interface and lang"; `locale.test.ts`: "the first supported browser language wins, otherwise English"; `lang` до загрузки бандла - проверка в браузере (см. ниже) |
| Неподдерживаемый язык браузера | `App.test.tsx`: "an unsupported browser language gets English"; `locale.test.ts`: "the first supported browser language wins, otherwise English" |
| Ручной выбор сохраняется (язык) | `App.test.tsx`: "a saved choice wins over a Russian browser"; `locale.test.ts`: "a saved language is read back, anything else means system", "a chosen mode is saved, system removes it" |
| Смена языка без перезагрузки | `App.test.tsx`: "choosing English in settings applies at once and is saved, system removes it"; `ui.test.tsx`: "formatTime follows the interface language" |
| Язык на странице входа | `App.test.tsx`: "the login page keeps the chosen language after logout" |
| Текст ошибки с сервера | `App.test.tsx`: "a server error text is shown as is" |

### project-landing

У лендинга в `site/` нет автоматических тестов. Локальный просмотр: `python3 -m http.server -d site`. При изменении `README.md` или `INSTALL.md` сверьте тексты в `site/index.html`: каждое утверждение на странице должно быть в этих файлах.

| Сценарий | Проверка |
|---|---|
| Изменение лендинга публикуется | история запусков workflow `pages` в GitHub Actions |
| Изменение сервиса не публикует лендинг | история запусков workflow `pages` в GitHub Actions |
| Ручной запуск | ручной запуск workflow `pages` после включения GitHub Pages |
| Локальный просмотр | проверка лендинга в браузере (см. ниже) |
| Первый экран | проверка лендинга в браузере |
| Путь к установке | сверка команд с `INSTALL.md` и `README.md`, ссылка на страницу релизов; проверка лендинга в браузере |
| Возможность вне версии | сверка с разделом "Что не входит в эту версию" `README.md` |
| Темная ОС | проверка лендинга в браузере |
| ОС сменила режим | проверка лендинга в браузере |
| Состояние без цвета | проверка лендинга в браузере |
| Запросы страницы | проверка лендинга в браузере |
| JavaScript выключен | проверка лендинга в браузере |
| Узкий экран | проверка лендинга в браузере |
| Обход с клавиатуры | проверка лендинга в браузере |
| Уменьшение движения | проверка лендинга в браузере |

### releases

Сборку релиза проверяют пробные прогоны GoReleaser и первый релиз по `docs/releasing.md`.

| Сценарий | Проверка |
|---|---|
| Тег версии публикует релиз | проверка первого релиза: запуск workflow `release` и страница релиза |
| Бинарник из архива | пробный прогон релиза (см. ниже) |
| Проверка контрольной суммы | пробный прогон релиза; проверка первого релиза |
| Состав changelog | пробный прогон релиза на локальном теге (см. ниже) |
| Последний релиз по постоянному адресу | проверка первого релиза |
| Тесты не прошли | разбор `.github/workflows/release.yml`: `make test` идет до шага GoReleaser в том же job |
| Push без тега | история запусков workflow `release` в GitHub Actions |
| Версия релизного бинарника | `cli`: `TestFormatVersion`; пробный прогон релиза; проверка первого релиза |
| Флаг версии | `cli`: `TestVersion` |
| Локальная сборка с изменениями | `cli`: `TestFormatVersion`; проверка `make build` (см. ниже) |
| Версия в логе | `e2e`: `TestEndToEnd` |
| Старый бинарник после обновления | `store`: `TestNewerSchemaIsRejected`; `cli`: `TestServeOnNewerDatabase` |
| Создание владельца на новой базе | `cli`: `TestOwnerCreateOnNewerDatabase` |
| Обновление | `store`: `TestMigrationsCreateTables`, `TestReopenDoesNotReapplyMigrations` |

## Разовые проверки вне `make test`

Эти проверки выполнялись один раз при разработке. Их скрипты не входят в репозиторий, и в CI они не запускаются.

- **Проверка в браузере.** Сценарии прогонялись в Google Chrome через `playwright-core` на двух запущенных экземплярах `metagit serve`:
  - вход с неверным паролем;
  - перенаправление на вход и возврат после него;
  - добавление target'а и его состояние с ошибкой;
  - поле секрета при изменении target'а остается пустым;
  - защищенные refs, журнал событий, архив;
  - токен показывается один раз;
  - экспорт на одном экземпляре и импорт на другом (в том числе с неверным паролем);
  - удаление проекта после ввода его имени.
- **Проверка оформления в браузере.** Google Chrome через `playwright-core` на трех экземплярах `metagit serve`: на первом проект с target'ами во всех состояниях (второй экземпляр по SSH - "синхронизирован", закрытый порт - "ошибка", адрес без ответа - "ожидает", выключенный target), второй экземпляр - target первого, третий - без владельца. Проверялось:
  - скриншоты всех страниц в светлой и темной теме на ширине 1280 и 375 px;
  - видимый фокус при обходе с клавиатуры;
  - выбранная тема сохраняется после перезагрузки и на странице входа после выхода;
  - в режиме "Системная" тема меняется вслед за ОС без перезагрузки;
  - бегущий пунктир у target'а в состоянии "ожидает" останавливается при `prefers-reduced-motion: reduce`;
  - все запросы страниц идут к адресу metagit, шрифты Golos Text и Martian Mono загружены для латиницы и кириллицы.
- **Проверка языка интерфейса в браузере.** Google Chrome через `playwright-core` на одном экземпляре `metagit serve` с проектом и target'ом. Проверялось:
  - inline-скрипт в `index.html` выставляет `lang` до загрузки бандла (бандл заблокирован): сохраненный `ru` в браузере `en-US` - `ru`, браузер `ru-RU` - `ru`, браузер `de-DE` - `en`, сохраненный `en` в браузере `ru-RU` - `en`;
  - браузер `ru-RU` открывает интерфейс на русском, `en-US` - на английском;
  - выбор "English" в настройках сразу, без перезагрузки, переводит шапку и страницу и действует после перезагрузки на страницах настроек, проектов и проекта, а также на странице входа после выхода;
  - на ширине 1280 и 375 px в обеих темах у страниц настроек, проектов и проекта нет горизонтальной прокрутки, английские тексты не переполняют кнопки и шапку.
- **Проверка лендинга в браузере.** Google Chrome через `playwright-core`. `site/` раздавался командой `python3 -m http.server -d site` и вторым сервером из каталога с символической ссылкой `metagit -> site`, чтобы страница открывалась по адресу `/metagit/`, как на GitHub Pages. Проверялось:
  - в корне и в `/metagit/` все запросы идут к адресу локального сервера и успешны, загружены оба шрифта и оба скриншота, якорные ссылки работают, в консоли нет ошибок;
  - в окнах 1280×800 и 375×667 без прокрутки видны название, описание и ссылка на репозиторий;
  - на ширине 320 и 375 px у страницы нет горизонтальной прокрутки, блоки кода прокручиваются внутри себя;
  - при темном режиме ОС страница и скриншоты в темной теме, после переключения ОС на светлый режим страница и скриншоты переходят в светлую тему без перезагрузки;
  - у каждого хостинга на иллюстрации есть текстовое состояние, огни скрыты от вспомогательных технологий, у скриншотов есть `alt`;
  - бегущий пунктир останавливается при `prefers-reduced-motion: reduce`;
  - с выключенным JavaScript страница отображается полностью;
  - все ссылки достижимы клавишей Tab и получают видимый контур фокуса в обеих темах;
  - контраст текста к фону не ниже 4.5:1, огней к фону страницы не ниже 3:1 в обеих темах.
- **Пробный прогон релиза.** GoReleaser 2.16 на macOS (arm64) во временном клоне `main` с `LICENSE` и изменениями ветки. Проверялось:
  - `goreleaser release --snapshot --clean` дает в `dist/` архивы `metagit_{linux,darwin}_{amd64,arm64}.tar.gz` с `metagit`, `LICENSE`, `README.md`, `INSTALL.md` и `checksums.txt`, `dist/` не виден в `git status`;
  - без `LICENSE` сборка останавливается на хуке `test -f LICENSE`;
  - на локальном теге `v0.1.0` `goreleaser release --skip=publish,announce --clean` пишет `dist/CHANGELOG.md`: коммиты `feat` в Features, `fix` в Fixes, коммитов `docs(openspec)` и merge-коммитов нет;
  - команды раздела "Установка готового бинарника" `INSTALL.ru.md` с архивом из `dist/` вместо скачивания: на macOS (`shasum`) и в контейнере Debian (`sha256sum`, linux/arm64) сумма сходится, `metagit version` печатает `metagit 0.1.0 (<коммит>)`;
  - `metagit serve` из архива darwin/arm64 отдает веб-консоль, а не страницу о несобранном интерфейсе.
- **Проверка `make build`.** В рабочей копии с незакоммиченными изменениями `./bin/metagit version` и `--version` печатают `metagit dev (<первые 7 символов HEAD>, modified)`, строка `metagit started` в логе `metagit serve` содержит то же значение `version`.
- **Шаги установки из `INSTALL.md` в чистом контейнере Debian** (git 2.47): установка, создание владельца, запуск, токен, push по HTTP, clone по SSH, экспорт. Systemd, reverse proxy с TLS и доставка на настоящие хостинги не проверялись.

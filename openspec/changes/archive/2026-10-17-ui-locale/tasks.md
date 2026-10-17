# Tasks

Команды выполняются в `web/`. Поиск кириллицы вне каталогов и тестов (дальше - "поиск кириллицы"): `ls src/*.ts src/*.tsx src/pages/*.tsx | grep -v test | xargs perl -CSD -ne 'print "$ARGV:$.: $_" if /\p{Cyrillic}/; close ARGV if eof'`. До изменений он выводит около 170 строк.

## 1. Модуль локализации

- [x] 1.1 Создать `src/locale.ts` по design, разделы 1 и 3: `Locale`, `LocaleMode`, `LOCALE_KEY`, `storedLocaleMode`, `setLocaleMode`, `resolveLocale`, `currentLocale`, `messages`, `useLocale`, `useT`, подписка на `metagit:locale` и `languagechange`; и `src/locale.test.ts`: `['ru-RU']` -> `ru`, `['de']` -> `en`, `['de-DE', 'ru']` -> `ru`, `['en-GB', 'ru']` -> `en`, пустой список -> `navigator.language`, сохраненный `en` побеждает русский браузер, неизвестное сохраненное значение -> `system`, `system` удаляет ключ; проверка: `npx vitest run src/locale.test.ts` проходит
- [x] 1.2 Создать `src/locales/en.tsx` (`en`, тип `Messages`) и `src/locales/ru.tsx` (`ru: Messages`) с группами `common`, `labels`, `api` по design, разделы 2 и 7; проверка: `npm run typecheck` проходит, а если временно удалить ключ из `ru.tsx`, падает
- [x] 1.3 В `src/test-setup.ts` подменить язык браузера на `['ru-RU']` в `beforeEach`, в `afterEach` удалять `metagit.locale` из localStorage и сбрасывать `lang` документа (design, раздел 8); проверка: `npm test` проходит без изменений в существующих тестах

## 2. Тексты из каталога

Каждая задача переносит строки файла в группу каталога в `en.tsx` и `ru.tsx`. Русские тексты переносятся без изменений, поэтому существующие тесты проверяют, что ничего не потерялось.

- [x] 2.1 `src/ui.tsx` (метки состояний и видов учетных данных в `labels`, `formatTime` с тегом `ru-RU` или `en-US` по `currentLocale()`) и `src/api.ts` (3 текста ошибок в `api`); проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в этих файлах
- [x] 2.2 `src/App.tsx`: загрузка, ошибка связи с сервером, страница не найдена, шапка, ошибка выхода, режимы и подпись темы (группа `app`); `App` вызывает `useT()` и эффектом выставляет `document.documentElement.lang` по `useLocale()` (design, раздел 5); проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в `App.tsx`
- [x] 2.3 `src/pages/LoginPage.tsx` (группа `login`); проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в файле
- [x] 2.4 `src/pages/ProjectsPage.tsx` (группа `projects`), включая фразу с `<strong>{name}</strong>` в подтверждении удаления как функцию, возвращающую JSX; проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в файле
- [x] 2.5 `src/pages/ProjectPage.tsx` (группа `project`, включая `window.confirm` и `aria-label` с именем target'а) и `src/pages/History.tsx` (группа `history`, метки событий в `labels`); проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в файлах
- [x] 2.6 `src/pages/TargetForm.tsx` (группа `targetForm`) и `src/pages/ProtectedRefs.tsx` (группа `protectedRefs`); проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы не находит строк в файлах
- [x] 2.7 `src/pages/SettingsPage.tsx` (группа `settings`), включая фразу с `<strong>` о созданном токене, `window.confirm`, `aria-label` и подсказку с `MIN_PASSPHRASE`; проверка: `npm test` и `npm run typecheck` проходят, поиск кириллицы ничего не выводит

## 3. Выбор языка

- [x] 3.1 `index.html`: `lang="en"` по умолчанию, inline-скрипт дополнительно выбирает язык по `metagit.locale` и `navigator.languages` и выставляет `lang`, комментарий ссылается на `src/locale.ts` (design, раздел 5); проверка: `npm run build` проходит, в браузере с `localStorage['metagit.locale'] = 'ru'` после перезагрузки `document.documentElement.lang` равен `ru` еще до загрузки бандла (точка останова на первом скрипте модуля)
- [x] 3.2 Секция `LanguageSection` первой карточкой на странице "Настройки": `h2`, `fieldset` со скрытой `legend`, режимы "Системный" / "System" из каталога, "Русский" и "English" одинаковые в обоих каталогах; в `src/style.css` класс `.locale-switch` добавлен к селекторам `.theme-switch` (design, раздел 6); проверка: `npm run typecheck` проходит, страница настроек в браузере на ширине 1280 и 375 px без горизонтальной прокрутки в обеих темах
- [x] 3.3 Тесты сценариев в `src/App.test.tsx`: "Браузер на русском" (`lang` равен `ru`), "Неподдерживаемый язык браузера" (`['de']`, страница входа на английском), "Ручной выбор сохраняется" (сохраненный `en`, русский браузер), "Смена языка без перезагрузки" (выбор "English" в настройках меняет заголовок страницы, ссылки шапки и `lang`, ключ сохранен; "Системный" удаляет ключ), "Язык на странице входа" (выбран `en`, выход, страница входа на английском), "Текст ошибки с сервера" (ответ `invalid name or password` показан как есть); в `src/ui.test.tsx` - `formatTime` при английском языке совпадает с `toLocaleString('en-US')`; проверка: `npm test` проходит

## 4. Документация

- [x] 4.1 `docs/testing.md`: в таблицу `web-console` добавить сценарии требования "Язык интерфейса" со ссылками на тесты из 1.1 и 3.3, в раздел разовых проверок - ручную проверку из 5.2; проверка: каждый сценарий из `specs/web-console/spec.md` этого change есть в таблице
- [x] 4.2 `README.md:50,82,84`: убрать русские названия разделов ("Настройки", "Экспорт", "Импорт"), оставить `Settings`, `Settings → Export`, `Settings → Import`; проверка: `grep -n 'Настройки' README.md` ничего не находит

## 5. Итоговая проверка

- [x] 5.1 Полная проверка; проверка: `make lint` и `make test` проходят, `git diff --stat main` показывает только `web/`, `docs/testing.md`, `README.md` и файлы этого change, в `web/package.json` нет новых зависимостей
- [x] 5.2 Проверка в браузере на собранном `metagit serve`: браузер с русским языком открывает интерфейс на русском; браузер с английским (или `chrome --lang=en`) - на английском; выбор "English" в настройках сразу переводит все страницы, переживает перезагрузку и действует на странице входа после выхода; английские тексты не переполняют шапку и кнопки на ширине 375 px в обеих темах; проверка: все пункты выполняются

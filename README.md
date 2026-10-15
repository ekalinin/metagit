# metagit

Self-hosted сервис, который для разработчика выглядит как обычный git remote, а сам хранит полную копию репозитория и рассылает ветки и теги на несколько git-хостингов (GitHub, Bitbucket, Codeberg и любые другие с доступом по git).

Зачем: если проект живет на одном хостинге, его можно потерять (заблокировали аккаунт, хостинг закрылся, ошибочный force-push затер историю). metagit держит копии в нескольких местах и не теряет прежние состояния веток.

```
 git push / clone
        |
        v
 +---------------- metagit ----------------+
 |  git по HTTP(S) и SSH                   |
 |  защищенные ветки и теги                |
 |  архив прежних состояний refs           |
 |  асинхронная доставка на хостинги       |
 +-----------------------------------------+
        |               |               |
        v               v               v
     GitHub         Bitbucket     любой git-хостинг
```

## Как это работает

- **Источник истины - metagit.** Синхронизация односторонняя: изменения приходят только через push в metagit, хостинги получают копию.
- **Доставка асинхронная.** Push успешен, как только metagit сохранил изменения у себя. Затем metagit несколько секунд ждет результатов доставки и печатает их в выводе push:

  ```
  remote: metagit: github: доставлено
  remote: metagit: codeberg: в очереди
  remote: metagit: bitbucket: ошибка: fatal: Authentication failed ... (повтор позже)
  ```

  Неудачная доставка повторяется автоматически с растущим интервалом (от 15 секунд до 30 минут).
- **Переносятся только ветки и теги.** Ветки и теги на хостинге, которые metagit туда не отправлял (например, ветки под pull request), он не трогает.
- **Ничего не теряется.** Перед force-push, удалением ветки или перемещением тега прежнее значение сохраняется в `refs/metagit/archive/push/...`. Для защищенных веток и тегов такие push запрещены.
- **Расхождения на хостинге.** Если ветку на хостинге изменили в обход metagit (например, merge в веб-интерфейсе), metagit сохраняет состояние хостинга в `refs/metagit/archive/targets/<id>/...`, перезаписывает ветку своим значением через `--force-with-lease` и пишет событие в журнал проекта.
- **Уведомления** - статус target'ов и журнал событий в веб-интерфейсе.

## Требования

- Linux или macOS.
- `git` 2.30 или новее и клиент OpenSSH (`ssh`) на сервере.
- Для сборки: Go 1.26 или новее, Node.js 24 или новее.

## Сборка

```sh
make build        # веб-интерфейс и бинарник bin/metagit
```

Получается один бинарник, веб-интерфейс встроен в него.

## Установка

1. Создайте системного пользователя и каталог данных:

   ```sh
   sudo useradd --system --home-dir /var/lib/metagit --shell /usr/sbin/nologin metagit
   sudo install -d -o metagit -g metagit -m 0700 /var/lib/metagit
   sudo install -m 0755 bin/metagit /usr/local/bin/metagit
   ```

2. Создайте мастер-ключ для шифрования учетных данных хостингов и сохраните его вне каталога данных (например, в хранилище секретов):

   ```sh
   openssl rand -base64 32
   ```

   Ключ передается через переменную `METAGIT_SECRET_KEY`. Если ее нет, metagit создаст файл `secret.key` в каталоге данных, но тогда ключ лежит на том же диске, что и база.

3. Создайте владельца. У metagit ровно один владелец, и создается он только командой на сервере:

   ```sh
   sudo -u metagit metagit owner create --name eugene --data-dir /var/lib/metagit
   ```

   Пароль спрашивается в терминале (или читается из первой строки stdin). Повторный вызов с тем же именем меняет пароль.

4. Запустите сервис, например через systemd (`/etc/systemd/system/metagit.service`):

   ```ini
   [Unit]
   Description=metagit
   After=network.target

   [Service]
   User=metagit
   Group=metagit
   EnvironmentFile=/etc/metagit/env
   ExecStart=/usr/local/bin/metagit serve \
       --data-dir /var/lib/metagit \
       --http-addr 127.0.0.1:8080 \
       --ssh-addr :2222 \
       --base-url https://git.example.com \
       --ssh-url ssh://git@git.example.com:2222
   Restart=on-failure

   [Install]
   WantedBy=multi-user.target
   ```

   В `/etc/metagit/env` (права 0600) положите `METAGIT_SECRET_KEY=...`.

5. TLS. Проще всего поставить перед metagit reverse proxy, например Caddy:

   ```
   git.example.com {
       reverse_proxy 127.0.0.1:8080
   }
   ```

   Для nginx отключите ограничение размера тела запроса (`client_max_body_size 0;`), иначе крупные push не пройдут. Можно обойтись без proxy: флаги `--tls-cert` и `--tls-key` включают TLS в самом metagit.

### Настройки `metagit serve`

| Флаг | Переменная | По умолчанию | Назначение |
|---|---|---|---|
| `--data-dir` | `METAGIT_DATA_DIR` | `./data` | репозитории, база, ключи |
| `--http-addr` | `METAGIT_HTTP_ADDR` | `:8080` | веб-интерфейс, API и git по HTTP |
| `--ssh-addr` | `METAGIT_SSH_ADDR` | `:2222` | git по SSH |
| `--base-url` | `METAGIT_BASE_URL` | из `--http-addr` | адрес для показа ссылок клонирования |
| `--ssh-url` | `METAGIT_SSH_URL` | из `--ssh-addr` | адрес SSH для показа ссылок клонирования |
| `--tls-cert`, `--tls-key` | `METAGIT_TLS_CERT`, `METAGIT_TLS_KEY` | не заданы | TLS без reverse proxy |
| `--push-status-timeout` | `METAGIT_PUSH_STATUS_TIMEOUT` | `5s` | сколько push ждет результатов доставки |
| `--max-parallel-deliveries` | `METAGIT_MAX_PARALLEL_DELIVERIES` | `4` | одновременные доставки |
| `--git-timeout` | `METAGIT_GIT_TIMEOUT` | `10m` | таймаут одной операции с хостингом |
| | `METAGIT_SECRET_KEY` | файл `secret.key` | мастер-ключ (base64 или hex, 32 байта) |

Флаг важнее переменной окружения, переменная важнее значения по умолчанию.

## Использование

1. Откройте веб-интерфейс и войдите.
2. В разделе "Настройки" создайте токен доступа для git (он показывается один раз) и, если нужен SSH, добавьте свой публичный ключ.
3. Создайте проект. На его странице видны адреса для клонирования:

   ```sh
   # HTTP(S): логин любой, пароль - токен metagit
   git remote add metagit https://git.example.com/foo.git
   # SSH
   git remote add metagit ssh://git@git.example.com:2222/foo.git

   git push metagit main
   ```

4. Добавьте target'ы - репозитории на хостингах. Репозиторий на хостинге нужно создать заранее. Target - это URL и учетные данные:
   - `https://...` с логином и токеном (например, fine-grained personal access token GitHub с правом записи в содержимое);
   - `ssh://...` или `git@host:path` с приватным SSH-ключом без пароля (например, deploy key с правом записи).

   Учетные данные в самом URL запрещены. Ключ хоста SSH запоминается при первом подключении (`known_hosts` в каталоге данных).
5. При необходимости защитите ветки и теги шаблонами, например `refs/heads/main` или `refs/tags/*` (`*` совпадает с любой последовательностью символов, включая `/`).

### Получение кода из архива

Список архивных refs есть на странице проекта вместе с командой для получения:

```sh
git fetch https://git.example.com/foo.git refs/metagit/archive/push/heads/feature/20261014T120000Z
git branch feature-restored FETCH_HEAD
```

### Восстановление после потери сервера

1. Заранее сделайте экспорт настроек ("Настройки" → "Экспорт"). Файл зашифрован паролем, который вы вводите при экспорте (формат [age](https://age-encryption.org), его можно открыть и командой `age -d`). В нем есть учетные данные хостингов, храните его соответственно.
2. На новом сервере установите metagit и создайте владельца.
3. Импортируйте файл ("Настройки" → "Импорт"). Появятся проекты с пустыми репозиториями, их защищенные refs и target'ы. До первого push metagit ничего не меняет на хостингах.
4. Заполните проекты из локальной копии или из любого живого зеркала:

   ```sh
   git clone --mirror git@github.com:me/foo.git && cd foo.git
   git push https://git.example.com/foo.git 'refs/heads/*:refs/heads/*' 'refs/tags/*:refs/tags/*'
   ```

   Ветки, которые на хостинге уже совпадают, считаются доставленными без архива и без событий расхождения.

Архивные refs хранятся только в metagit и при потере сервера теряются вместе с ним.

## Каталог данных

```
data/
  metagit.db              настройки и состояние (SQLite)
  secret.key              мастер-ключ, если не задан METAGIT_SECRET_KEY
  ssh_host_ed25519_key    ключ SSH-сервера
  known_hosts             ключи хостингов для доставки по SSH
  hooks/                  git-хуки, перезаписываются при каждом старте
  bin/askpass             выдает git учетные данные HTTPS-target'ов
  repos/<project>.git     bare-репозитории
  tmp/                    временные файлы
```

Для резервной копии достаточно остановить сервис и скопировать каталог данных (и мастер-ключ, если он хранится отдельно).

## Безопасность

- Запускайте metagit под отдельным системным пользователем: учетные данные хостингов передаются дочерним процессам git через окружение.
- Учетные данные target'ов хранятся зашифрованными (AES-256-GCM) и не показываются в веб-интерфейсе, журналах и выводе push.
- Пароль владельца хранится как argon2id-хеш, попытки входа ограничены по частоте.
- SSH-сервер принимает только ключи владельца и выполняет только `git-upload-pack` и `git-receive-pack`.

## Что не входит в эту версию

Git LFS, wiki, релизы, issues и pull request; несколько пользователей; двусторонняя синхронизация; создание репозиториев на хостингах через их API; отправка архивных refs на хостинги; уведомления вне веб-интерфейса.

## Разработка

```sh
make test         # Go и веб-тесты
make lint         # gofmt, go vet, проверка типов TypeScript
```

### Веб-интерфейс в режиме разработки

`npm run dev` запускает только интерфейс (Vite, по умолчанию http://localhost:5173), а запросы к `/api` передает запущенному `metagit serve`. Поэтому сначала нужен сам сервис:

```sh
make go
./bin/metagit owner create --name dev --data-dir ./data
./bin/metagit serve --data-dir ./data          # API на http://localhost:8080

# в другом терминале
cd web && npm run dev
```

Если `metagit serve` слушает другой адрес, передайте его через `METAGIT_API_URL`:

```sh
./bin/metagit serve --data-dir ./data --http-addr :9000
cd web && METAGIT_API_URL=http://localhost:9000 npm run dev
```

Интеграционные тесты запускают настоящий git и собирают бинарник metagit. `METAGIT_TEST_LOG=1` выводит журнал сервиса в тестах.

Уровни тестов, шаги сквозного теста и то, какие тесты проверяют каждый сценарий спецификаций, описаны в [docs/testing.md](docs/testing.md).

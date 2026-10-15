# Установка metagit

## Требования

- Linux или macOS.
- `git` 2.30 или новее и клиент OpenSSH (`ssh`) на сервере.
- Для сборки: Go 1.26 или новее, Node.js 24 или новее.

## Сборка

```sh
make build        # веб-интерфейс и бинарник bin/metagit
```

Получается один бинарник, веб-интерфейс встроен в него.

## Установка на сервер

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

## Настройки `metagit serve`

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

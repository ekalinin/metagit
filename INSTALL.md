# Установка metagit

## Требования

- Linux или macOS.
- `git` 2.30 или новее и клиент OpenSSH (`ssh`) на сервере.
- Только для сборки из исходников: Go 1.26 или новее, Node.js 24 или новее.

## Установка готового бинарника

Готовые бинарники для Linux и macOS (amd64 и arm64) лежат на [странице релизов](https://github.com/ekalinin/metagit/releases). Веб-интерфейс встроен в бинарник.

```sh
os=linux arch=amd64        # или os=darwin, arch=arm64
base=https://github.com/ekalinin/metagit/releases/latest/download
curl -fLO "$base/metagit_${os}_${arch}.tar.gz"
curl -fLO "$base/checksums.txt"
sha256sum -c checksums.txt --ignore-missing     # macOS: shasum -a 256 -c checksums.txt --ignore-missing
tar xzf "metagit_${os}_${arch}.tar.gz" metagit
./metagit version
```

## Сборка из исходников

```sh
make build        # веб-интерфейс и бинарник bin/metagit
```

Получается один бинарник, веб-интерфейс встроен в него.

## Установка на сервер

1. Создайте системного пользователя и каталог данных:

   ```sh
   sudo useradd --system --home-dir /var/lib/metagit --shell /usr/sbin/nologin metagit
   sudo install -d -o metagit -g metagit -m 0700 /var/lib/metagit
   sudo install -m 0755 metagit /usr/local/bin/metagit     # после сборки из исходников: bin/metagit
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

## Обновление

1. Скачайте и проверьте архив новой версии, как в разделе "Установка готового бинарника", или соберите ее из исходников.
2. Остановите сервис и сделайте копию каталога данных (и мастер-ключа, если он хранится отдельно):

   ```sh
   sudo systemctl stop metagit
   sudo cp -a /var/lib/metagit /var/lib/metagit.bak
   ```

3. Замените бинарник и запустите сервис:

   ```sh
   sudo install -m 0755 metagit /usr/local/bin/metagit
   sudo systemctl start metagit
   ```

4. Проверьте версию в строке `metagit started` лога сервиса (`journalctl -u metagit`).

Сервис останавливают до замены бинарника: git-хуки репозиториев вызывают бинарник по тому же пути, и до перезапуска push обрабатывали бы новый бинарник и старый сервис вместе. При старте новая версия сама перезаписывает хуки и обновляет схему базы.

## Откат

1. Остановите сервис и верните копию каталога данных, сделанную перед обновлением:

   ```sh
   sudo systemctl stop metagit
   sudo mv /var/lib/metagit /var/lib/metagit.failed
   sudo mv /var/lib/metagit.bak /var/lib/metagit
   ```

2. Поставьте прежний бинарник и запустите сервис.

Push, принятые после обновления, останутся только в `/var/lib/metagit.failed`, их нужно отправить заново из локальных копий. Если вернуть только прежний бинарник, а новая версия изменила схему базы, прежний бинарник не запустится и напишет `database schema version N is newer than this metagit supports (M)`.

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

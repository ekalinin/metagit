# Installing metagit

English | [Русский](INSTALL.ru.md)

## Requirements

- Linux or macOS.
- `git` 2.30 or newer and the OpenSSH client (`ssh`) on the server.
- Only for building from source: Go 1.26 or newer, Node.js 24 or newer.

## Installing a ready binary

Ready binaries for Linux and macOS (amd64 and arm64) are on the [releases page](https://github.com/ekalinin/metagit/releases). The web console is built into the binary.

```sh
os=linux arch=amd64        # or os=darwin, arch=arm64
base=https://github.com/ekalinin/metagit/releases/latest/download
curl -fLO "$base/metagit_${os}_${arch}.tar.gz"
curl -fLO "$base/checksums.txt"
sha256sum -c checksums.txt --ignore-missing     # macOS: shasum -a 256 -c checksums.txt --ignore-missing
tar xzf "metagit_${os}_${arch}.tar.gz" metagit
./metagit version
```

## Building from source

```sh
make build        # web console and the bin/metagit binary
```

The result is a single binary with the web console built in.

## Installing on a server

1. Create a system user and the data directory:

   ```sh
   sudo useradd --system --home-dir /var/lib/metagit --shell /usr/sbin/nologin metagit
   sudo install -d -o metagit -g metagit -m 0700 /var/lib/metagit
   sudo install -m 0755 metagit /usr/local/bin/metagit     # after building from source: bin/metagit
   ```

2. Create a master key for encrypting hosting credentials and keep it outside the data directory (for example, in a secret store):

   ```sh
   openssl rand -base64 32
   ```

   The key is passed in the `METAGIT_SECRET_KEY` variable. Without it, metagit creates the file `secret.key` in the data directory, but then the key is on the same disk as the database.

3. Create the owner. metagit has exactly one owner, and it is created only by a command on the server:

   ```sh
   sudo -u metagit metagit owner create --name eugene --data-dir /var/lib/metagit
   ```

   The password is asked in the terminal (or read from the first line of stdin). Running the command again with the same name changes the password.

4. Start the service, for example with systemd (`/etc/systemd/system/metagit.service`):

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

   Put `METAGIT_SECRET_KEY=...` in `/etc/metagit/env` (mode 0600).

5. TLS. The simplest way is a reverse proxy in front of metagit, for example Caddy:

   ```
   git.example.com {
       reverse_proxy 127.0.0.1:8080
   }
   ```

   Behind a proxy, all requests reach metagit from the proxy address, while failed sign-in attempts are limited per client address. Without this setting, anyone who enters a wrong password 5 times blocks sign-in for the owner too, for 15 minutes. So set the proxy address in `--trusted-proxies` (for the Caddy example, `--trusted-proxies 127.0.0.1`): for requests from this address metagit takes the client address from the `X-Forwarded-For` header. Caddy sends this header by default.

   For nginx, turn off the request body size limit (`client_max_body_size 0;`), otherwise large pushes fail, and pass the client address: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`. You can do without a proxy: the `--tls-cert` and `--tls-key` flags turn on TLS in metagit itself.

## Updating

1. Download and check the archive of the new version as in "Installing a ready binary", or build it from source.
2. Stop the service and make a copy of the data directory (and of the master key, if it is stored separately):

   ```sh
   sudo systemctl stop metagit
   sudo cp -a /var/lib/metagit /var/lib/metagit.bak
   ```

3. Replace the binary and start the service:

   ```sh
   sudo install -m 0755 metagit /usr/local/bin/metagit
   sudo systemctl start metagit
   ```

4. Check the version in the `metagit started` line of the service log (`journalctl -u metagit`).

The service is stopped before the binary is replaced: the git hooks of the repositories call the binary at the same path, so until the restart pushes would be handled by the new binary and the old service together. On start, the new version rewrites the hooks and updates the database schema itself.

## Rolling back

1. Stop the service and put back the copy of the data directory made before the update:

   ```sh
   sudo systemctl stop metagit
   sudo mv /var/lib/metagit /var/lib/metagit.failed
   sudo mv /var/lib/metagit.bak /var/lib/metagit
   ```

2. Install the previous binary and start the service.

Pushes accepted after the update remain only in `/var/lib/metagit.failed`, they have to be pushed again from local copies. If you put back only the previous binary and the new version changed the database schema, the previous binary does not start and prints `database schema version N is newer than this metagit supports (M)`.

## `metagit serve` settings

| Flag | Variable | Default | Purpose |
|---|---|---|---|
| `--data-dir` | `METAGIT_DATA_DIR` | `./data` | repositories, database, keys |
| `--http-addr` | `METAGIT_HTTP_ADDR` | `:8080` | web console, API and git over HTTP |
| `--ssh-addr` | `METAGIT_SSH_ADDR` | `:2222` | git over SSH |
| `--base-url` | `METAGIT_BASE_URL` | from `--http-addr` | address used to show clone links |
| `--ssh-url` | `METAGIT_SSH_URL` | from `--ssh-addr` | SSH address used to show clone links |
| `--tls-cert`, `--tls-key` | `METAGIT_TLS_CERT`, `METAGIT_TLS_KEY` | not set | TLS without a reverse proxy |
| `--trusted-proxies` | `METAGIT_TRUSTED_PROXIES` | not set | reverse proxy addresses (IPs or CIDRs, comma-separated); for requests from them the client address for the sign-in attempt limit is taken from `X-Forwarded-For` |
| `--push-status-timeout` | `METAGIT_PUSH_STATUS_TIMEOUT` | `5s` | how long a push waits for delivery results |
| `--max-parallel-deliveries` | `METAGIT_MAX_PARALLEL_DELIVERIES` | `4` | simultaneous deliveries |
| `--git-timeout` | `METAGIT_GIT_TIMEOUT` | `10m` | timeout of one operation with a hosting |
| | `METAGIT_SECRET_KEY` | the `secret.key` file | master key (base64 or hex, 32 bytes) |

A flag takes precedence over the environment variable, and the variable over the default.

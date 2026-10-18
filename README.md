# metagit

English | [Русский](README.ru.md)

[![Go version](https://img.shields.io/github/go-mod/go-version/ekalinin/metagit)](go.mod)
[![Release](https://img.shields.io/github/v/release/ekalinin/metagit)](https://github.com/ekalinin/metagit/releases)

A self-hosted service that looks like an ordinary git remote to a developer, while it keeps a full copy of the repository and sends branches and tags to several git hostings (GitHub, Bitbucket, Codeberg and any other with git access).

Why: a project that lives on one hosting can be lost (the account gets blocked, the hosting shuts down, a mistaken force-push wipes out the history). metagit keeps copies in several places and does not lose previous states of branches.

```
 git push / clone
        |
        v
 +---------------- metagit ----------------+
 |  git over HTTP(S) and SSH               |
 |  protected branches and tags            |
 |  archive of previous ref states         |
 |  asynchronous delivery to hostings      |
 +-----------------------------------------+
        |               |               |
        v               v               v
     GitHub         Bitbucket     any git hosting
```

## How it works

- **metagit is the source of truth.** Sync is one-way: changes arrive only through a push to metagit, the hostings get a copy.
- **Delivery is asynchronous.** A push succeeds as soon as metagit has saved the changes. Then metagit waits a few seconds for the delivery results and prints them in the push output:

  ```
  remote: metagit: github: delivered
  remote: metagit: codeberg: queued
  remote: metagit: bitbucket: error: fatal: Authentication failed ... (retry later)
  ```

  A failed delivery is retried automatically with a growing interval (from 15 seconds to 30 minutes).
- **Only branches and tags are sent.** Branches and tags on a hosting that metagit did not send there (for example, branches for pull requests) are left alone.
- **Nothing is lost.** Before a force-push, a branch deletion or a tag move, the previous value is saved in `refs/metagit/archive/push/...`. For protected branches and tags such pushes are rejected.
- **Divergence on a hosting.** If a branch on a hosting was changed bypassing metagit (for example, a merge in the hosting's web interface), metagit saves the hosting's state in `refs/metagit/archive/targets/<id>/...`, overwrites the branch with its own value using `--force-with-lease` and records an event in the project log.
- **Notifications** - target status and the event log in the web console.

## Installation

Requirements, building, installing on a server and `metagit serve` settings are described in [INSTALL.md](INSTALL.md) (in Russian).

## Usage

The web console is in Russian for now, its labels are given in quotes.

1. Open the web console and sign in.
2. In Settings, create a git access token (it is shown once) and, if you need SSH, add your public key.
3. Create a project. Its page shows the clone addresses:

   ```sh
   # HTTP(S): any login, the password is a metagit token
   git remote add metagit https://git.example.com/foo.git
   # SSH
   git remote add metagit ssh://git@git.example.com:2222/foo.git

   git push metagit main
   ```

4. Add targets - repositories on hostings. Create the repository on the hosting beforehand. A target is a URL and credentials:
   - `https://...` with a login and a token (for example, a GitHub fine-grained personal access token with write access to contents);
   - `ssh://...` or `git@host:path` with a private SSH key without a passphrase (for example, a deploy key with write access).

   Credentials in the URL itself are not allowed. The SSH host key is remembered on the first connection (`known_hosts` in the data directory).
5. If needed, protect branches and tags with patterns, for example `refs/heads/main` or `refs/tags/*` (`*` matches any sequence of characters, including `/`).

How the web console pages look in the light and dark themes is shown in [docs/web-interface.md](docs/web-interface.md).

### Getting code from the archive

The list of archived refs is on the project page, together with the command to fetch each of them:

```sh
git fetch https://git.example.com/foo.git refs/metagit/archive/push/heads/feature/20261014T120000Z
git branch feature-restored FETCH_HEAD
```

### Recovering after losing the server

1. Export the settings in advance (Settings → Export). The file is encrypted with the password you enter on export (the [age](https://age-encryption.org) format, it can also be opened with `age -d`). It contains the hosting credentials, keep it accordingly.
2. On the new server, install metagit and create the owner.
3. Import the file (Settings → Import). Projects appear with empty repositories, their protected refs and targets. Until the first push, metagit changes nothing on the hostings.
4. Fill the projects from a local copy or from any live mirror:

   ```sh
   git clone --mirror git@github.com:me/foo.git && cd foo.git
   git push https://git.example.com/foo.git 'refs/heads/*:refs/heads/*' 'refs/tags/*:refs/tags/*'
   ```

   Branches that already match on a hosting are counted as delivered, without archives and without divergence events.

Archived refs are stored only in metagit and are lost together with the server.

## Data directory

```
data/
  metagit.db              settings and state (SQLite)
  secret.key              master key, if METAGIT_SECRET_KEY is not set
  ssh_host_ed25519_key    SSH server key
  known_hosts             hosting keys for delivery over SSH
  hooks/                  git hooks, rewritten on every start
  bin/askpass             gives git the credentials of HTTPS targets
  repos/<project>.git     bare repositories
  tmp/                    temporary files
```

For a backup, it is enough to stop the service and copy the data directory (and the master key, if it is stored separately).

## Security

- Run metagit as a separate system user: hosting credentials are passed to child git processes through the environment.
- Target credentials are stored encrypted (AES-256-GCM) and are not shown in the web console, logs or push output.
- The owner password is stored as an argon2id hash, sign-in attempts are rate limited.
- The SSH server accepts only the owner's keys and runs only `git-upload-pack` and `git-receive-pack`.

## Not in this version

Git LFS, wiki, releases, issues and pull requests; multiple users; two-way sync; creating repositories on hostings through their API; sending archived refs to hostings; notifications outside the web console.

## Development

```sh
make test         # Go and web tests
make lint         # gofmt, go vet, TypeScript type check
```

### Web console in development mode

`npm run dev` starts only the interface (Vite, http://localhost:5173 by default) and passes `/api` requests to a running `metagit serve`. So the service itself is needed first:

```sh
make go
./bin/metagit owner create --name dev --data-dir ./data
./bin/metagit serve --data-dir ./data          # API on http://localhost:8080

# in another terminal
cd web && npm run dev
```

If `metagit serve` listens on another address, pass it in `METAGIT_API_URL`:

```sh
./bin/metagit serve --data-dir ./data --http-addr :9000
cd web && METAGIT_API_URL=http://localhost:9000 npm run dev
```

Integration tests run real git and build the metagit binary. `METAGIT_TEST_LOG=1` prints the service log in tests.

Test levels, the steps of the end-to-end test and which tests check each spec scenario are described in [docs/testing.md](docs/testing.md) (in Russian).

How to make a release is described in [docs/releasing.md](docs/releasing.md) (in Russian).

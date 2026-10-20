# Working on metagit

metagit is a self-hosted service that looks like an ordinary git remote: it keeps a full copy of each repository and delivers branches and tags to several git hostings. The service is written in Go; the React web console is embedded into the binary.

## Layout

- `cmd/metagit` - entry point; subcommands are in `internal/cli`.
- `internal/` - every package has a `// Package` doc comment. The main ones:
  - `app` - assembles storage, git over HTTP and SSH, hooks, delivery and the web interface;
  - `core` - operations on projects, targets and protected refs, push decisions for hooks;
  - `githttp`, `gitssh` - git smart HTTP and the SSH server;
  - `hooks` - git hooks talk to the running service over a unix socket;
  - `replicate` - delivery to targets;
  - `archive` - protected refs and previous ref states under `refs/metagit/archive/`;
  - `store` - SQLite storage and migrations;
  - `api` - JSON API of the web console; `webui` - serves the embedded console;
  - `apptest`, `testutil`, `e2e` - test helpers and the end-to-end test.
- `web/` - web console (React, Vite, Vitest). The build goes to `internal/webui/dist/app/` (git-ignored), which the binary embeds. `web/go.mod` keeps `node_modules` out of the Go module.
- `site/` - static English landing for GitHub Pages; preview with `python3 -m http.server -d site`.
- `openspec/` - specs and changes, see below.
- `docs/` - `testing.md`, `releasing.md`, `web-interface.md` (screenshots), `explore/` (early idea notes).

## Commands

Requirements: Go 1.26+, Node.js 24+, git 2.30+ and the OpenSSH client. The service checks git and ssh on start, so tests need them too.

```sh
make build                                    # web console, then bin/metagit
make test                                     # Go and web tests
make lint                                     # gofmt on cmd and internal, go vet, TypeScript type check
go test -race ./...
METAGIT_TEST_LOG=1 go test ./internal/app/    # with the service log
```

`make go` builds only the binary; without `make web` before it, the binary serves a "web interface is not built" page. Web dev mode against a running `metagit serve`: README.md, "Development". CI runs `make lint`, `make build`, `make test`.

## Testing

`docs/testing.md` (Russian) describes the test levels and the end-to-end steps, and maps spec scenarios to the tests that check them. Changes that add scenarios also add their rows there (see `tasks.md` of archived changes).

- Integration tests (`internal/app`, `internal/api`, `internal/transfer`) and `internal/e2e` run real git and build the metagit binary (`testutil.Binary`).
- Web tests run with a Russian browser language (`web/src/test-setup.ts`), so they see the `ru` texts.

## OpenSpec

- Main specs: `openspec/specs/<capability>/spec.md`. Changes: `openspec/changes/<change>/` with `proposal.md`, `design.md`, `tasks.md` and delta specs.
- Every task in `tasks.md` ends with `проверка:` - how to check that it is done.
- In history, `feat` and `refactor` PRs carry an OpenSpec change; `fix`, `chore` and `docs` PRs go without one.
- After the change PR is merged, the change is archived in its own PR: branch `docs/archive-<change>`, commit `docs(openspec): archive <change>`. Archiving moves the change to `openspec/changes/archive/YYYY-MM-DD-<change>/` and merges its delta specs into `openspec/specs/`.

## Git

- Commits: Conventional Commits in English, `type(scope): subject`. Types in use: `feat`, `fix`, `refactor`, `docs`, `chore`. Most commits have a body that explains the reason for the change.
- Branches: prefix `feat/`, `fix/`, `refactor/`, `docs/` or `chore/`; one change per branch and PR.
- PRs are merged with a merge commit, so every branch commit reaches the release changelog with its own subject. The changelog groups `feat` (Features), `fix` (Fixes) and the rest (Other), and drops `docs(openspec)` and merge commits (`.goreleaser.yaml`).

## Languages

- `README.md` and `INSTALL.md` are English, `README.ru.md` and `INSTALL.ru.md` are their Russian versions: change both files of a pair together.
- `docs/*.md`, OpenSpec specs and change files are Russian.
- `site/index.html` is English, and every statement on it must be in `README.md` or `INSTALL.md` (`openspec/specs/project-landing/spec.md`). After changing `README.md` or `INSTALL.md`, check the landing texts.
- Server messages (API errors, events, push output, hooks), logs and code comments are English.
- Web console texts: `web/src/locales/en.tsx` is the reference catalog (type `Messages`), `ru.tsx` has exactly the same keys.

## Releases

A `v*` tag runs `.github/workflows/release.yml` with GoReleaser. Steps and recovery: `docs/releasing.md` (Russian).

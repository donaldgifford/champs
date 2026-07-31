# champs

A Go CLI that reconciles a per-org `security_champions` GitHub team as
`roster ∩ org_members` — under the hard invariant that it never expands
organization access (it will never send an org invitation). See
[DESIGN-0001](docs/design/0001-champs-security-champions-team-management-cli.md)
for the full design and
[IMPL-0001](docs/impl/0001-champs-v010-implementation.md) for delivery status.

## Usage

```sh
champs plan                    # show the diff; writes nothing
champs apply                   # reconcile every configured org
champs apply --dry-run         # identical to plan
champs apply --prune           # also remove members no longer on the roster
champs apply --orgs org1,org2  # limit to configured orgs (unknown names fail)
champs version                 # ldflags-injected version/commit/date
```

Flags: `--config champs.hcl`, `--roster roster.csv`, `--parallelism 5`,
`--no-color` (also honors `NO_COLOR` and disables color when piped).

Output is one stdout stream, Terraform-style: green `+` adds, red `-` removals
per org, skip records as timestamp-free `slog` JSON lines (`not_org_member`,
`no_installation`, `unknown_user`), then a summary. Exit `0` means completed
(skips included); exit `1` means any error — per-org errors never abort the
other orgs.

Configuration is HCL (`team`, `github`, repeated `org` blocks); the App private
key comes from `CHAMPS_GITHUB_PRIVATE_KEY` (PEM contents, preferred) or
`private_key_path`. The roster is a single-column CSV of GitHub logins with an
optional `login`/`username` header.

## Quickstart

```sh
mise install                  # toolchain
just                          # task menu
just build                    # binary at bin/champs
just test                     # race + coverage
just run -- --help            # run via `go run`
```

## Release

```sh
just release v0.1.0           # tags + pushes; CI runs goreleaser
```

Multi-arch archives land on the Forgejo (or GitHub) release page. Version
metadata (`version`, `commit`, `date`) is embedded via `-ldflags` and surfaced
by `champs version` / `champs --version`.

## Container

```sh
docker build -t champs:dev \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) .
```

Image is distroless + nonroot; entrypoint is `champs`.

## Layout

```
cmd/champs/    main package
internal/               library code (private to this module)
Dockerfile              multi-stage distroless build
.goreleaser.yml         release config
mise.toml               pinned toolchain
justfile                task runner
```

## Conventions

See `CLAUDE.md` for the full operating notes (Go-specific + homelab universals).

## License

Apache-2.0

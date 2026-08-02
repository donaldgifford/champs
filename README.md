# champs

[![CI](https://github.com/donaldgifford/champs/actions/workflows/ci.yml/badge.svg)](https://github.com/donaldgifford/champs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

A Go CLI that reconciles a per-org `security_champions` GitHub team as
`roster ∩ org_members` — under the hard invariant that it never expands
organization access (it will never send an org invitation).

It authenticates as a GitHub App, fans out across every configured org, prints a
Terraform-style plan/apply diff, and exits with codes CI can react to. Champions
who aren't org members yet are reported as skips — the standing "needs an invite
through the normal process" list — never invited.

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

Configuration is HCL (`team`, `github`, repeated `org` blocks) — see
[example.config.hcl](example.config.hcl) for a commented starting point. The App
private key comes from `CHAMPS_GITHUB_PRIVATE_KEY` (PEM contents, preferred) or
`private_key_path`. The roster is a single-column CSV of GitHub logins with an
optional `login`/`username` header.

**[USAGE.md](USAGE.md) is the full operator reference** — GitHub App
prerequisites, the complete config/roster schema, the output contract, the
safety model, and CI deployment patterns.

## Quickstart (developers)

```sh
mise install                  # pinned toolchain
just                          # task menu
just build                    # binary at build/bin/champs
just test                     # race + coverage
just check                    # lint + test, the pre-commit gate
```

**[DEVELOPMENT.md](DEVELOPMENT.md) has the rest** — repo layout, how the
fake-GitHub test suite works, step-by-step instructions for setting up a free
test org with a real GitHub App, and what CI enforces on PRs.

## Documentation

| Document                                                                         | What's in it                                         |
| -------------------------------------------------------------------------------- | ---------------------------------------------------- |
| [USAGE.md](USAGE.md)                                                             | Operator reference: config, roster, output, safety.  |
| [example.config.hcl](example.config.hcl)                                         | Commented example configuration.                     |
| [DEVELOPMENT.md](DEVELOPMENT.md)                                                 | Dev setup, testing (incl. real-org walkthrough), CI. |
| [CONTRIBUTING.md](CONTRIBUTING.md)                                               | How to report issues and submit PRs.                 |
| [MAINTAINERS.md](MAINTAINERS.md)                                                 | Who maintains champs; security contact.              |
| [DESIGN-0001](docs/design/0001-champs-security-champions-team-management-cli.md) | The design: invariant, skip taxonomy, decisions.     |
| [IMPL-0001](docs/impl/0001-champs-v010-implementation.md)                        | Phase-by-phase delivery tracking for v0.1.0.         |

## Release

```sh
just release v0.1.0           # tags + pushes; CI runs goreleaser
```

Multi-arch archives (linux/darwin × amd64/arm64) land on the GitHub release page
with checksums and SBOMs. Version metadata (`version`, `commit`, `date`) is
embedded via `-ldflags` and surfaced by `champs version` / `champs --version`.

## Layout

```text
cmd/champs/             main package
internal/               library code (private to this module)
docs/                   design + implementation docs (docz-managed)
.github/workflows/      CI, security scanning, release
.goreleaser.yml         release config
mise.toml               pinned toolchain
justfile                task runner
```

## Conventions

See `CLAUDE.md` for the full operating notes (Go-specific + homelab universals).

## License

Apache-2.0

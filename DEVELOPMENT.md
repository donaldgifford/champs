# Developing champs

Everything you need to hack on champs locally: toolchain setup, everyday tasks,
how the tests work, how to test against a real GitHub org, and what CI expects
from a PR.

For the operator-facing reference (flags, config, output contract) see
[USAGE.md](USAGE.md). For contribution etiquette see
[CONTRIBUTING.md](CONTRIBUTING.md).

## Prerequisites

- [mise](https://mise.jdx.dev/) — pins the entire toolchain (Go, golangci-lint,
  just, git-cliff, prettier, and friends) in [mise.toml](mise.toml). One tool to
  install; it fetches the rest.
- `git`, and [gh](https://cli.github.com/) if you want to drive PRs from the
  terminal.

Without mise you need at minimum Go 1.26.5 (match the `go` directive in
`go.mod`), `just`, and golangci-lint 2.x — but mise is the supported path and
what CI uses.

## First-time setup

```sh
git clone https://github.com/donaldgifford/champs.git
cd champs
mise install          # installs the pinned toolchain
just                  # lists every recipe
just build            # binary at build/bin/champs
just test             # unit tests, race detector on
```

## Everyday tasks

All automation is in the [justfile](justfile):

| Recipe                               | What it does                                                      |
| ------------------------------------ | ----------------------------------------------------------------- |
| `just build`                         | Build `build/bin/champs` with version ldflags from git.           |
| `just test`                          | `go test -race` for everything.                                   |
| `just test-pkg ./internal/reconcile` | One package.                                                      |
| `just test-coverage`                 | Race + coverage profile to `coverage.out`.                        |
| `just test-report`                   | Coverage profile + open the HTML report.                          |
| `just coverage-gate`                 | Fail if any tested package is below the 80% floor (what CI runs). |
| `just lint`                          | golangci-lint (config in `.golangci.yml`).                        |
| `just lint-fix`                      | golangci-lint with `--fix`.                                       |
| `just lint-actions`                  | actionlint over the workflows.                                    |
| `just fmt`                           | gofmt + goimports (local prefix `github.com/donaldgifford`).      |
| `just license-check`                 | go-licenses against the allow list.                               |
| `just release-check`                 | Validate `.goreleaser.yml`.                                       |
| `just release-local`                 | Full goreleaser snapshot build, no publish.                       |
| `just check`                         | lint + test — the pre-commit gate.                                |

Run `just check` before pushing; it is the local equivalent of the two CI jobs
that gate merges hardest.

## Repo layout

```text
cmd/champs/          main: slog default handler, os.Exit(cli.Execute(...))
internal/cli/        cobra commands (apply, plan, version), flag → Options wiring
internal/config/     HCL config: parse (gohcl), validation, private-key resolution
internal/roster/     roster CSV: normalize, validate login shape
internal/gh/         GitHub App auth + REST/GraphQL operations, guard backstop
internal/ghtest/     httptest fake GitHub server used by all client/engine tests
internal/reconcile/  the engine: set math, skip classification, org fan-out
internal/render/     diff + skip + summary rendering, color handling
internal/stringset/  small set type shared by roster/reconcile
docs/design/         DESIGN-0001 — the what and why
docs/impl/           IMPL-0001 — phase-by-phase delivery tracking
```

Two structural rules worth internalizing:

- **`internal/` is a hard wall** — nothing here is importable by other modules,
  so refactor freely.
- **The dependency direction is one-way**:
  `cli → config/roster/gh/ reconcile → stringset`. `reconcile` talks to GitHub
  only through its `ClientSource` interface; it never imports `net/http`.

Design context lives in
[DESIGN-0001](docs/design/0001-champs-security-champions-team-management-cli.md);
delivery status in [IMPL-0001](docs/impl/0001-champs-v010-implementation.md).
Doc scaffolding is managed with [docz](https://github.com/donaldgifford/docz)
(`docz create design "..."`, `docz update`).

## Testing

### Unit tests (no network)

Every test runs against `internal/ghtest`, an `httptest` fake of the GitHub API:
token minting, REST pagination via Link headers, the GraphQL `membersWithRole`
query, GitHub-realistic team-membership `PUT` semantics (org member → `active`,
non-member → `pending` + invitation), and fault injection (`FailMembershipPut`,
one-shot secondary rate limits). **Unit tests make zero real network calls.** If
you add a client operation, extend the fake — don't mock at the interface level;
the tests' value is that they exercise the real HTTP path.

Things CI will hold you to:

- `just test` runs with `-race`; keep it green.
- `just coverage-gate` enforces an 80% statement-coverage floor per tested
  package.
- The two invariant tests in `internal/reconcile/reconcile_test.go` — the guard
  regression (no `PUT` for a non-member, ever) and the idempotency test (steady
  state issues zero writes) — are the safety contract. Treat a change that
  breaks them as a design problem, not a test problem.

Local runs of the CLI can point at any fake or GHES-style endpoint via
`CHAMPS_GITHUB_BASE_URL`; the CLI tests use this to drive the production binary
path end to end against `ghtest`.

### Testing against a real GitHub org

Unit tests prove the logic; a real org proves the App wiring. A free GitHub org
is enough — teams and org membership are all on the Free plan. This takes about
ten minutes.

**1. Create the test org.**

1. Go to <https://github.com/account/organizations/new> (or your avatar →
   **Settings** → **Organizations** → **New organization**).
2. Choose the **Free** plan.
3. Name it something obviously non-production, e.g. `champs-test-<you>` (org
   names are globally unique).
4. Skip inviting members for now — your own account is the org owner and is
   already a member, which is enough to exercise the core path.

**2. Create a GitHub App owned by that org.**

1. Go to the org's **Settings** → **Developer settings** → **GitHub Apps** →
   **New GitHub App** (URL shape:
   `https://github.com/organizations/<org>/settings/apps/new`).
2. **GitHub App name:** globally unique, e.g. `champs-test-<you>`.
3. **Homepage URL:** anything, e.g. this repo's URL.
4. **Webhook:** uncheck **Active** (champs never receives webhooks; this also
   removes the webhook-URL requirement).
5. **Permissions** → **Organization permissions** → **Members** → **Read and
   write**. Leave every other permission at "No access" — this is the App's
   entire manifest.
6. **Where can this GitHub App be installed?** → **Only on this account**.
7. Create the App.

**3. Collect credentials.**

1. On the App's **General** page, note the **App ID** (top of the About section)
   — it goes in `champs.hcl` as `app_id`.
2. Scroll to **Private keys** → **Generate a private key**. A `.pem` file
   downloads; that is the only copy, store it like a password.

**4. Install the App on the org.**

1. On the App's page, left sidebar → **Install App**.
2. Install on your test org. Repository selection doesn't matter (champs uses
   only the org-level Members permission) — **All repositories** is fine.

**5. Write a config and roster.**

```hcl
# champs.hcl
team {
  slug        = "security_champions"
  description = "champs test team"
  privacy     = "closed"
}

github {
  app_id = <your app id>
}

org "champs-test-<you>" {}
```

```csv
login
<your github login>
some-org-member-if-you-have-one
definitely-not-a-member
```

**6. Run it.**

```sh
export CHAMPS_GITHUB_PRIVATE_KEY="$(cat ~/Downloads/champs-test-<you>.*.private-key.pem)"
just build
./build/bin/champs plan  --config champs.hcl --roster roster.csv
./build/bin/champs apply --config champs.hcl --roster roster.csv
```

`plan` should show your login as `+ <you>`; `apply` should create the
`security_champions` team (check the org's Teams tab) with you in it. A second
`apply` should print only the summary — zero writes.

**7. Exercise the failure paths** (each is a one-line change):

- A roster login that exists on GitHub but isn't in the org → `not_org_member`
  skip. **Verify no org invitation appears** under the org's **People → Pending
  invitations** — this is the invariant.
- A roster login that exists nowhere (e.g. `zz-champs-no-such-user-zz`) →
  `unknown_user` skip.
- Add a second `org "..."` block for an org the App is not installed on →
  `no_installation` warning, exit `0`, other orgs unaffected.
- With a member in the team but not on the roster: plain `apply` leaves them;
  `apply --prune` removes them from the team (and **only** the team — they stay
  an org member).
- An empty roster + `--prune` → refused before any API call, exit `1`.

**8. (Optional) A second member.**

To exercise multi-member adds beyond your own account, invite a second account
you control to the org (GitHub's ToS permits one machine account per user for
automation) or a colleague's test account. Roster entries that aren't org
members already exercise the skip path without any extra accounts.

**Reusing the org in CI.** The same org can back a live smoke test in this
repo's CI:

1. Add the App's PEM as a repo **Actions secret**, e.g.
   `CHAMPS_TEST_GITHUB_PRIVATE_KEY`, and the App ID as a variable.
2. Commit the test-org config and roster somewhere obvious, e.g.
   `e2e/champs.hcl` + `e2e/roster.csv`.
3. A workflow job then runs the real binary against the real org:

   ```yaml
   e2e-plan:
     runs-on: ubuntu-latest
     steps:
       - uses: actions/checkout@v6
       - uses: actions/setup-go@v6
         with:
           go-version-file: go.mod
       - run:
           go run ./cmd/champs plan --config e2e/champs.hcl --roster
           e2e/roster.csv
         env:
           CHAMPS_GITHUB_PRIVATE_KEY:
             ${{ secrets.CHAMPS_TEST_GITHUB_PRIVATE_KEY }}
   ```

   Start with `plan` only (read-only, safe on every PR, and secrets are
   unavailable to fork PRs anyway); an `apply` smoke test belongs on pushes to
   `main` or a schedule, where it doubles as an idempotency check — steady state
   must report zero changes.

## CI: what a PR has to pass

Every push/PR runs the full battery ([.github/workflows/](.github/workflows/)):

| Check                                   | Workflow            | Satisfy it by                                                |
| --------------------------------------- | ------------------- | ------------------------------------------------------------ |
| Lint                                    | `ci.yml`            | `just lint` clean.                                           |
| Test Go + coverage gate                 | `ci.yml`            | `just test-coverage` green, every package ≥ 80%.             |
| Build (goreleaser snapshot + SBOM scan) | `ci.yml`            | `just release-check` / `just release-local` build.           |
| Security Scan                           | `ci.yml`            | govulncheck + Trivy clean (HIGH/CRITICAL fail).              |
| CodeQL                                  | `codeql.yml`        | No new alerts.                                               |
| Dependency licenses                     | `license-check.yml` | `just license-check` clean (allow list in the justfile).     |
| Changelog Drift Check                   | `changelog.yml`     | See [Commits and the changelog](#commits-and-the-changelog). |
| PR Label Check                          | `pr-labels.yml`     | Exactly one of `major` / `minor` / `patch` / `dont-release`. |
| Secret scan                             | `trufflehog.yml`    | Don't commit secrets.                                        |

`just check` locally covers the two you're most likely to trip (lint, test);
`just ci` adds build + license check.

## Commits and the changelog

- **Conventional commits** (`feat:`, `fix:`, `docs:`, `chore:`, …) —
  [cliff.toml](cliff.toml) groups them into [CHANGELOG.md](CHANGELOG.md).
- The **Changelog Drift Check** regenerates the changelog in CI and fails if the
  committed one is stale. After any commit that git-cliff includes (most things
  except `chore(deps)` and changelog-regen commits), regenerate and commit:

  ```sh
  mise x -- git-cliff -o CHANGELOG.md
  git add CHANGELOG.md
  git commit -m "chore: regenerate changelog"
  ```

  That exact message style is skipped by cliff itself, so the regeneration
  converges instead of chasing its own tail.

## Releasing

```sh
just release v0.1.0    # tags and pushes the tag
```

The `v*` tag triggers `release.yml`, which runs `goreleaser release --clean`:
multi-arch archives (linux/darwin × amd64/arm64), checksums, SBOMs, and a GitHub
release. Version metadata is injected via `-ldflags` (`main.version`,
`main.commit`, `main.date`) and surfaced by `champs version`.

Dependency updates arrive as Renovate PRs (`go.mod` via the Go manager,
`mise.toml` via a regex manager configured in `donaldgifford/renovate-config`).

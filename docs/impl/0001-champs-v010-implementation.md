---
id: IMPL-0001
title: "champs v0.1.0 implementation"
status: In Progress
author: Donald Gifford
created: 2026-07-30
---

<!-- markdownlint-disable-file MD024 MD025 MD041 -->

# IMPL 0001: champs v0.1.0 implementation

**Status:** In Progress **Author:** Donald Gifford **Date:** 2026-07-30

<!--toc:start-->

- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Foundations — config and roster](#phase-1-foundations--config-and-roster)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: GitHub App client](#phase-2-github-app-client)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Reconcile engine](#phase-3-reconcile-engine)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: CLI and rendering](#phase-4-cli-and-rendering)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: CI, release, and rollout](#phase-5-ci-release-and-rollout)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Deliver champs v0.1.0 as specified in
[DESIGN-0001](../design/0001-champs-security-champions-team-management-cli.md):
a cobra CLI that reconciles a per-org `security_champions` team as
`roster ∩ org_members` under the hard invariant that the tool never expands
organization access, with Terraform-style plan/apply output and exit codes CI
can react to.

**Implements:** DESIGN-0001

## Scope

### In Scope

- HCL config parsing (`hclkit`) and roster CSV parsing/normalization.
- GitHub App authentication with per-org installation tokens, and the REST and
  GraphQL operations the reconcile needs.
- The reconcile engine: set logic, the membership guard and its `PUT`-state
  backstop, skip classification (`not_org_member`, `unknown_user`,
  `no_installation`), the empty-roster prune guard, bounded org fan-out.
- cobra CLI (`apply`, `plan`, `version`), colorized diff + summary rendering to
  a single stdout stream, `slog` JSON skip records, exit codes `0`/`1`.
- The test suite from the design's Testing Strategy, including the guard
  regression test.
- CI/release verification and pilot rollout tracking.

### Out of Scope

- Inviting users to orgs or granting org membership in any form (the inverse of
  the invariant).
- Assigning the custom security-champions repository role (downstream automation
  keyed off team membership).
- Enterprise Teams or IdP team sync (rejected in DESIGN-0001 Background).
- Managing the roster's content; champs only consumes the CSV.
- Registering and installing the enterprise-owned GitHub App (manual ops,
  repo-guardian pattern) — a prerequisite tracked under Dependencies, not a code
  task.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Foundations — config and roster

Pure-function ground floor: everything later phases consume, nothing that
touches the network.

#### Tasks

- [x] `internal/config`: parse the DESIGN-0001 HCL shape with `hcl/v2` + `gohcl`
      — `team {slug, description, privacy}`,
      `github {app_id, private_key_path}`, repeated `org "<name>" {}` blocks.
      (hclkit swap-out: see Dependencies.)
- [x] Config validation: at least one org (no duplicates), non-empty team slug,
      valid privacy, `app_id` set, at least one private-key source available
      (env wins when both are set); sentinel errors aggregated with
      `errors.Join`, wrapped with `%w`, actionable.
- [x] Private key resolution: `CHAMPS_GITHUB_PRIVATE_KEY` env var (PEM contents)
      wins over `private_key_path`; both sources PEM-sanity-checked
      (`ErrInvalidPEM`) so a mispasted secret fails at load with a named source.
- [x] `internal/roster`: single-column CSV parse — optional header
      (`login`/`username`), lowercase, trim whitespace, drop empty lines, dedupe
      after lowercasing, GitHub login-shape validation with line-numbered
      errors; returns an `internal/stringset.Set` (shared with Phase 3's
      reconcile math: Intersect/Diff/Sorted).
- [x] Table-driven unit tests for config and roster (pure functions, no mocks):
      design-example-verbatim parse, privacy default, diagnostics aggregation +
      Render, every validation sentinel, key resolution matrix, messy-roster
      normalization, login-shape rejections with line numbers, stringset ops.

#### Success Criteria

- `just build` and `just test` pass (race detector on).
- The example config from DESIGN-0001 parses verbatim; messy roster fixtures
  (mixed casing, whitespace, duplicates, with/without header) all normalize to
  the same set.
- Invalid config/roster inputs produce wrapped errors, never panics.

---

### Phase 2: GitHub App client

Everything that talks to GitHub, behind an interface the reconcile engine can
consume and the tests can fake.

#### Tasks

- [x] Add client dependencies: `google/go-github/v89` +
      `bradleyfalzon/ghinstallation/v2` (OQ-1), `shurcooL/githubv4` reusing the
      same installation transport (OQ-3), `gofri/go-github-ratelimit/v2` (OQ-6).
- [x] App auth: JWT from app ID + private key (`gh.NewApp`); resolve the
      installation per org; per-org installation transport (shallow-copied
      `AppsTransport` — the shared pointer races under fan-out) feeding both
      REST and GraphQL clients; `WithBaseURL` points everything (REST, GraphQL,
      token minting) at one test server.
- [x] Missing installation → `ErrNoInstallation` sentinel the engine maps to
      `no_installation` (never a crash).
- [x] `ListOrgMembers` — GraphQL `membersWithRole`, cursor-paginated, logins
      only, returns a lowercase login set (OQ-3).
- [x] `EnsureTeam` — GET by slug, create on 404 with config settings; never
      modifies an existing team's settings.
- [x] `ListTeamMembers` — paginated, returns a lowercase login set (maintainers
      included; prune does not special-case them).
- [x] `AddTeamMember` — membership `PUT`, then assert response
      `state == "active"`; on `"pending"` cancel the org invitation and return
      `*GuardBreachError{Org, User, CancelErr}`.
- [x] `RemoveTeamMember` — membership `DELETE` (team only, never org).
- [x] `UserExists` — `GET /users/{login}` for the residue check (rides the
      installation token; app JWTs only authorize `/app/*` endpoints).
- [x] Wrap the shared transport with secondary-rate-limit retry honoring
      `Retry-After` (OQ-6) — one limiter beneath ghinstallation, so token
      minting, REST, and GraphQL all get it. Known v0.1.0 limitation noted in
      code: retried body-carrying writes are not body-rewound; idempotent reruns
      heal.
- [x] `httptest` fake GitHub server (`internal/ghtest`, reused by Phase 3): REST
      Link-header pagination, team 404→create, GitHub-realistic membership `PUT`
      (org member → `active`; non-member → `pending` + org invitation created),
      invitation cancellation, token minting, a `/graphql` `membersWithRole`
      handler, and one-shot secondary-rate-limit injection (`Retry-After` must
      be ≥ 1 — the limiter ignores 0).

#### Success Criteria

- Every client operation is exercised against the `httptest` fake; unit tests
  make zero real network calls.
- The guard-breach path (`PUT` returns `pending`) has a test asserting the
  invitation is cancelled and the error surfaces.
- A faked secondary-rate-limit response is retried after `Retry-After` and then
  succeeds (retry transport covered).
- Per-org API usage is list-based only — no per-user membership lookups
  (rate-limit stance from DESIGN-0001 holds).

---

### Phase 3: Reconcile engine

The heart of the tool: DESIGN-0001 reconciliation steps 1–8, both guards, and
the concurrency model.

#### Tasks

- [ ] `internal/reconcile`: pure set ops — intersection,
      `adds = desired −     team_members`, `removes = team_members − desired` —
      with table-driven tests.
- [ ] Per-org reconcile unit (steps 1–7) returning a result struct
      `{org, adds, removes, skips, err}`; dry-run computes everything and writes
      nothing.
- [ ] Skip classification: `not_org_member` per user, `no_installation` per org.
- [ ] Cross-org residue check: one `UserExists` call per roster login seen in
      zero orgs; reclassify those skips as `unknown_user`.
- [ ] Empty-roster prune guard: roster parses to zero logins with `--prune` set
      → fail before any writes.
- [ ] Org fan-out: `errgroup` bounded by `--parallelism` (default 5, OQ-5),
      sequential calls within an org, results collected and sorted org-then-user
      after the join.
- [ ] Guard regression test: fake server asserts no membership `PUT` is ever
      issued for a login absent from the org member list.
- [ ] Idempotency test: applying against already-reconciled fake state issues
      zero writes.

#### Success Criteria

- Guard regression and idempotency tests pass.
- A reconcile against unchanged state produces an empty diff and zero write
  calls (asserted by the fake, not just by output).
- Output ordering is deterministic regardless of goroutine completion order.

---

### Phase 4: CLI and rendering

The user-facing shell: cobra commands, the Terraform-style diff, and the single
stdout stream.

#### Tasks

- [ ] cobra root command in `internal/cli`; `cmd/champs/main.go` stays thin (set
      `slog` default JSON handler, call `cli.Execute`, map error to exit 1).
- [ ] `apply` with `--config`, `--roster`, `--orgs`, `--prune`, `--dry-run`,
      `--no-color`, `--parallelism` (default 5).
- [ ] `plan` as a true alias for `apply --dry-run`.
- [ ] `version` subcommand (plus cobra's `--version`) printing the
      ldflags-injected `version`/`commit`/`date` already declared in `main.go`.
- [ ] `--orgs` validation: hard error before any API call when a name is not in
      the config (OQ-4).
- [ ] Diff renderer: adds in green, removals in red; end-of-run summary of
      per-org added/removed/skipped counts, org-level errors, run totals.
- [ ] Color auto-disable: `--no-color` flag, `NO_COLOR` env var, or non-TTY
      stdout.
- [ ] Skip records as `slog` JSON on the same stdout stream.
- [ ] Exit codes: `0` completed (skips included), `1` on any error — fatal or
      per-org; a failing org never aborts the others.

#### Success Criteria

- `just build` produces a working binary; `champs version` prints injected
  metadata (`dev`/`none`/`unknown` locally).
- `plan` against a seeded fake renders the expected diff and summary; piped
  output contains no ANSI escape codes.
- Exit codes verified by test: clean → 0, skips-only → 0, org API error → 1,
  prune guard trip → 1.
- `just lint` passes.

---

### Phase 5: CI, release, and rollout

Prove it in the pipeline and land it in production per the DESIGN-0001 migration
plan.

#### Tasks

- [ ] Repo CI (`.forgejo/workflows/ci.yml` + `.github` mirror) green on the full
      codebase — `just test` + `just lint`.
- [ ] `goreleaser check` passes; ldflags version injection verified in a
      snapshot build.
- [ ] Manual integration: `champs plan` (`--dry-run`) against the sandbox org;
      review output, confirm zero writes.
- [ ] Resolve OQ-2 and land `champs.hcl`, the roster CSV, and the three
      workflows (plan on roster PR, apply on merge, cron drift apply) in the
      chosen home, with `CHAMPS_GITHUB_PRIVATE_KEY` in secrets.
- [ ] Pilot: config with pilot orgs → `plan` reviewed with program owners →
      `apply` → verify rerun produces an empty diff.
- [ ] Expand config to all managed orgs; enable the scheduled workflow.
- [ ] Enable `--prune` in the scheduled apply once the roster is confirmed
      authoritative.
- [ ] Tag `v0.1.0` via `just release`.

#### Success Criteria

- CI green on Forgejo (and the GitHub mirror if active); release pipeline
  validated.
- Sandbox plan shows expected adds/skips with zero writes issued.
- Pilot apply matches its preceding plan; the immediate rerun is a no-op.
- Scheduled workflow runs green and its output shows the skip records (standing
  drift report) as designed.

---

## File Changes

| File                              | Action | Description                                                                               |
| --------------------------------- | ------ | ----------------------------------------------------------------------------------------- |
| `go.mod` / `go.sum`               | Modify | hclkit, cobra, go-github + ghinstallation, githubv4, go-github-ratelimit, color, `x/sync` |
| `cmd/champs/main.go`              | Modify | slog default handler, call `cli.Execute`, keep ldflags vars                               |
| `internal/cli/*.go`               | Create | cobra root, `apply`, `plan`, `version`, flag wiring                                       |
| `internal/config/*.go`            | Create | HCL config parsing + validation, key resolution                                           |
| `internal/roster/*.go`            | Create | CSV parsing + normalization                                                               |
| `internal/gh/*.go`                | Create | App auth, installation tokens, REST/GraphQL operations, retry transport, typed errors     |
| `internal/ghtest/*.go`            | Create | Fake GitHub API server for gh + reconcile tests                                           |
| `internal/reconcile/*.go`         | Create | Set logic, per-org unit, guards, fan-out, result types                                    |
| `internal/render/*.go`            | Create | Terraform-style diff + summary rendering, color handling                                  |
| `champs.hcl` + roster + workflows | Create | Location pending OQ-2                                                                     |

## Testing Plan

- [x] Table-driven unit tests for set logic and login normalization (pure, no
      mocks).
- [x] `httptest` fake GitHub API: pagination, team creation, membership `PUT`
      state handling, invitation cancellation.
- [ ] Guard regression test: no membership `PUT` for a login absent from the org
      member list — the load-bearing invariant test.
- [ ] Idempotency test: rerun against reconciled state issues zero writes.
- [ ] CLI tests: exit codes, `--orgs` validation, no-ANSI-when-piped.
- [ ] Manual sandbox `--dry-run` before first production use (Phase 5).

## Dependencies

- `hashicorp/hcl/v2` + `gohcl` for HCL parsing. DESIGN-0001 named `hclkit`, but
  as of 2026-07-31 hclkit is a fresh scaffold whose only importable package is
  `cmd/hclkit` — it has no public library API. Swap it in if it grows one.
- `spf13/cobra` (house default), `fatih/color`, `golang.org/x/sync/errgroup`.
- `google/go-github` + `bradleyfalzon/ghinstallation` (REST + app auth),
  `shurcooL/githubv4` (member listing), `gofri/go-github-ratelimit`
  (secondary-rate-limit retry).
- Enterprise-owned GitHub App registered and installed across managed orgs
  (manual ops, repo-guardian pattern) — blocks Phase 5, not Phases 1–4.
- A sandbox org for the integration dry-run — blocks Phase 5.

## Open Questions

Answer format: option `a` is my recommendation; `b`+ are alternatives; write in
anything else under Other.

1. **GitHub API client library** — what talks to the REST API?
   - a. `google/go-github` + `bradleyfalzon/ghinstallation` (recommended): typed
     coverage of every endpoint we need; ghinstallation handles app JWT and
     installation-token refresh; both ubiquitous and maintained.
   - b. Hand-rolled `net/http` + `golang-jwt`: two fewer dependencies, but
     re-implements token refresh, pagination, and error mapping — more code to
     test for no capability gain.
   - c. GraphQL (`shurcooL/githubv4`) for reads: fewer calls on huge orgs, but a
     second client/auth path, harder `httptest` fakes, and writes stay REST
     anyway.
   - Other: \_\_\_

   **Answer:** a — `google/go-github` + `bradleyfalzon/ghinstallation`.

2. **Where do `champs.hcl`, the roster CSV, and the three workflows live?**
   - a. A separate ops/roster repo consuming a pinned champs release
     (recommended): roster PRs don't run tool CI and tool PRs can't trigger a
     production apply; the app private key lives only in that repo's secrets;
     champs version bumps are explicit PRs (Renovate-able).
   - b. This repo with path-filtered workflows (`roster/**` triggers
     plan/apply): one repo to manage, but couples tool development with
     production writes and hands this repo's CI the app key.
   - c. Defer: run manually from a workstation until the program's home is
     settled; wire workflows later.
   - Other: \_\_\_

   **Answer:** a — a separate ops/roster repo pinning a released champs version.

3. **Org member listing** — REST or GraphQL?
   - a. REST `GET /orgs/{org}/members?per_page=100` (recommended): native to
     go-github, trivially fake-able, and at homelab scale each org is a few
     pages at most.
   - b. GraphQL `membersWithRole`: fewer round trips for thousands-of-member
     orgs; not worth the second client until an org's member list costs real
     time.
   - Other: \_\_\_

   **Answer:** b — GraphQL `membersWithRole`: some managed orgs have thousands
   of members against a ~100-login roster.

4. **`--orgs` values that aren't in the config** — how strict?
   - a. Hard error before any API call (recommended): a typo'd org name surfaces
     immediately instead of silently reconciling a subset; consistent with the
     fail-fast prune guard.
   - b. Warn and continue with the valid subset: friendlier for scripted
     callers, but a silent partial run hides mistakes.
   - Other: \_\_\_

   **Answer:** a — hard error before any API call.

5. **Org fan-out limit** — how is the worker-pool bound set?
   - a. Unexported constant, e.g. `5` (recommended): no new flag surface;
     revisit only if real runs are slow.
   - b. `--parallelism` flag defaulting to 5: tunable without a rebuild, at the
     cost of one more flag to document and test.
   - c. Fully sequential in v0.1.0: simplest possible; the per-org unit already
     makes parallelizing later a one-line change.
   - Other: \_\_\_

   **Answer:** b — `--parallelism` flag, default 5.

6. **HTTP retry policy** — what happens on transient failures?
   - a. No retry layer (recommended): surface the error into the summary, exit
     1, let the next idempotent run self-heal — matches the design's error
     stance; go-github's rate-limit errors just fail cleanly.
   - b. Retry middleware honoring `Retry-After` on secondary rate limits (e.g.
     `gofri/go-github-ratelimit`): more resilient single runs, another
     dependency and behavior to test.
   - Other: \_\_\_

   **Answer:** b — retry middleware honoring `Retry-After`.

## References

- [DESIGN-0001: champs: security champions team management CLI](../design/0001-champs-security-champions-team-management-cli.md)
- GitHub REST: Teams and team membership endpoints
- GitHub REST: Organization members and invitations endpoints
- `bradleyfalzon/ghinstallation` — GitHub App transport for go-github
- `docs/github-enterprise-setup.md` — enterprise app registration notes

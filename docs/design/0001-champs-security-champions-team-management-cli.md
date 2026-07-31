---
id: DESIGN-0001
title: "champs: security champions team management CLI"
status: Approved
author: Donald Gifford
created: 2026-07-29
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0001: champs: security champions team management CLI

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Authentication](#authentication)
  - [Configuration](#configuration)
  - [Roster input](#roster-input)
  - [Reconciliation algorithm](#reconciliation-algorithm)
  - [The membership guard](#the-membership-guard)
  - [Concurrency](#concurrency)
  - [Run output](#run-output)
  - [Scheduling](#scheduling)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

**Status:** Approved **Author:** Donald **Date:** 2026-07-29

## Overview

`champs` is a Go CLI that manages a per-organization `security_champions` GitHub
team across all managed organizations in the enterprise. It ensures the team
exists in each configured org and reconciles team membership against a champion
roster, under one hard invariant: **the tool never expands organization
access**. A champion is added to an org's team only if they are already a member
of that org. Downstream automation uses team membership as the trigger to grant
a custom security-champions repository role.

## Goals and Non-Goals

### Goals

- Ensure a `security_champions` team exists in every org listed in the HCL
  config.
- Reconcile team membership as the intersection of the champion roster and each
  org's existing membership (`champions ∩ org_members`).
- Accept the roster as a single-column CSV of GitHub logins. The CSV is
  version-controlled; changes land by PR (plan on the PR, apply on merge), so
  there is no separate ad-hoc mutation path.
- Log every skip as a structured `(user, org, reason)` record via `slog`, and
  end each run with a Terraform-style summary (added/removed/skipped per org,
  errors) that doubles as a standing drift report.
- Be fully idempotent: reruns against an unchanged state are no-ops, making the
  tool safe to run on a schedule.
- Optionally prune team members who are no longer on the roster (`--prune`),
  closing the offboarding loop.

### Non-Goals

- Inviting users to organizations or granting org membership in any form. This
  is the inverse of the invariant.
- Assigning the custom security-champions repository role. That remains separate
  automation keyed off team membership.
- Managing enterprise-level (Enterprise Teams) constructs. See Background for
  why they are unsuitable.
- Managing the roster itself. The roster's source of truth lives outside this
  tool; `champs` consumes it.

## Background

The security champions program needs champions grouped per-org so that
repo-level automation can grant them an elevated custom role on repositories in
orgs they already work in. The grouping mechanism must respect existing access
boundaries: not every champion has access to every org, and membership in the
champions program must never itself confer org access.

Two platform-native alternatives were evaluated and rejected:

**Enterprise Teams** (GA on GitHub Enterprise Cloud, June 2026) allow defining a
team once at the enterprise level and assigning it across orgs. However,
assigning an enterprise team to an organization adds all team members directly
to that org without invitation, granting them standard member access. These are
union semantics — assignment grants access — which directly violates the
invariant.

**IdP team synchronization** (classic GHEC with Okta/Entra) has exactly the
desired intersection semantics: team sync is not a provisioning service, so a
user in the linked IdP group is only added to the team if they are already an
org member. This would reduce the problem to "ensure the team exists in each org
and link it to one IdP group," with membership managed in the IdP thereafter.
Two caveats keep this as an alternative rather than the chosen approach: (1)
once linked, team membership cannot be managed on GitHub or via the API,
coupling the program to IdP group workflows; and (2) under Enterprise Managed
Users the equivalent SCIM-driven reconciliation _does_ add non-members to the
org, flipping back to union semantics. See Open Questions.

## Detailed Design

### Authentication

A dedicated **enterprise-owned GitHub App**, following the same ownership and
installation pattern as `repo-guardian`: registered under the enterprise account
(visibility restricted to the enterprise — never public), installed into every
org. Its permission manifest requests only **Organization permissions → Members:
read & write**, which covers team creation, team settings, and team membership.
No enterprise-level permissions are needed on the app itself — teams are
org-scoped APIs, and enterprise ownership is purely where the registration
(private key, settings) is administered.

Installation across all orgs uses GitHub's enterprise app-installation
automation APIs (the enterprise "installer app" pattern) rather than per-org
clicking, matching how repo-guardian was rolled out. Future permission changes
made by the enterprise owner are auto-accepted by orgs in the enterprise, so the
manifest can evolve without chasing org-owner approvals.

The CLI authenticates as the app (app ID + private key). The key is read from
`private_key_path` in config or from the `CHAMPS_GITHUB_PRIVATE_KEY` environment
variable (PEM contents); the env var wins, so CI workflows never write the
secret to disk. The CLI resolves the installation per org and mints a per-org
installation token for all API calls in that org. Each installation carries an
independent rate-limit budget. An org in the config without a corresponding
installation is a distinct error category surfaced in the run output
(`no_installation`), not a crash.

### Configuration

HCL, parsed with `hashicorp/hcl/v2` (`gohcl`) — `hclkit` exposes no public API
yet. The config declares which orgs are managed and the team settings; the
roster is deliberately kept out of the config so it can be exported from
wherever the program tracks champions.

```hcl
team {
  slug        = "security_champions"
  description = "Security champions with existing access to this org"
  privacy     = "closed"
}

github {
  app_id           = 12345
  private_key_path = "/path/to/key.pem" # or CHAMPS_GITHUB_PRIVATE_KEY env var
}

org "org1" {}
org "org2" {}
org "org3" {}
```

The team is created with `name` set to the configured slug; GitHub derives the
actual slug from the name, and for `security_champions` the two are identical.
Team settings are fixed in config — there is no per-org variation.

### Roster input

A single-column CSV of GitHub logins (header optional). Per-user org scoping is
intentionally absent: the invariant makes it unnecessary, since the tool
computes each org's membership intersection itself. `--orgs` acts as an optional
filter narrowing which managed orgs a run touches.

Logins are normalized to lowercase on both sides of every comparison. GitHub
logins are case-insensitive but case-preserving, and roster exports will contain
arbitrary casing; without normalization, phantom skips result. Parsing also
trims whitespace, drops empty lines, and dedupes after lowercasing — CSV exports
reliably contain all three problems.

### Reconciliation algorithm

Per managed org, entirely set-based:

1. List the org's full membership into a set via GraphQL `membersWithRole`
   (cursor-paginated, logins only) — some managed orgs run to thousands of
   members against a ~100-login roster, so the query pulls just logins and
   spends the GraphQL rate budget instead of REST's. **Do not** issue per-user
   membership checks — a 100-champion roster across 50 orgs would be 5,000 point
   lookups versus a few hundred list calls, and the list approach keeps
   installation-token rate limit consumption trivial.
2. Ensure the `security_champions` team exists (create if missing,
   `POST /orgs/{org}/teams`, using the settings from config). Settings apply
   only at creation; an existing team's settings are left untouched — v0.1.0
   reconciles membership, not team settings.
3. List current team membership into a set.
4. Compute `desired = roster ∩ org_members`.
5. `adds = desired − team_members`; issue
   `PUT /orgs/{org}/teams/{slug}/memberships/{user}` for each.
6. With `--prune`: `removes = team_members − desired`; issue `DELETE` for each.
   Prune only touches team membership, never org membership, so it cannot revoke
   anything beyond the champion role.
7. Everything in `roster − org_members` is logged as a skip (`not_org_member`).
8. After all orgs are processed, each roster login that appeared in zero org
   member lists gets one `GET /users/{login}`: if the login does not resolve,
   its skips are reported as `unknown_user` instead. This residue check is the
   only per-user lookup the tool makes.

Reruns against unchanged state produce empty diffs and no writes.

One guardrail: if the roster parses to zero logins while `--prune` is set, the
run fails before any writes. The PR flow reviews roster changes, but the cron
apply runs unattended — a truncated or corrupt CSV must not be able to empty
every team in every org.

### The membership guard

`PUT .../memberships/{username}` on a user who is not an org member **sends them
an org invitation**. This is the single API behavior that would silently violate
the invariant, and it is why membership is computed by set intersection before
any write is issued: the tool never calls the membership endpoint for a
non-member. This guard is load-bearing and must be preserved in any refactor.

As a backstop, every membership `PUT` response's `state` field is checked: it
must be `active`. A `pending` state means an invitation was just sent — a guard
bug — so the tool cancels the invitation and fails the run loudly.

Related edge cases:

- **Pending invitations.** A pending invitee does not appear in the org member
  list, so they fall out as an ordinary `not_org_member` skip — no separate
  detection or category. A later run picks them up once the invite is accepted.
- **Nonexistent users.** A login that does not resolve is a roster data problem,
  not expected drift, and gets a distinct reason code (`unknown_user`, via the
  residue check above). It is reported, never a fatal error.

### Concurrency

Orgs are the unit of parallelism. Each org is fully independent — its own
installation token, its own rate-limit budget, no shared state — so the per-org
reconcile (steps 1–7) is a self-contained unit, fanned out across orgs with a
bounded worker pool (`errgroup`, limit set by `--parallelism`, default 5).
Everything _inside_ an org stays sequential: paginated list calls are inherently
ordered, and GitHub's secondary rate limits penalize concurrent writes, so adds
and removes issue one at a time per org.

Results are collected per org and rendered only after all orgs finish, sorted
alphabetically by org and then by user, so output is deterministic no matter how
the scheduler interleaves the work. The membership checks themselves need no
sorting: member and team lists load into maps, making intersection and diff O(1)
lookups per login.

Plan and apply are the same computation with different endings: compute the
diff, then either render it (plan) or execute it and render what happened
(apply). Apply never consumes a saved plan artifact — it recomputes against live
GitHub state, so the membership guard is evaluated at write time. A champion who
leaves an org between plan and apply falls out of the intersection instead of
receiving an invitation.

### Run output

One stream, no files or artifacts: everything goes to stdout, and the user pipes
or redirects it Unix-style if they want a log. The rendering is a
Terraform-style diff — adds in green, removals in red — followed by the
end-of-run summary of per-org counts (added, removed, skipped), org-level
errors, and run totals. `--no-color`, a set `NO_COLOR` env var, or a non-TTY
stdout disables color, so piped and CI output is plain text by default. Skips
are one structured `slog` record per `(user, org)` pair:

```json
{"level":"INFO","msg":"skip","user":"jdoe","org":"org2","reason":"not_org_member"}
{"level":"INFO","msg":"skip","user":"type0","org":"","reason":"unknown_user"}
{"level":"WARN","msg":"skip","user":"","org":"org3","reason":"no_installation"}
```

Reason codes: `not_org_member` (includes pending invites), `unknown_user`
(org-independent, `org` empty), and `no_installation` (org-level, `user` empty).

Because the skip records are machine-readable JSON, they diff cleanly between
runs — on a schedule this becomes a standing drift report of "champions who do
not yet have access to org X"; when a champion gains access to a new org, the
next run adds them automatically and the entry disappears.

### Scheduling

The roster CSV is version-controlled and changes only by PR: CI runs
`champs plan` on the PR so reviewers see the exact diff (adds, removes, skips),
and the post-merge workflow runs `champs apply`. A cron-triggered run of the
same apply catches drift between roster changes — champions gaining or losing
org membership. Idempotency makes all of this safe; each run's summary output is
the program's drift view.

## API / Interface Changes

The CLI is built with cobra (the house default for Go CLIs).

```text
champs apply    --config champs.hcl --roster champions.csv [--orgs org1,org2] [--prune] [--dry-run] [--no-color] [--parallelism N]
champs plan     --config champs.hcl --roster champions.csv [--orgs ...] [--no-color] [--parallelism N]   # alias for apply --dry-run
champs version
```

- `apply` — full reconcile as described above.
- `plan` / `--dry-run` — print the computed adds, removes, and skips without
  writing, rendered as a colorized Terraform-style diff (see Run output).
- `version` — print `main.version`, `main.commit`, and `main.date` as injected
  at release time via `-ldflags`.
- `--no-color` — plain output; color is also disabled automatically when
  `NO_COLOR` is set or stdout is not a TTY.
- `--parallelism` — bound on concurrent org reconciles (default 5).

There is no ad-hoc single-user command: a one-off change is a one-line PR to the
roster CSV, which gets the same plan/apply treatment as any other change.

Exit codes: `0` — run completed, with or without skips (skips are expected
state, not failure); `1` — errors, whether fatal (cannot authenticate or reach
GitHub) or per-org (an org's API calls failed). A failing org does not abort the
run — remaining orgs still reconcile, the error lands in the summary, and the
next idempotent run retries it.

## Data Model

No persistent state. Inputs are the HCL config and roster CSV; outputs are
GitHub team state and the skip log. GitHub is the source of truth for current
state on every run; the roster is the source of truth for desired membership.
Statelessness is what makes the tool trivially idempotent and schedulable.

## Testing Strategy

- Table-driven unit tests for the set logic (intersection, diff, prune) and
  login normalization — pure functions, no mocks needed.
- A fake GitHub API via `httptest` exercising pagination, team creation, the
  `PUT` response-state backstop, and — critically — an assertion that no
  membership `PUT` is ever issued for a login absent from the org member list
  (regression test for the guard).
- Integration run with `--dry-run` against a sandbox org before first production
  use.

## Migration / Rollout Plan

1. Register the enterprise-owned app with Members (read & write); install it
   across managed orgs via the enterprise installation automation APIs
   (repo-guardian pattern).
2. Land the config with a small pilot set of orgs.
3. `champs plan` against the pilot; review the planned adds and skips with the
   program owners.
4. `champs apply` on the pilot; wire downstream role automation to the team.
5. Expand config to all managed orgs; enable the scheduled workflow.
6. Enable `--prune` once the roster export is confirmed authoritative.

## Open Questions

- **EMU vs classic GHEC.** If the enterprise is classic GHEC, IdP team sync
  remains a lower-code alternative worth a spike: intersection semantics for
  free, at the cost of IdP-managed membership. Under EMU, the CLI is the only
  option with the right semantics.
- **Prune default.** Should `--prune` eventually become the default once trust
  is established, making the roster fully authoritative?
- **Team settings drift.** v0.1.0 applies config settings only at team creation
  and never reconciles an existing team's privacy or description. Revisit if
  pre-existing teams with divergent settings turn out to matter.

## References

- GitHub changelog: Enterprise Teams general availability (2026-06-04)
- GitHub docs: Creating enterprise teams (org assignment grants membership)
- GitHub docs: Managing team synchronization for your organization
  (non-provisioning semantics)
- GitHub docs: Troubleshooting team membership with identity provider groups
  (EMU/SCIM reconciliation adds users to orgs)
- GitHub REST: Teams and team membership endpoints

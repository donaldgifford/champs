# Using champs

champs reconciles a per-org `security_champions` GitHub team so that its
membership is exactly `roster ∩ org_members`: everyone on the champion roster
who is already a member of the org. It operates under one hard invariant:
**champs never expands organization access.** It will never send an org
invitation — a roster entry who is not an org member is reported as a skip, not
invited.

This document is the full operator reference. For a quick orientation see the
[README](README.md); for contributing and local development see
[DEVELOPMENT.md](DEVELOPMENT.md).

## Installation

Pick whichever fits:

- **Release archive** — download the `tar.gz` for your OS/arch from the
  [releases page](https://github.com/donaldgifford/champs/releases), verify
  against `checksums.txt`, and put `champs` on your `PATH`. Archives are built
  for linux and darwin, amd64 and arm64.
- **`go install`**:

  ```sh
  go install github.com/donaldgifford/champs/cmd/champs@latest
  ```

  Note: `go install` builds without release ldflags, so `champs version` reports
  `dev`.

- **From source** — see [DEVELOPMENT.md](DEVELOPMENT.md).

## Prerequisites

champs authenticates as a **GitHub App**, not a personal token:

1. A GitHub App with **Organization permissions → Members: Read and write**. No
   repository or enterprise permissions are needed — teams are org-scoped APIs.
2. The App **installed on every org** you configure. An org without an
   installation is skipped with a `no_installation` record; champs never
   attempts to install the App itself.
3. The App ID (in config) and the App's **PEM private key** (via env var or file
   — see below).

Step-by-step App setup, including a free test org, is in
[DEVELOPMENT.md](DEVELOPMENT.md#testing-against-a-real-github-org).

## Configuration

Configuration is a single HCL file, `champs.hcl` by default (`--config`
overrides). A commented example lives at
[example.config.hcl](example.config.hcl):

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

| Block / attribute         | Required | Meaning                                                                              |
| ------------------------- | -------- | ------------------------------------------------------------------------------------ |
| `team.slug`               | yes      | Team slug (and name) champs manages in every org.                                    |
| `team.description`        | no       | Description applied when champs creates the team.                                    |
| `team.privacy`            | no       | `closed` (default) or `secret`.                                                      |
| `github.app_id`           | yes      | The GitHub App's numeric ID.                                                         |
| `github.private_key_path` | see note | Path to the App's PEM key. Optional when `CHAMPS_GITHUB_PRIVATE_KEY` is set instead. |
| `org "<name>" {}`         | yes, ≥1  | One block per managed org, no duplicates.                                            |

Team settings are applied **only at team creation**. If the team already exists
in an org, champs never modifies its settings — it only reconciles membership.

Validation happens before any API call, and every problem is reported in one
run: empty slug, invalid privacy, missing `app_id`, no key source, no orgs,
duplicate orgs.

### Private key resolution

The key is resolved in this order:

1. `CHAMPS_GITHUB_PRIVATE_KEY` — the **PEM contents** (not a path). Wins
   whenever set and non-empty. Preferred in CI so the key never touches disk.
2. `github.private_key_path` — path to the PEM file.

Both sources are PEM-sanity-checked at load, so a mispasted secret fails
immediately with the offending source named.

## The roster

The roster is a single-column CSV of GitHub logins, `roster.csv` by default
(`--roster` overrides):

```csv
login
alice
bob
carol
```

Parsing rules:

- An optional header is skipped when the first non-empty line is exactly `login`
  or `username`.
- Logins are lowercased (GitHub logins are case-insensitive) and
  whitespace-trimmed; empty lines are dropped; duplicates collapse.
- Every entry must match GitHub's login shape (1–39 alphanumerics and hyphens,
  no leading/trailing/consecutive hyphens). Invalid lines are reported all at
  once with line numbers.
- Multi-column lines are an error — the roster is one login per line.

An empty roster is valid for `plan` and plain `apply`, but combined with
`--prune` it is refused before any API work: an empty roster plus prune would
mean "remove everyone", which is far more likely a bad export than an intent.

## Commands

```sh
champs plan                    # show the diff; writes nothing
champs apply                   # reconcile every configured org
champs apply --dry-run         # identical to plan
champs apply --prune           # also remove members no longer on the roster
champs apply --orgs org1,org2  # limit to a subset of configured orgs
champs version                 # version, commit, build date
```

- **`plan`** is a true alias for `apply --dry-run`: same code path, same output
  modulo `Plan:`/`Applied:` wording, guaranteed zero writes — it won't even
  create a missing team (a missing team renders as everyone-to-add).
- **`apply`** creates the team where missing, adds roster members who are org
  members, and — only with `--prune` — removes team members no longer on the
  roster. Removal is from the **team only**, never from the org.
- **`version`** prints `champs <version> (commit: <sha>, built: <date>)`;
  `champs --version` prints the same.

### Flags (`plan` and `apply`)

| Flag            | Default      | Meaning                                                                                                         |
| --------------- | ------------ | --------------------------------------------------------------------------------------------------------------- |
| `--config`      | `champs.hcl` | Path to the HCL config.                                                                                         |
| `--roster`      | `roster.csv` | Path to the roster CSV.                                                                                         |
| `--orgs`        | all          | Limit the run to these configured orgs (repeatable or comma-separated). Unknown names fail before any API call. |
| `--prune`       | `false`      | Remove team members not on the roster.                                                                          |
| `--parallelism` | `5`          | Max orgs reconciled concurrently.                                                                               |
| `--no-color`    | `false`      | Disable colorized output.                                                                                       |
| `--dry-run`     | `false`      | (`apply` only) Compute the full diff but write nothing.                                                         |

### Environment variables

| Variable                    | Meaning                                                                       |
| --------------------------- | ----------------------------------------------------------------------------- |
| `CHAMPS_GITHUB_PRIVATE_KEY` | GitHub App private key as PEM contents; wins over `private_key_path`.         |
| `CHAMPS_GITHUB_BASE_URL`    | Override the GitHub API base URL (tests and GitHub Enterprise Server).        |
| `NO_COLOR`                  | Any non-empty value disables color, per [no-color.org](https://no-color.org). |

Color is enabled only when stdout is a terminal, `NO_COLOR` is unset, and
`--no-color` was not passed.

## Output

Everything lands on **stdout** in one stream, in a fixed order so runs diff
cleanly against each other:

1. **Per-org diff blocks**, Terraform-style — green `+` per addition, red `-`
   per removal (with `--prune`), sorted by org and login. An org whose reconcile
   failed shows an `error:` line instead; orgs already in the desired state
   print nothing.

   ```text
   org1:
     + alice
     + bob

   org2:
     - mallory
   ```

2. **Skip records**, one JSON line each (`slog` format, timestamp-free):

   ```json
   {
     "level": "INFO",
     "msg": "skipped",
     "user": "carol",
     "org": "org1",
     "reason": "not_org_member"
   }
   ```

   | Reason            | Meaning                                                                                                         |
   | ----------------- | --------------------------------------------------------------------------------------------------------------- |
   | `not_org_member`  | On the roster, exists on GitHub, but not a member of this org. The standing "needs an org invite" drift report. |
   | `no_installation` | The App is not installed on this org (logged at `WARN` — usually a rollout gap).                                |
   | `unknown_user`    | On the roster but the login exists in **zero** configured orgs and no such GitHub user exists — likely a typo.  |

3. **Summary** — `Plan: 2 to add, 1 to remove, 3 skipped.` for dry runs,
   `Applied: 2 added, 1 removed, 3 skipped.` after writes, with `, N error(s)`
   appended only when there were any.

Fatal errors that prevent the run entirely (bad config, bad roster, the
empty-roster prune guard, unknown `--orgs` names) go to **stderr**.

## Exit codes

| Code | Meaning                                                                                             |
| ---- | --------------------------------------------------------------------------------------------------- |
| `0`  | Run completed. Skips are normal operation and do not affect the exit code.                          |
| `1`  | Anything went wrong: a fatal startup error, or any per-org error (the other orgs still reconciled). |

This is the contract CI reacts to: exit `0` means the reported state is
trustworthy; exit `1` means re-run after fixing the cause — champs is
idempotent, so re-running is always safe.

## Safety model

Worth knowing as an operator:

- **Never invites to orgs.** The desired set is computed as
  `roster ∩ org_members` _before_ any write, so a team-membership `PUT` is only
  ever issued for existing org members. As a backstop, if GitHub ever answers a
  `PUT` with a `pending` (invitation) state, champs cancels that invitation
  immediately and reports the org as errored.
- **Per-org isolation.** Orgs reconcile independently (bounded by
  `--parallelism`); one org's API failure never blocks the others. Within a
  failing org, champs stops at the first write error and reports the partial
  progress it made.
- **Idempotent.** A second run over an already-reconciled fleet issues zero
  writes and prints only the standing skips.
- **Deterministic output.** Same fleet state, same output bytes, at any
  parallelism — safe to diff run-over-run.

## Running in CI

The intended deployment is an ops repo that owns `champs.hcl` and `roster.csv`,
with the App key in a secret:

```yaml
- name: Plan on roster PRs
  run: champs plan
  env:
    CHAMPS_GITHUB_PRIVATE_KEY: ${{ secrets.CHAMPS_GITHUB_PRIVATE_KEY }}
```

- **On roster PRs:** run `champs plan` and surface the diff for review.
- **On merge to main:** run `champs apply`.
- **On a schedule:** run `champs apply` to converge drift (members who left
  orgs, manual team edits).

Exit code `1` fails the job; the `not_org_member` skip lines in the log double
as the list of champions who still need an org invitation through the normal
(human) process.

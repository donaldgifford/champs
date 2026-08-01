# Maintainers

## Current maintainers

| Name           | GitHub                                             | Email            | Role            |
| -------------- | -------------------------------------------------- | ---------------- | --------------- |
| Donald Gifford | [@donaldgifford](https://github.com/donaldgifford) | <dgifford@pm.me> | Lead maintainer |

## What maintainers do

- Review and merge pull requests (see [CONTRIBUTING.md](CONTRIBUTING.md) for the
  contribution flow and [DEVELOPMENT.md](DEVELOPMENT.md) for what CI enforces).
- Apply the release label (`major` / `minor` / `patch` / `dont-release`) to
  every PR — the label check blocks merges without one.
- Cut releases: tag with `just release vX.Y.Z`; CI does the rest.
- Triage issues and Renovate/Dependabot PRs.
- Own the safety invariant: any change touching the membership guard, the
  `PUT`-state backstop, or the prune guard needs a maintainer's explicit
  sign-off and must keep the guard-regression and idempotency tests meaningful.

## Security issues

Do **not** open a public issue for a vulnerability. Use
[GitHub private vulnerability reporting](https://github.com/donaldgifford/champs/security/advisories/new)
or email a maintainer directly.

## Becoming a maintainer

Sustained, high-quality contributions (code, review, docs, triage) are the path.
An existing maintainer nominates; the current maintainers agree; the new
maintainer is added to this file in the same PR that grants access.

# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/).
## [0.1.0] - 2026-08-02

### Features

- *(config)* Add HCL config types and diagnostics-first parsing
- *(config)* Validate semantic constraints with aggregated errors
- *(config)* Resolve GitHub App private key with env-first sourcing
- *(roster)* Parse single-column roster CSV into a string set
- *(gh)* GitHub App client with per-org installation tokens
- *(reconcile)* Pure set math and result types
- *(reconcile)* Per-org unit with dry-run and skip classification
- *(reconcile)* Bounded org fan-out and empty-roster prune guard
- *(reconcile)* Cross-org unknown_user residue check
- *(render)* Terraform-style diff, skip records, and summary
- *(cli)* Cobra apply/plan/version with exit-code contract

### Documentation

- Approve DESIGN-0001 and add IMPL-0001 implementation plan
- *(readme)* Describe the reconcile CLI, usage, and output contract
- *(readme)* Add language to layout code fence (MD040)
- *(impl)* Record phase 5 release-verification progress
- *(impl)* Phase 5 targets GitHub Actions, not Forgejo
- *(claude)* Repo lives on GitHub, drop Forgejo references
- *(claude)* Add languages to code fences (MD040)
- Add Apache-2.0 license file
- *(impl)* CI green on PR #1 — check off the phase 5 CI task
- Add USAGE, DEVELOPMENT, MAINTAINERS, example config; refresh README
- *(impl)* Check off the sandbox integration test — live hoomlab run

### Styling

- *(roster)* Gofumpt-format test table row
- *(gh)* Group related constants, use Go 1.26 new(expr)
- *(reconcile)* Wrap overlong function signatures

### Testing

- *(config,roster)* Cover Phase 1 with table-driven suites
- *(gh)* Fake GitHub server and full client test suite
- *(reconcile)* Guard regression and idempotency invariants
- Add e2e fixtures for the hoomlab test org

### Miscellaneous Tasks

- Close Phase 1 — style review fixes, IMPL-0001 in progress
- Add coverage-gate recipe and fix codecov slug
- Add Forgejo primary workflow, fix snapshot SBOM path
- Pin trufflehog to v3.96.0
- *(ignore)* Pem keys
- *(gitignore)* *.pem
- Add live e2e plan job against the hoomlab test org


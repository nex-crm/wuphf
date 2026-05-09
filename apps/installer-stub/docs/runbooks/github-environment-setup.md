# GitHub Environment Setup

Last updated: 2026-05-09 / Owner: @FranDias

The release workflow uses the `production-release` environment to keep Apple
and Azure signing secrets out of PR jobs and out of non-signing build steps.

## Required Environment

1. In GitHub, open Settings -> Environments.
2. Create an environment named `production-release`.
3. Add required reviewers for release approval.
4. Add the Apple and Azure secrets listed in the Apple and Azure runbooks as
   environment secrets.
5. Do not duplicate those signing secrets as repository-wide secrets.

## Workflow Expectations

- `pull_request` runs are restricted to PRs targeting `main` and use `pr` mode.
- Tag pushes matching `v[0-9]*-rewrite` use `production` mode.
- Signing secrets are injected only into the platform-specific detection,
  keychain, notarization, or Azure signing steps that need them.
- The publish job also uses `production-release` so draft release upload remains
  behind the same environment approval gate.

## Validation

After setup, push a test rewrite tag and confirm GitHub pauses for environment
approval before any Apple or Azure secret is used. If a tag run starts signing
without an environment approval, stop the run and move the secrets from
repository scope into `production-release`.

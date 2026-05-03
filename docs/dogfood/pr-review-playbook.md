# PR Review Playbook Fixture

## Review Priorities

1. Behavioral regressions and data loss risks.
2. Contract drift between Go handlers, Web API types, and TUI renderers.
3. Missing tests at service, route, or renderer boundaries.
4. Ownership conflicts across domain, Web, TUI, shared API, and docs files.
5. Documentation gaps in `docs/surfaces.md` and architecture guides.

## Agent PR Expectations

- The PR is draft until relevant checks have run.
- The description lists changed files by domain.
- Task evidence names tests/checks run and known risks.
- Surface parity changes are explicit.
- New route behavior has typed inputs/outputs or a documented debt item.

## Review Comments

Actionable review comments should name the file, line, expected behavior, and
test that would catch the issue. Avoid broad rewrites unless the current shape
blocks the contract or ownership model.

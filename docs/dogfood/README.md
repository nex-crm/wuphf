# WUPHF Dogfood Fixtures

These markdown files are seed context for agents working on WUPHF itself. They
are intentionally local-first and can be ingested into the wiki/notebook or used
as project context without requiring external services.

Fixture files:

- [`wuphf-roadmap.md`](wuphf-roadmap.md) - near-term product and architecture
  themes.
- [`refactor-pr-queue.md`](refactor-pr-queue.md) - a realistic stack of
  decomposition PRs.
- [`release-checklist.md`](release-checklist.md) - evidence needed before a
  release.
- [`ci-triage-playbook.md`](ci-triage-playbook.md) - check failure triage flow.
- [`pr-review-playbook.md`](pr-review-playbook.md) - review expectations for
  agent-authored PRs.
- [`architecture-decisions.md`](architecture-decisions.md) - current decisions
  agents should treat as durable unless superseded.

Keep fixture facts sourced. If a future fixture mixes local wiki facts, Nex
context, integration signals, human notes, and agent notebook entries, each item
should identify that origin inline.

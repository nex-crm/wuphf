# Architecture Decisions Fixture

## Durable Decisions

- WUPHF is a context graph platform for AI agents. Do not describe it as a
  different product category in code, docs, comments, or copy.
- Local-first behavior is the default. External context providers are additive
  and must expose provenance.
- The canonical repo-work pipeline is:
  task -> local_worktree -> checks -> review -> draft_pr -> CI -> human gate.
- Domain service methods should own business behavior. HTTP handlers adapt
  request parsing, auth, status codes, and JSON encoding to those methods.
- Web and TUI surfaces render shared contracts unless a row in
  `docs/surfaces.md` documents an intentional gap.
- Web unit and component tests run through Vitest via
  `bash scripts/test-web.sh` for the full suite and
  `bash scripts/test-web.sh web/src/path/to/file.test.ts` for focused runs.
  `bun test` is reserved for Bun-native package-local tests outside `web/`.

## Decisions Needing Follow-Up

- Choose the route registry or generation strategy for long-term API contracts.
- Decide whether review queue and notebook are contracted TUI capabilities or
  intentionally Web-only.
- Define the source-of-truth contract for task completion evidence.

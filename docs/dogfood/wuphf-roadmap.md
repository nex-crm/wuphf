# WUPHF Roadmap Fixture

## Current Themes

- Make domain boundaries explicit so agents can own tasks, requests, reviews,
  wiki/notebook, workspaces, providers, integrations, skills, and platform work
  independently.
- Move surface parity to contract-first planning. Web and TUI may render
  differently, but both should point at the same capability contract when a
  capability is shared.
- Keep repo work on the canonical pipeline:
  task -> local_worktree -> checks -> review -> draft_pr -> CI -> human gate.
- Preserve local-first behavior. Nex context, integration events, and notebook
  entries should add provenance instead of hiding state.

## Near-Term Slices

1. Extract typed task service methods and keep `/tasks` handlers as adapters.
2. Add review queue terminal renderer or explicitly mark review as web-only.
3. Move wiki search/read/write into a named contract consumed by Web and TUI.
4. Add a route registry for new API endpoints before broadening Web API types.
5. Seed WUPHF-on-WUPHF project context into local wiki/notebook fixtures.

## Evidence Of Progress

- `docs/surfaces.md` rows stay current in feature PRs.
- Each new domain method has service-level and route-level tests.
- Task completion evidence includes changed files, checks run, logs, branch,
  risks, and next human action.

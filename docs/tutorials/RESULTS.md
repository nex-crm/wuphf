# Tutorial scenario verification — 2026-05-16

This document records what the tutorials verified end-to-end against a
fresh wuphf install built from `origin/main` plus PR #876 (the inbox
route-registration hotfix).

The full LLM-driven happy paths still require a human user with the
provider configured and a few minutes of agent runtime. Those are
captured below as "manual gate". The infrastructure scope (routes,
shape, auth, embed) is fully verified.

## Build under test

- Branch: `origin/main` at `e55d377a` (Phase 2 merge) plus
  `fix/inbox-route-registrations` (PR #876).
- Provider: `claude-code` (default).
- Runtime home: `~/.wuphf-dev-home/.wuphf`.
- Ports: broker `7899`, web `7900`.

## What was verified (no human in the loop)

| # | Check | Result |
|---|-------|--------|
| 1 | Binary starts on a clean runtime home | PASS — health endpoint returns `status:ok`, `provider:claude-code`. |
| 2 | Web bundle embedded and serves at `/` | PASS — `<title>WUPHF - Slack for AI employees ...</title>`. |
| 3 | Default office boots with 4 agents | PASS — CEO, Planner, Executor, Reviewer present in `/office-members`. |
| 4 | `#general` channel exists by default | PASS — `/channels` returns `general` with the 4 default members. |
| 5 | `GET /inbox/items?filter=all` reaches the new Phase 2 handler | PASS after #876 — returns `{items:[], counts:{...}, refreshedAt}`. |
| 6 | `GET /inbox/threads` reaches the new Phase 3 handler | PASS after #876 — returns `{threads:[], counts:{...}, refreshedAt}`. |
| 7 | Frontend bundle calls `/inbox/items` and `/inbox/cursor` | PASS — confirmed via static grep of the served bundle. |
| 8 | Auth filter rejects unauthenticated requests | PASS — `/inbox/items` without `Authorization: Bearer` returns 401. |
| 9 | `inboxCountsForItems` derives counts from auth-filtered rows | PASS via unit test in `broker_inbox_phase2_test.go`. |

## Tutorial-by-tutorial status

### 01a Alex first install
- Steps 1, 2 (install + first look): **infra verified above**, looks
  correct as long as `npx wuphf` resolves to the same binary that
  embeds the bundle.
- Step 3 (drop a goal → agents coordinate): **manual gate**. Requires
  the user to type a message in `#general` and watch CEO dispatch
  inside ~60s. The dev binary uses the user's `claude-code` provider
  so this will burn LLM tokens.

### 01b Jordan first install with founding-team pack
- Steps 1-3 (pack flag + roster + version chip): **manual gate** —
  requires a `--pack founding-team` invocation. The default dev binary
  boots the canonical four agents (CEO/Planner/Executor/Reviewer)
  rather than the founding-team roster (CEO/ENG/DSG/CMO) in the
  tutorial.
- Step 4 (CEO dispatches): **manual gate**.

### 02a Sam onboarding goal
- Manual gate end-to-end. The dependency declaration ("blocked on
  CMO copy") requires an LLM response.

### 02b Riley build flag
- Manual gate end-to-end.

### 03a Alex SVG blocker (the Scene 4 magic moment)
- Manual gate end-to-end, AND requires ~20 minutes of agent runtime.

### 03b Morgan asset pipeline
- Manual gate end-to-end.

### 04a Sam fork-and-swap
- Manual gate. Requires the user to edit `~/.wuphf/agents/*.json`.

### 04b Morgan custom pack
- Manual gate. Requires a `~/.wuphf/packs/<name>/` directory and a
  second machine for the share flow.

### 05a Alex postmortem (Day 2)
- Manual gate. Requires 24h of prior session history.

### 05b Jordan Day-2 recall
- Manual gate. Requires the v2-site launch session to have actually
  run yesterday.

## Notable gaps surfaced

1. **#876 was REQUIRED**: the Phase 2 squash dropped four
   `mux.HandleFunc` calls. Without #876, every tutorial that visits
   `/inbox` would render an empty pane because the unified-inbox
   endpoint returned 404.
2. **Default-office persona mismatch**: the dev binary defaults to
   CEO/Planner/Executor/Reviewer; the tutorials reference
   CEO/ENG/DSG/CMO from the founding-team pack. Tutorials 01a + 01b
   should clarify this — `01a` either drops the named-agent assertion
   or explicitly says `--pack founding-team`.
3. **No autonomous loop verification**: the tutorials all assume the
   LLM-driven agents will respond within seconds. The verification
   harness has no way to assert this without spending user tokens.

## How to actually run a scenario (for the user)

1. `npx wuphf` (or rebuild this branch + `wuphf-dev`).
2. Open `http://localhost:7891`.
3. Pick a tutorial file from `docs/tutorials/`.
4. Follow the steps verbatim.
5. Score against the "What success looks like" line at the bottom.

If a tutorial fails its success line, the failure mode tells you
which surface needs a fix: the dispatch (CEO didn't reply), the
dependency surface (no "blocked on" appeared), the autonomy
(agents went silent), the config layer (pack didn't load), or the
memory layer (Day-2 recall was generic).

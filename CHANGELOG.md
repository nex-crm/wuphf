# Changelog

All notable changes to WUPHF will be documented in this file.

## [0.0.4.1] - 2026-04-17

### Added
- **One CLI is now selectable in Settings → Integrations → Action Provider.** The dropdown was missing the option even though the action registry already routed to One CLI by default for connections, action execution, and relays. The React settings UI, the legacy HTML fallback, and the typed API client all expose the option now.

### Fixed
- **Saving `action_provider = one` from the web UI no longer 400s.** The `POST /config` handler's allowlist only accepted `auto` and `composio`, so even though `/config set action_provider one` worked from the CLI, clicking Save in the web UI silently failed with HTTP 400 "unsupported action_provider". Added a regression test covering every provider value the registry supports.

## [0.0.4.0] - 2026-04-17

### Added
- **Shred your workspace from Settings.** New "Danger Zone" section in the web Settings with a `Shred workspace` button that deletes your team, company identity, office task receipts, and workflows, then reopens onboarding on next launch. The card lists exactly what gets deleted vs preserved, and the confirm modal requires typing `i am sure` before firing. Task worktrees, logs, sessions, LLM caches, and `config.json` are always preserved.
- **`wuphf shred` CLI subcommand.** Full workspace wipe that reopens onboarding. Prompts for the verb to confirm, or takes `-y` for scripted teardown. `wuphf kill` kept as an alias.
- **`/shred` slash command in the TUI.** Wipes the workspace in-process, then exits the session so your next `wuphf` boots clean. The existing `/reset` (clear transcript and refresh panes) is unchanged.

## [0.0.3.0] - 2026-04-14

### Added
- **Skill invocations now drop you in the channel where the run is happening.** Click `⚡ Invoke` on the Skills tab, or run `/skill invoke <name>` from anywhere, and the UI jumps to the channel so you can watch the agents pick up the work instead of staring at the Skills list wondering if anything happened.

### Fixed
- **Broker stays up when something panics.** A panic inside a message-notification handler, task-action handler, or headless codex turn used to kill the whole broker (no stack, no logs). Three long-running goroutines now recover panics, write the full stack to `~/.wuphf/logs/panics.log`, and keep the office alive. If you see the broker die silently after this, that file will tell us exactly what blew up.
- **`/skills/<name>/invoke` now returns the resolved channel in its response.** The UI uses this to redirect reliably even when the skill has a default channel that differs from where you invoked from.

## [0.0.2.1] - 2026-04-14

### Removed
- **`docs/` removed from version control.** All planning documents, specs, and analysis files under `docs/` are now gitignored — local-only, never committed. Keeps the repo focused on shipped code.

## [0.0.2.0] - 2026-04-14

### Added
- **Resume in-flight work on restart.** When WUPHF shuts down with tasks in progress or conversations mid-flight, work now automatically resumes when WUPHF comes back up. On startup, agents receive a resume packet listing their active tasks (with stage, status, and working directory for worktree-isolated work) and any unanswered human messages awaiting their response. No more orphaned tasks or dropped conversations after a crash or restart.
- **Spec-compliant routing.** Resume packets route using pack membership: tagged messages go to the tagged agents, untagged messages go to the pack lead. Agents no longer in the current pack are silently skipped. The CEO is always enqueued first in headless mode to bypass the queue-hold guard.
- **29 new tests** covering in-flight detection, reply-chain parsing, pack membership filtering, 1:1 mode, nil-broker safety, terminal status exclusions (including `completed`), nex-sender inclusion, and the full resume flow in both tmux and headless paths.

### Changed
- `RecentHumanMessages` now includes the `nex` sender alongside `you` and `human`, so Nex automation messages that triggered work are correctly captured in resume packets.
- `findUnansweredMessages` now only counts replies from agent senders, so human-to-human thread continuations no longer falsely mark a message as answered.

## [0.0.1.0] - 2026-04-14

### Added
- **Proactive skill suggestions.** CEO agent now detects repeated workflows during normal conversation and proposes reusable skills via `[SKILL PROPOSAL]` blocks. Proposals surface as non-blocking interviews in the Requests panel. One-click accept activates the skill, reject archives it. The system learns from the team's actual work instead of requiring manual prompt editing.
- **Author-gated proposal parsing.** Only the team lead (CEO) can trigger skill proposals via message blocks. Prevents specialists and pasted transcripts from creating false proposals. Empty offices reject all proposals by default.
- **Agent team suggestions via existing tools.** CEO can suggest new specialist agents using the existing `team_member` and `team_channel_member` MCP tools with human approval via `human_interview`. No new data model needed.
- **11 unit tests** covering the full skill proposal lifecycle: CEO happy path, non-CEO rejection, malformed input, dedup, re-proposal after rejection, non-blocking interview creation, accept/reject callbacks, prompt content verification, persistence round-trip.

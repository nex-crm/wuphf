# Changelog

All notable changes to WUPHF will be documented in this file.

## [0.0.5.3] - 2026-04-21

### Fixed

- **Messages in #general now reach your team lead instead of silently disappearing.** Five operation blueprints (`niche-crm`, `local-business-ai-package`, `bookkeeping-invoicing-service`, `multi-agent-workflow-consulting`, `paid-discord-community`) and two legacy packs (`coding-team`, `lead-gen-agency`) declared a non-ceo `lead_slug` (`operator`, `tech-lead`, `ae`). Broker code hardcodes `"ceo"` for channel-member injection, focus-mode routing, and the one-on-one default, so human messages dispatched to `@ceo` — an agent that never existed in those blueprints — and got dropped with no trace. The lead slug is now `ceo` everywhere, and `TestAllOperationBlueprintsUseCEOLead` guards against regression. Fresh onboards route correctly. Existing installs that still have `operator` registered in `~/.wuphf/state.yaml` need a reset; a migration for in-place upgrades lands in a follow-up.

## [0.0.5.2] - 2026-04-20

### Fixed

- **Pane spawn no longer races across concurrent WUPHF instances.** Two launches on the same machine used to share a hardcoded tmux socket (`wuphf`) and session name (`wuphf-team`). When a second launch ran `kill-session` between the first launch's `new-session` and `split-window`, tmux tore down the whole server and the first launch's split-window exited 1 with "no server running on /private/tmp/tmux-*/wuphf". That race is what pushed prod into the headless fallback message "Running in headless mode (spawn visible agents failed: spawn first agent: exit status 1). Install tmux and relaunch to use interactive Claude sessions on your normal subscription." even though tmux was installed and healthy. The socket and session names now carry a `-<port>` suffix on any non-default broker port — dev (7899), worktree launches, CI, or any custom `--broker-port` — so prod on the default port still uses the original `wuphf` / `wuphf-team` and concurrent instances can't trample each other. Backward compatible: default-port installs keep historical names.
- **Pane spawn failures now carry the real tmux error.** `spawnVisibleAgents` used `.Run()` which threw away tmux's stderr, so the #general system message was limited to "exit status 1" with no hint of cause. Both split-window calls now use `CombinedOutput()` and append `(tmux: <stderr>)` to the wrapped error — e.g. `spawn first agent: exit status 1 (tmux: no server running on /private/tmp/tmux-501/wuphf)` — so the next failure diagnoses itself from the fallback message alone.

## [Unreleased] — `feat/llm-wiki`

### Added — LLM Wiki (Karpathy-pattern team memory)

- **Git-native team wiki at `~/.wuphf/wiki/`.** Every article is a real markdown file in a local git repo. Each agent commits under its own git identity (per-commit `-c user.name=...` flags — never touches your global git config). Your team's memory is explicit, yours, file-over-app, and portable. `cat` it, `git log` it, `git clone` it anywhere.
- **`--memory-backend markdown` as the new default for fresh installs.** Existing Nex/GBrain users keep their current backend via `.wuphf/config.yaml` — no forced migration. `--memory-backend` now accepts `markdown | nex | gbrain | none`, and the MCP tool surface switches accordingly: markdown exposes `team_wiki_*` tools, the knowledge-graph backends keep the existing `team_memory_*` tools. The two never coexist on one server instance.
- **Serialized-write worker with fail-fast backpressure.** All writes flow through a single goroutine-owned queue (buffered 64, 10s per-write timeout). On saturation the MCP tool returns `wiki queue saturated, retry on next turn` — no hidden retries, no silent blocking. Covered by an IRON regression matrix that verifies exact tool-registration per backend.
- **Crash recovery on startup.** If the wiki repo has uncommitted changes from a prior crashed write, startup auto-commits them with a `wuphf-recovery` author. No data loss, full trace in `git log`.
- **Backup mirror + double-fault recovery.** Every commit kicks off a debounced async copy to `~/.wuphf/wiki.bak/`. If the repo corrupts and the backup is healthy, startup restores automatically. If both are corrupt, WUPHF falls back to `--memory-backend none` with a banner rather than crashing.
- **Graceful fallback when git is unavailable.** Detected at startup; WUPHF disables the wiki backend and shows a banner telling you to install git. Never crashes.
- **Transactional blueprint materialization.** Each of the 6 shipped blueprints (`bookkeeping-invoicing-service`, `local-business-ai-package`, `multi-agent-workflow-consulting`, `niche-crm`, `paid-discord-community`, `youtube-factory`) declares a domain-specific `wiki_schema:` with thematic directories and skeleton articles. On blueprint pickup during onboarding, those land in your wiki via temp-dir-then-rename — either all skeletons land or none do. Idempotent: re-picking a blueprint never overwrites existing articles.
- **Wikipedia-style UI at `/wiki`.** Reading-first editorial layout: Fraunces display headings, Source Serif 4 body at 18px/1.72, warm-paper `#FAF8F2` palette. Full Wikipedia information architecture — Article/Talk/History hat-bar, infobox with dark title band, italic strapline ("From Team Wiki, your team's encyclopedia"), hatnote cross-refs, numbered nested TOC with `[hide]`, Page Stats / Cite This Page / Referenced By panels, Categories chip footer, Wikipedia-style page footer with "View git history / Cite / Download as markdown / Clone wiki locally" actions, fixed-bottom live edit-log footer pulsing on every `wiki:write` SSE event. Agent pixel avatars on every byline — ties the wiki visually to the rest of the WUPHF app. See `DESIGN-WIKI.md` for the full spec.
- **18 React components under `web/src/components/wiki/`** with 90%+ test coverage via Vitest + React Testing Library (net-new frontend test infrastructure, also usable by every future feature). `react-markdown` + `remark-gfm` + `remark-wiki-link` + `rehype-slug` + `rehype-autolink-headings` render article content. `dompurify` sanitizes. SSE live-update on `wiki:write` invalidates the affected article's TanStack Query cache in real time.
- **Wikilink parser** with shared Go ⇄ TypeScript test fixture at `web/tests/fixtures/wikilinks.json` — both parsers consume the same canonical grammar cases. Syntax: `[[slug]]` → `team/slug.md`, `[[slug|Display]]` → renders "Display" but links to slug. Broken wikilinks (target doesn't exist) render red with a trailing marker; healthy ones render with a dashed-blue underline that solidifies on hover.
- **`GET /wiki/article?path=...` rich endpoint** returns article content + extracted title (first H1) + revision count + contributors + backlinks (reverse index via tree walk) + word count. Matches `web/src/api/wiki.ts WikiArticle`. Agents read via MCP (`team_wiki_read` — raw markdown); UI reads via the rich endpoint.

### Architecture notes

- **Three design systems, one repo.** `DESIGN.md` covers the pixel-office marketing site (dark, Press Start 2P). `web/src/styles/global.css` covers the general Slack-inspired web app. `DESIGN-WIKI.md` covers the `/wiki` surface (editorial-reference, warm paper, Fraunces + Source Serif 4). Each scope has non-interchangeable rules.
- **Per-agent wikis are deferred to v1.1.** v1 ships team wiki only. Per-agent `agents/{slug}/` introduces a private-on-filesystem access model that isn't load-bearing for the demo moment.
- **LLM merge-resolver is deferred to v1.1.** v1 uses serialized writes — no concurrent commits can conflict. Merge-resolver only worth building once the serialized-write path shows measurable pain at real-world load.
- **Nex compounding intelligence layer (entity briefs, playbook compilation, skill generation) is deferred to v1.1.** These sit additively on top of the markdown files and are disableable — the file-over-app guarantee is preserved forever.

### Internal

- New Go packages touched: `internal/team/wiki_git.go`, `wiki_worker.go`, `wiki_article.go`, `wiki_e2e_test.go` + tests; `internal/operations/wiki_materialize.go` + tests; additions to `internal/teammcp/server.go`, `internal/team/broker.go`, `internal/team/broker_onboarding.go`, `internal/config/config.go`. New env var `WUPHF_MEMORY_BACKEND` drives the tool-surface switch (matches the existing `WUPHF_CHANNEL` / `WUPHF_AGENT_SLUG` propagation pattern from broker to MCP subprocess).
- 33+ new Go tests at 81.6% coverage on wiki files (`wiki_git.go` · `wiki_worker.go` · `wiki_article.go`). 80 new web tests at 90% coverage on `web/src/components/wiki/` and `web/src/lib/`. Cross-lane integration tests in `internal/team/wiki_e2e_test.go` exercise the full HTTP stack.
- Full-repo `go test ./...` green across all 25 packages. `go test -race ./internal/team/... -run TestE2EWiki` clean.

## [0.0.5.1] - 2026-04-20

### Fixed
- **Blueprint channel names no longer leak `{{command_slug}}` as literal text.** Onboarding blank-slate seeding now renders the `{{command_slug}}` template variable alongside `{{brand_name}}` and `{{brand_slug}}`, matching the sibling code paths in `internal/company/blueprints.go` and `internal/team/operation_bootstrap.go`. Default channels created from blueprint starter packs show a real command-room slug (e.g., `acme-co-command`) instead of `{{command-slug}}`.

## [0.0.5.0] - 2026-04-17

### Added
- **Won't Do column in the Tasks board.** Canceled tasks now have their own lane next to Done instead of disappearing silently. Drag a card onto it (or use the task detail modal's "Won't do" action) to record that the work was skipped without deleting it. Empty Won't Do / Blocked / Pending columns stay hidden when idle and reappear as drop targets while you are dragging.
- **Task detail modal with owner reassign and won't-do action.** Click any task card to open a detail view, reassign the owner in place, or mark the work as won't-do without leaving the board.

### Changed
- **"Blocked" stat on the Office Activity view split into two pills.** The single "Blocked" card used to show `blocked tasks + watchdog alerts` combined so a "2" there could mean anything. Now you see "Blocked lanes" and "Watchdog alerts" as separate counts, and clicking either pill smooth-scrolls down to the "Needs attention" list where you can act on the items. Both are keyboard-activatable (Enter/Space) with an accessible label.

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

# Changelog

All notable changes to WUPHF will be documented in this file.

## [Unreleased]

### Added

- **`@wuphf/credentials` per-agent OS keychain substrate.** New v1 package for
  opaque credential handles backed by macOS Keychain, Linux libsecret, and
  Windows Credential Manager adapters. Handles serialize only an opaque id,
  include `agentId` in the keychain account key, reject Linux plaintext
  `basic_text` collections, and keep broker construction explicit instead of a
  global singleton.
- **Channel participant rail for conversations.** Channel views now include a Slack-like participants list that shows which agents are part of the current channel, opens an agent panel from each row, filters out human seats, and keeps lead agents pinned. The rail supports adding available office agents, disabling or enabling specific channel participants, removing agents from only the current channel, and undoing a remove from the toast within five seconds.
- **Skills app reskinned as pixel-art trading cards.** Every entry in the Skills tab is now a TCG-style card with a procedurally-generated 144px pixel-art portrait (using the existing `drawPixelAvatar` system), status-driven type palette (active = electric, proposed = psychic with "NEEDS REVIEW" stamp, disabled = dark, archived = steel), and a 3D card flip (700ms, ease-out-expo, respects `prefers-reduced-motion`).
  - **Front face:** Title + creator byline, procedural portrait, status/owner stat strip, promoted "Triggers on" row, and scrollable flavor-text description.
  - **Back face:** Full skill metadata (`<dl>` with status, owners, created by, created/updated dates, source) plus "View full SKILL.md →" CTA into the SidePanel preview.
  - **Visual treatment:** TCG aspect ratio (63:88), rounded outer corners fused with the pixel-border treatment, scanline overlay for CRT feel. Press Start 2P + VT323 fonts preloaded.
  - **Accessibility:** Card faces use `inert` (off-axis face removed from tab order); stat strip wrapped in `<dl>` with visually-hidden Status/Owners labels for screen readers; flip button uses `aria-pressed` for toggle semantics, not disclosure; doubled black-then-cardstock focus ring; touch targets ≥ 36px desktop / 44px on coarse-pointer devices.
  - **Performance:** `contain: paint` on the card wrap caps repaint cost on grids of 20+ cards.
  - **Continuity:** All in-app skill flows (approve, reject, suggest changes, archive, disable, invoke) preserved below the card. Skill metadata pinned to UTC so created/updated dates render the same calendar day for every viewer.
- **Live agent event bubbles on the WUPHF roster.** Each agent row in the office sidebar now shows a per-agent pill that pulses on activity (halo for routine and milestone events), holds the most recent action for 60s (120s for milestones), dims into a 60s grace window, then rotates through role-aware Office voice idle copy ("watching tests", "doodling in Figma", "looking at memes") so the rail always feels alive even before any agent has done work. A new `Kind` field on the activity SSE event lets the backend tag events as `routine`, `milestone`, or `stuck`; a `headlessActivityClassifier` table maps tool/status/detail inputs onto the right kind. Stuck detection has two paths into the same UI surface: a 90-second stale-while-active reaper for agents that go quiet mid-task, and a watchdog hook for explicit alert escalation. Stuck pills get bordered chrome (works under `prefers-reduced-motion`) and fire an `aria-live="assertive"` announcement once per transition. A first-run nudge under the first agent row points new users at the chat ("→ tag `@<agent>` in #general") and disappears the moment any human posts; the broker tracks `humanHasPosted` durably (in-memory bool seeded from a one-time message-log scan on bootstrap) and surfaces it on `/office-members.meta` so the frontend hook needs no extra round-trip. EventSource disconnect grace is 5 seconds before the rail dims and a "Reconnecting…" badge appears at the bottom of the agent list; polling continues underneath so state recovers automatically without a banner.
- **Private-network team-member sharing with `wuphf share`.** A running office can now mint a one-use invite, start a private Tailscale/WireGuard web listener, and let one team member join the shared office without exposing the local broker bearer token. Public binds are blocked by default; existing single-user `npx wuphf` launches are unchanged.
- **Wiki-backed team learnings — agents can now record and retrieve compact reusable lessons separately from procedural playbooks.** New `team_learning_record` and `team_learning_search` MCP tools write validated, typed learnings into the team wiki, regenerate a human-readable `team/learnings/index.md`, surface playbook-linked lessons in the wiki UI, and inject high-confidence prior learnings into future agent prompts.
- **Multi-workspace support — run multiple isolated WUPHF teams side by side, switch between them with one click, and pause unused workspaces to stop burning tokens.** A new left-rail switcher shows every workspace, the create modal pre-fills from your current workspace's blueprint and config, and pause/resume drains in-flight agent work cleanly. Each workspace gets its own broker process on its own port pair (7910–7999), its own agent roster, and its own state under `~/.wuphf-spaces/<name>/.wuphf/`. API keys are forked at create time so you can mix providers across workspaces without one workspace overwriting another's config.
- **`wuphf workspace` CLI — list, create, switch, pause, resume, shred, restore, doctor.** Full lifecycle from the terminal, JSON output for automation (`--json`), and a `wuphf --workspace=<name> <cmd>` global override for single-shot commands without flipping the active workspace. `doctor` reconciles the registry against reality: orphan trees, zombie running state, port conflicts, partial migration, broken symlinks — each fix is opt-in.
- **Page-reload-on-switch architecture for the web rail.** Clicking a workspace in the rail navigates to that broker's URL (full reload). The SPA never sees more than one broker's auth token, CORS stays restricted to the served origin, and the UI never has to manage cross-broker state. The simpler architecture replaces an earlier smooth-switch design that codex outside-voice flagged as overbuilt and a real XSS-blast-radius regression.
- **Shred is reversible for 30 days.** `wuphf workspace shred` moves the workspace to `~/.wuphf-spaces/.trash/<name>-<timestamp>/` instead of deleting. Restore via `wuphf workspace restore <trash-id>` (CLI) or the 30-second undo toast (web). Auto-cleanup runs on the next CLI invocation after 30 days.
- **Symmetric workspace layout — every workspace lives at `~/.wuphf-spaces/<name>/.wuphf/`, including main.** First launch on a workspace-aware build atomically renames `~/.wuphf` to `~/.wuphf-spaces/main/.wuphf` and leaves a compatibility symlink for one release cycle so external scripts referencing `~/.wuphf/...` keep working. No special-cased main, no asymmetry tax compounding into future features.

### Changed

- **Pause now drains every Launcher subsystem, not just tmux pane dispatch.** A new `Launcher.Drain(ctx)` cancels the headless dispatch context, scheduler loop, watchdog loop, notify-poll loop, and pane dispatch in lockstep. The previous drain only covered tmux pane sends, which left web-mode's default headless dispatch path running through pause. Codex outside-voice caught this: pause was advertising drain semantics while leaving most of the dispatch surface untouched.
- **Phase-0 audit migrated 18 sites from `os.UserHomeDir()` to `config.RuntimeHomeDir()`.** Every WUPHF-state path now respects `WUPHF_RUNTIME_HOME`, so a broker spawned with `WUPHF_RUNTIME_HOME=/some/path` writes its state under that path instead of bleeding into the user's real `~/.wuphf/`. LLM CLI auth paths (`~/.codex`, `~/.claude`, `~/.config/opencode`) intentionally stay user-global so re-login is not required per workspace. A greppable regression test enforces the boundary on every CI run; an integration leak test spawns a real broker with an isolated runtime home and asserts zero writes under `$HOME/.wuphf/`.
- **`killStaleBroker` now verifies the detected PID's binary path matches `wuphf` before sending SIGKILL.** With workspaces reusing the 7910–7999 range, the previous behavior could have killed an unrelated process bound to a previously-vacated port. Codex outside-voice flagged this as a real safety regression that scope-aware port allocation introduces.
- **Per-agent opencode config now writes under `<runtime_home>/.wuphf/opencode-configs/<slug>.json` instead of `~/.config/opencode/opencode.<slug>.json`.** Two workspaces with the same agent slug would have raced on the shared opencode config directory; namespacing under runtime home eliminates the collision while preserving the read of the user's base opencode config (themes, providers).
- **Human approval cards now show what the action will actually do.** When an agent calls `team_action_execute` for a mutating external action (sending a Gmail email, posting to Slack, updating a HubSpot contact, etc.), the human approval card now surfaces a structured "What this will do" panel with the recipient, subject, body excerpt, and other decision-relevant payload fields, plus a faint footer with the action ID, account, and channel. Before this, the card said only "Approve gmail action: GMAIL_SEND_EMAIL" and the human had to dig into the agent transcript to see what was actually being sent. The new layout uses a recessed inset panel for the payload bullets (theme-aware via `--overlay-soft` so it reads correctly in both light and dark themes), label/value typography (faint `--text-secondary` label, prominent `--text` value), a `(truncated)` chip when an email body exceeds 240 runes, and a "No additional details available" fallback line for empty-payload cases. An `EXTERNAL ACTION` badge sits next to `BLOCKING` so the user sees at a glance that this is a side-effect-causing call. Both the modal `HumanInterviewOverlay` and the inline `InterviewBar` render through the shared `ApprovalContextView` component for consistency. Long bodies clip with rune-aware truncation so multi-byte characters (CJK, emoji) cannot land mid-codepoint and produce invalid UTF-8.

### Security

- **Closed a confused-deputy bypass in the human approval gate.** Agent-controlled fields (`Summary`, `ConnectionKey`, `ActionID`) now flow through `sanitizeContextValue` before they enter the approval card's context string. Without this, a prompt-injected agent could craft a `Summary` containing fake `What this will do:` / `Action:` / `Channel:` sections at line starts; the web parser's first-match-wins regexes would surface the FORGED structure to the human, hiding the real action. The sanitizer collapses every newline variant (LF, CRLF, U+2028, U+2029) to a space and replaces the bullet glyph U+2022 with the middle dot U+00B7, so forged section headers cannot land at a line start where the parser's `^Section:` regexes match. The forged tokens still appear as inline text inside the rendered Why — visible to the human as a long run-on sentence that looks suspicious by construction. Adversarial regression tests (`TestBuildActionApprovalSpecRejectsForgedSummary`, `TestBuildActionApprovalSpecRejectsForgedConnectionKey`) pin the defense.

### Deprecated

- **`wuphf --tui` is renamed to `wuphf --legacy-tui`.** The old flag remains as a warning-only alias for now; the legacy bubbletea TUI is slated for removal after the desktop replacement lands.

### Fixed

- **Public tunnel start now retries through transient `trycloudflare.com` failures instead of bouncing straight to the user.** When Cloudflare's free QuickTunnel API returns a 5xx or HTML body (the `error code: 1101` / "Error unmarshaling QuickTunnel response" / "failed to unmarshal quick Tunnel" chain), `cloudflared` exits in under a second before publishing a URL. Previously, that surfaced as a hard failure on the first click. The tunnel controller now respawns `cloudflared` up to 3 times with a 1.5s/3s backoff on a recognized transient signature (5xx + QuickTunnel unmarshal errors), keeping the loopback listener and share server stable across attempts so only the subprocess cycles. Non-transient failures (timeout, missing binary, context cancel, pipe error) skip the retry path so the user is not waiting through pointless respawns. If every attempt fails, the surfaced error gains a hint that `trycloudflare.com` itself looks unhealthy.
- **First-run agent nudge now reads as a high-contrast retro speech bubble instead of black text on the purple sidebar.** The "→ tag @&lt;agent&gt; in #general" line that points new users at the office chat had no CSS rule attached, so it inherited the body text color and rendered at roughly 3:1 contrast on the Nex sidebar. It now ships as an NES-style pixel dialog box: olive-yellow fill, near-black mono text (≈16:1 contrast, WCAG AAA), four zero-blur stacked `box-shadow` strokes for a sharp pixel border, a soft drop-shadow underneath for CRT depth, and a chunky 4×4 pixel tail that steps up-left from the bubble toward the agent avatar. A 1.6s `translateY` bob draws the eye without moving any layout-bound properties; `prefers-reduced-motion: reduce` falls back to a static bubble. Matches the existing PixelAvatar aesthetic in the agent rail.
- **Workspace endpoints are wired before the web broker starts serving.** Creating a workspace from the web UI no longer hits `503 {"error":"workspaces not configured"}` because `/workspaces/*` routes now receive the orchestrator during broker construction instead of after `LaunchWeb` blocks forever.
- **Newly created agents now use the office avatar sprite system instead of falling back to the deprecated legacy procedural sprites.** Operation-generated slugs like `planner`, `builder`, `growth`, `reviewer`, and `operator` now resolve to generated office portraits, and arbitrary custom slugs get deterministic office-style procedural portraits in both web and TUI surfaces.
- **Passing `--pack`/`--blueprint` no longer deletes broker state on launch.** Restarting the same office after a crash can pass the selected blueprint again; the launcher treated that as an authoritative reseed and removed `broker-state.json`, making the office look wiped even though `wuphf shred` was never run.
- **Fresh wikis no longer warn during Stage B skill synthesis because `team/agents/` does not exist yet.** The notebook signal scanner now treats the missing agents directory as an empty signal source.
- **Caret no longer drifts when typing past an `@agent` chip in the composer.** The mirror-overlay treatment rendered mention chips with 4–5px horizontal padding and `font-size: 0.9em`, so each chip took more width than the raw text in the textarea behind it. The caret fell behind the typed characters by a few pixels after every chip. Composer chips now inherit the textarea's font metrics exactly (15px, no padding, `display: inline`) and only apply a background highlight — layout-neutral, so character-for-character alignment with the textarea is preserved. Message-bubble chips keep the original styled pill treatment.
- **Self-heal task explosion is capped at 3 active tasks per agent.** An agent that failed on N distinct task IDs previously opened N self-heal entries because the dedupe key was `(agent, taskID)` and each new taskID landed a new entry. On long-running installs this produced hundreds of stale incident tasks for a single agent. Once an agent reaches `maxActiveSelfHealsPerAgent` (default 3, override with `WUPHF_SELF_HEAL_MAX_ACTIVE_PER_AGENT`), additional escalations merge their incident detail into the most recently updated active self-heal task instead of opening a new one. The agent slug match is anchored on the title's trailing space (the literal `@<slug>` followed by a space) so prefix-overlapping slugs like `eng` and `engineering` keep their repair lanes isolated. Adds four targeted tests covering the burst cap, the substring-collision guard, per-agent independence, and the exact-reuse path winning over overflow merge.

### Changed

- **Headless `claude --print` is now the default dispatch path for both `wuphf` (web) and `wuphf --legacy-tui`.** Anthropic re-sanctioned headless CLI reuse in the 2026-04 OpenClaw policy note, and it runs on the user's normal subscription quota — no separate extra-usage charge. Every turn now dispatches as a fresh `claude --print` invocation, matching how the Codex runtime already worked and unifying dispatch across both modes. The previous per-agent long-lived interactive tmux pane path is preserved as an internal fallback primitive (reachable if dispatch ever needs to promote to panes at runtime) but is no longer invoked at startup. tmux is still required for `--legacy-tui` since the channel-view TUI runs in tmux; the web UI no longer needs it.
- **Pane-fallback messaging updated to reflect the new default.** The stderr banner and `#general` system post no longer frame headless as "extra-usage quota" — it isn't, anymore. Messaging now reads as: pane-backed fallback attempted but unavailable → continuing with the default headless path on your normal subscription.
- **Inline Enter-key hint on wizard CTAs drops the redundant `Enter` word.** The hint rendered both a `↵` key pill and the word `Enter` next to it — same information twice. Now it's just the pill. On the task step (⌘+Enter), the modifier is rendered as its own pill so the cue stays two key pills instead of one pill plus a string.

## [0.0.7.1] - 2026-04-23

### Fixed

- **Rapid `@agent` tags now coalesce into one reply instead of cutting claude off mid-turn.** The previous per-slug queue served bursts serially but the 3-second minimum gap between sends was shorter than a typical claude turn (~30s), so the second `/clear` still wiped the first reply mid-generation. The queue now tracks a 60-second coalesce window per slug: if a new notification arrives while claude's previous turn is still in flight, the new prompt is MERGED into the pending next dispatch instead of triggering a fresh `/clear`. Claude sees one combined prompt with every question separated by a divider and answers them together. Single tags still dispatch immediately — coalesce only engages when there's a live turn to protect.

## [0.0.7.0] - 2026-04-22

### Added

- **Run the whole web app without a mouse.** Press `?` anywhere (outside a text field) to open a keyboard reference with every shortcut grouped by Global / Command palette / Onboarding wizard / Slash commands / Composer / Feed navigation.
- **`⌘1`–`⌘9` jump to channels 1–9.** The first nine channels in the sidebar now advertise their shortcut with a faint kbd badge that lights up on hover/focus/active. Dark-sidebar legible via `currentColor` inheritance.
- **Enter advances every wizard step.** Identity, Templates, Team, Setup, and Ready all move forward on Enter when their own gate is satisfied. The Task step keeps Enter for newlines and uses `⌘/Ctrl+Enter` to advance, with an inline kbd hint spelling out the difference. Every primary CTA carries a visible `↵ Enter` badge.
- **`? shortcuts` button** in the status bar opens the help modal for mouse users who don't know the shortcut exists.
- **Shared `<Kbd>` + `<KbdSequence>` component** with platform-aware modifier (`⌘` on macOS, `Ctrl` elsewhere). One visual treatment across sidebar hints, wizard CTAs, help modal, status bar, and command palette footer.
- **`:focus-visible` rings** on template tiles, runtime tiles, team tiles, and task suggestions — keyboard users can see where they are without imposing an outline on mouse users.

### Fixed

- **Escape no longer cascades through every open panel.** Pressing Escape with the help modal + command palette + thread panel open used to close all three at once. The modal now uses capture-phase `stopImmediatePropagation` so one press closes only the topmost surface.
- **Wizard Enter stops hijacking focused buttons.** Enter on Back/Skip/runtime-reorder buttons used to advance the step instead of firing the button. On the Ready step, Enter on Skip submitted the task anyway. Enter is now ignored when focus is on a `<button>`, `<a>`, or `<select>`.
- **Hold-Enter on the Ready step no longer corrupts config.json.** The wizard's Enter handler now bails on `e.repeat`, preventing parallel writes to the broker's /config endpoint (which has a known race under concurrent requests).
- **Focus restoration no longer targets detached DOM nodes.** Closing the help modal checks `isConnected` before calling `focus()` on the previously-focused element — if that element was unmounted while the modal was open, focus falls back cleanly.
- **Wizard gate logic and platform detection deduplicated.** `cmdEnterLabel()` now reuses the shared `MOD_KEY` from the Kbd component, and the inconsistent `hasAtLeastOneKey` / `hasAnyApiKey` names are unified.

## [0.0.6.6] - 2026-04-22

### Added

- **@-mentions render as colored chips in both the composer and your own messages.** Typing `@pm` in the textbox now shows a pink chip inline via a mirror-overlay (same font metrics as the textarea, transparent text on top, caret still visible). Human message bubbles apply the same treatment so `@pm what are you doing?` reads with the chip, not as a literal `@pm` token. Unknown slugs like `@joedoe` stay plain text — the chip only fires for registered agents. Shared helper `parseMentions` powers both surfaces and uses the same pattern as the broker's routing regex so what the UI highlights is exactly what the backend will route. Mirror scroll-syncs with the textarea so chips stay aligned once a message exceeds the 120px visible cap.

## [0.0.6.5] - 2026-04-22

### Fixed

- **Rapid `@agent` tags no longer clobber each other mid-turn.** Two `@pm` tags in quick succession used to type two `/clear` + prompt sequences into the same tmux pane back-to-back; claude's TUI finished one turn (answering whichever input it had fully parsed) and silently ate the other. A per-agent dispatch queue now serialises pane notifications with a 3-second minimum gap between `/clear` cycles, so each question gets its own turn. Different agents still dispatch in parallel — the gap is per-slug, not global.

## [0.0.6.3] - 2026-04-22

### Fixed

- **Web UI no longer thrashes with `Maximum update depth exceeded` on every render.** `useCommands` returned a freshly-built array on every call, so the Autocomplete effect watching `commands + items` re-fired on every Composer render, calling `onItems(items)` which re-set `acItems`, which re-rendered Composer. The hook now memoizes the derived array against the underlying query data, giving stable referential identity across renders.
- **Dead tmux panes no longer silently break the live-output stream forever.** When an office reseed (or an agent crash, or an overflow-pane recreation) invalidates a pane target, the capture loop used to log "stopped after 5 failures" and exit permanently. It now sleeps 30s, re-resolves the current pane target from the launcher, and resumes — so long-running sessions recover from pane churn without a restart.
- **Headless codex queue no longer spins in a stale-cancel livelock.** Prod logs showed dozens of `stale-turn: cancelling active turn after 0s` lines per session because an enqueue could preempt an active turn that had just started. Cancellation now requires both the configured staleness threshold AND a 2-second minimum turn age, floors out tight cancel loops without blocking legitimate preemption.

## [0.0.6.2] - 2026-04-22

### Fixed

- **Typing `@pm` in a human message now tags PM.** The web composer does not always commit typed `@slug` text into an explicit tag chip, so the POST body arrived with `tagged: []`. Routing then treated the message as untagged and woke CEO instead. The broker now auto-promotes `@slug` body mentions to `tagged` for every sender (not only agents), provided the slug matches a registered agent. Conversational `@` references to non-agents stay untouched.
- **`office_reseeded: respawn panes failed: tmux: no server running` no longer logs as an error.** Web/headless mode never has a tmux server; attaching to it is expected to fail and the headless dispatch path handles delivery. The no-session error class is now silenced there, so the console stops surfacing a recurring error for a normal code path. Real failures (permission denied, corrupt state) keep logging.

## [0.0.6.1] - 2026-04-22

### Fixed

- **@-tagging a specialist now wakes the specialist, not CEO.** Tagging any non-CEO agent in `#general` was silently routing to CEO instead, because the channel-membership filter ran before the explicit @-tag check. A newly hired specialist is in the broker's member list but not yet in `ch.Members` for `#general`, so the filter dropped the notification and CEO absorbed the message. Explicit @-tags from humans or CEO now bypass the channel-membership filter, the sender's intent is explicit and trumps domain inference. Both collaborative and focus modes are patched.
- **DMs to specialists now reach the specialist.** Agents hired via the web wizard (`POST /office-members`) were added to the broker's roster but not to the launcher's in-memory pack, so `activeSessionMembers()` silently excluded them from `agentNotificationTargets`. Any DM or explicit @-tag targeting a wizard-hired agent dropped into the void. `activeSessionMembers` now appends broker-only members after pack-listed ones, keeping pack ordering stable while ensuring every hired agent is reachable.

## [0.0.6.0] - 2026-04-21

### Fixed

- **Agents boot into real tmux panes again — no more silent drop to the extra-usage quota.** Pane-backed spawn was failing on every launch with `tmux: command too long` because the launch command embedded the full 5-10 KB system prompt as a single shell argument, exceeding tmux's internal command-parse buffer. The prompt now goes to `$TMPDIR/wuphf-prompt-<slug>.txt` and the launcher passes `--append-system-prompt-file` to `claude`, keeping the tmux command under 4 KB. A regression test pins the length bound so future prompt growth cannot repeat the bug. Interactive panes also mean no more parallel `claude --print` subprocess storm at launch, which removes the CPU-starvation path that was making the web UI appear to boot-timeout on first launch.
- **Pane-spawn failure now tells you what actually broke.** The fallback message used to say "Install tmux to run agents on your normal subscription" whether tmux was missing OR tmux was installed but rejected the command. If you hit the second case, the advice was wrong and misleading. The launcher now distinguishes the two cases: missing → install it; rejected → file a bug with the detail.
- **Broker tokens and system prompts no longer linger in `$TMPDIR` after the session ends.** `Launcher.Kill()` now removes per-agent `wuphf-mcp-*.json` and `wuphf-prompt-*.txt` files for every known office member on shutdown.
- **`npx wuphf` no longer falsely warns about itself.** The PATH shadow detector treated the npm package's `wuphf.js` launcher as a separate binary shadowing the native `wuphf`, even though both ship in the same `node_modules/wuphf/bin` directory. Sibling files in the running binary's own directory are now recognized as one install, not a shadow. Real shadows (a hand-built copy in `~/.local/bin`, a stale homebrew install) still surface.
- **Boot-error diagnostics survive the 10-second watchdog.** If the web UI threw a specific error during bootstrap (`Uncaught error: X`, `React failed to mount`), the 10s watchdog used to destructively replace that overlay with a generic "Boot timeout" message, erasing the actionable signal. First overlay now wins, and the generic watchdog no longer fires once any fatal is already on screen. When it does fire, the detail now includes `document.readyState`, `location.hash`, and which `/assets/index-*` bundles actually finished downloading — so a real boot-timeout can be debugged instead of guessed at.

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
- **`wuphf shred` CLI subcommand.** Full workspace wipe that reopens onboarding. Prompts for the verb to confirm, or takes `-y` for scripted teardown.
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

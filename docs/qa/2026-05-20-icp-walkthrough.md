# WUPHF ICP Walkthrough QA — 2026-05-20

Branch under test: `qa/icp-walkthrough-2026-05-20` (forked from `origin/main` at `30d7e30f`).
Tester surface: web UI on `http://127.0.0.1:7902`, broker on `7901`, isolated
`HOME=~/.wuphf-qa-icp`. Browser driven via `browser-harness` against the user's
Chrome. Screenshots saved under `/tmp/wuphf-qa-shots/`.

The walkthrough exercised the 17-step "ideal workflow" the user described.
The agent-execution path (steps 5–11, 16) could not be observed end-to-end
because the CEO subprocess fails authentication on first turn (see B-04);
everything up to and including step 4 (issue spec + approval surface) was
exercised. The remaining steps were inspected by reading the routes,
schemas, and on-disk shapes that should support them.

Severity rubric:
- **P0** — blocks the ICP from completing the workflow at all.
- **P1** — significantly degrades the first-run experience.
- **P2** — friction or polish; the workflow still completes.

---

## A. How far each workflow step actually got

| # | Step | Status | Notes |
|---|------|--------|-------|
| 1 | Pick provider | ✅ Works | Claude Code / Codex / Opencode auto-detected; CLI/key tabs render. |
| 2 | Onboarding + website scan | ⚠️ Partial | Website asked, scan fires, but fails on a perfectly readable URL and the UI gets stuck on the "Scanning…" chip. See B-05, B-06, B-09, B-10. |
| 3 | #general task → CEO opens issue with spec | ❌ Blocked | CEO replies "Not logged in · Please run /login" instead of decomposing. The Phase-4 LLM draft loop is wired but never reachable (B-04). |
| 4 | Issue visible + commentable, CEO iterates | ⚠️ Form-only | `/#/issues/new` works for human-filed issues, but the detail view renders an empty body + no comment composer. See B-15, B-16. |
| 5 | CEO spins up focused agents | ❌ Not exercised | Scratch path seeds just CEO; boot log still claims "4 agents" (B-12). |
| 6 | Integration approvals show full request context | ❌ Not exercised | Inbox routing surface is empty; could not provoke an approval card. |
| 7 | Agents log to personal notebooks | ❌ Not exercised | Notebook subtab exists but stays on "Loading article…" indefinitely (B-17). |
| 8 | Artifacts of the run (drafts + wiki articles + playbooks) | ❌ Not exercised | Activity App ("activity") doubles as Artifacts; surface exists, no events. |
| 9 | Playbook promotion + CEO review | ❌ Not exercised | Wiki "Reviews" tab is present but cannot be tested without an agent running. |
| 10 | Canonical playbooks → skills | ❌ Not exercised | Skills app present (`Compile` button), zero skills until an agent fills the wiki. |
| 11 | Notebooks + wiki as runtime context | ❌ Not exercised | Same blocker as 7/8. |
| 12 | Wiki articles as rich HTML, not plain MD | ❌ Confirmed gap | All current wiki content on disk is plain `*.md` (`team/about/README.md` etc.); no rich-component renderer wired. See I-04. |
| 13 | Skills surfaced as editable `*.md` in wiki | ⚠️ Partial | `team/skills/.system/migrated-skill-consolidation.md` lives in the wiki tree but Skills app is its own surface and the wiki UI does not list it under About / Skills. |
| 14 | Each agent owns its own skills only | ❌ Not exercised | Schema supports it; UX path not testable here. |
| 15 | Visibility into other agents' skills/tasks | ❌ Not exercised | Office Overview app renders but is empty in scratch state. |
| 16 | Self-heal on agent failure | ❌ Not exercised | The first failure (login-required) surfaces as a flat chat reply, not as a self-heal event (B-13). |
| 17 | Agent-authored policies pending CEO review | ❌ Not exercised | Policies app route works but `team/policies/` is not auto-listed; no review path observed. |

---

## B. Bugs

### B-01 — `bootstrap.sh` does not build the web bundle, so a clean checkout serves a fallback page
**Severity:** P1.
**Repro:** Fresh `git clone` → `./scripts/bootstrap.sh` → `go build -o ./wuphf ./cmd/wuphf` → `./wuphf`. The browser lands on a page titled "WUPHF web UI assets are missing" with instructions to run `cd web && bun run build` manually.
**Evidence:** `/tmp/wuphf-qa-shots/01-initial.png`. The Go binary embeds `web/dist/index.html`, but `scripts/bootstrap.sh` only runs `bun install` in `web/`, not `bun run build`. The first-run user has no signal that they need to do this themselves.
**Fix sketch:** Add `(cd web && bun run build)` to `scripts/bootstrap.sh` after the `bun install` block, or have `cmd/wuphf` detect the empty bundle and offer to run the build itself.

### B-02 — Workspace-migration pre-flight refers to port 7890 even when the broker is being told to use a different port
**Severity:** P2.
**Repro:** `WUPHF_BROKER_PORT=7901 ./wuphf --broker-port 7901 --web-port 7902 …`. Stderr prints:
```
workspace migration: workspaces: migrate: broker is running on port 7890; stop WUPHF before upgrading, then restart with `npx wuphf`
```
even though no broker is on 7890. The instruction to run `npx wuphf` is also wrong for a source build (you would run the same binary you just compiled).
**Fix sketch:** Use the configured broker port in the migration probe and recovery hint; suppress the line entirely when no broker is running at all.

### B-03 — Onboarding accepts a "detected" runtime without verifying it can authenticate
**Severity:** P0.
**Repro:** Provider-pick screen shows "Claude Code · Detected · 2.1.145 (Claude Code)". Selecting it advances the wizard. The first agent turn then fails with "Not logged in" (B-04). A clean user that installed Claude Code but never ran `claude login` would see no signal until their first task collapses.
**Fix sketch:** During the provider tile probe, run a `claude --print "ok"` (with a 5s budget) or query the OAuth keychain. If unauthenticated, label the tile "Not signed in — click to log in" and offer an inline action; do not advance the wizard until it succeeds.

### B-04 — CEO's "Not logged in" reply tells the user to run a slash command that does not exist in the WUPHF UI
**Severity:** P1.
**Repro:** With a runtime that cannot reach its credentials (e.g. wuphf running with `HOME=~/.wuphf-qa-icp/` while the real Claude OAuth lives in the user's keychain under `$HOME/.claude/`), any `@ceo …` message in `#general` triggers:
> CEO · Not logged in · Please run /login
Typing `/login` in the composer then surfaces "Unknown command: /login. Try /help." — there is no path from this state to authentication via the UI.
**Evidence:** `/tmp/wuphf-qa-shots/run2-after-login-cmd.png`.
**Root cause:** `isClaudeLoginRequired` in `internal/provider/claude.go` returns true and the broker forwards the upstream Claude CLI's own "/login" guidance verbatim. WUPHF has no `/login` skill or in-chat re-auth affordance.
**Fix sketch:** Render the login-required state as a system error card (not a CEO chat bubble) with a "Sign in to Claude" button that shells `claude login` in the host terminal or opens the in-Settings runtime card; rewrite the copy to mention the actual command (`claude login`, `codex login`, etc.) rather than `/login`.

### B-05 — Wizard step counter is inconsistent: `STEP X OF 5` ↔ `PHASE: WEBSITE` ↔ `PHASE: SCAN`, then jumps from 3 → 5
**Severity:** P2.
**Repro:** During onboarding the header reads:
- Step 1 of 5 · OFFICE NAME
- Step 2 of 5 · WHO YOU ARE  *(but the prompt is "What does Acme Postmark do?", which is a company description, not who-you-are)*
- PHASE: WEBSITE *(no step counter)*
- PHASE: SCAN *(no step counter)*
- Step 3 of 5 · PICK A STARTING BLUEPRINT
- Step 5 of 5 · FIRST TASK *(step 4 — team trim — silently skipped when blueprint = "scratch")*
**Fix sketch:** Either keep the same chrome on every phase (`STEP n of N`) and account for the skipped team-trim step, or drop the step counter and use phase names everywhere. The mismatched "WHO YOU ARE" label is also a copy bug — the phase asks for the company description, not the human's identity.

### B-06 — Website scan reports failure on a perfectly readable URL with no diagnostic
**Severity:** P1.
**Repro:** Wizard PHASE: WEBSITE → enter `https://example.com` → wizard prints "Scanning example.com…" then "Couldn't read example.com — skipping the scan." Zero further information about why.
**Evidence:** `/tmp/wuphf-qa-shots/run2-scan-t2.png`.
**Notes:** `operations.SeedCompanyContext` (called via `runScanPhase`) treats `result.NeedsRetry || scanErr != nil` as failure but only ever logs the underlying error via `log.Printf`. The user sees only the generic refusal.
**Fix sketch:** Surface a one-line reason ("not enough text on the page", "request timed out", "blocked by Cloudflare", etc.) and keep the verbose stack in `~/.wuphf-qa-icp/logs/onboarding.log`.

### B-07 — `materializeScratchWikiStubs` is never called from the scratch seed path
**Severity:** P2.
**Repro:** Pick "Start from scratch" → after onboarding completes, `~/.wuphf/wiki/README.md` and `~/.wuphf/wiki/team-charter.md` are missing.
**Evidence:** `find ~/.wuphf-qa-icp/.wuphf/wiki -name README\*` yields only `team/about/README.md` (written by a different seeder), not the two top-level files that `materializeScratchWikiStubs` promises to write. The function exists at `internal/team/broker_onboarding_phase2.go:251` but no caller invokes it; `runSeedPhase`'s scratch branch only calls `seedMinimalScratchLocked` + `ensureNotebookDirsForRoster`.
**Fix sketch:** Either delete `materializeScratchWikiStubs` (it's dead code) or wire it after `seedMinimalScratchLocked` and add a regression test asserting `wikiRoot/README.md` exists on the scratch path. Today there is a stale promise in a doc comment that the code does not keep.

### B-08 — Wiki article loader is stuck on "Loading article…" for an article that exists on disk
**Severity:** P1.
**Repro:** `#/wiki/team/about/README.md` after onboarding-completes. The file is present (`team/about/README.md`, 9 lines, valid markdown) and the index "About This Team" entry routes to it correctly, but the right pane reads "Loading article…" indefinitely.
**Evidence:** `/tmp/wuphf-qa-shots/ui-wiki-article.png`. The disk content is intact (`cat ~/.wuphf-qa-icp/.wuphf/wiki/team/about/README.md` returns the article).
**Notes:** The article path includes `team/about/` and the loader may be choking on the directory delimiter, or the SPA is requesting the wrong API path on first render.
**Fix sketch:** Add a console-error / network-tab capture path for wiki loads; the "Loading article…" placeholder needs a timeout + retry button + error message.

### B-09 — Wiki index regen ignores articles written by the company-context seeder
**Severity:** P2.
**Repro:** After onboarding completes, `~/.wuphf/wiki/team/about/README.md` exists on disk and the wiki app shows `1 articles`, but `wiki/index/all.md` still reads `_No articles yet._`
**Evidence:** `cat ~/.wuphf-qa-icp/.wuphf/wiki/index/all.md`. The index is "auto-generated" per its own header, so a regen step is being skipped on the seed boundary.
**Fix sketch:** Call the wiki index regen after `seedFromBlueprintLocked` / `seedMinimalScratchLocked` and (separately) after every onboarding scan that writes new articles.

### B-10 — Frontend desync after scan failure: backend advances to `blueprint`, UI stays on the "Scanning…" chip
**Severity:** P0 (the user is stranded without realizing they need to reload).
**Repro:** Submit a URL → scan fails → CEO posts "Couldn't read … skipping the scan." → backend writes `"phase":"blueprint"` and a `blueprint-pick` pending suggestion → UI still shows the read-only "Scanning https://example.com…" chip in the composer area with no buttons. Hard reload fixes it.
**Evidence:** `/tmp/wuphf-qa-shots/run2-scan-t2.png` (stuck) vs. `/tmp/wuphf-qa-shots/run2-hard-reload.png` (post-reload).
**Notes:** The SSE event for `PendingSuggestion` replacement appears to land but the chat-mode composer is not swapping the chip card when the scan transitions to a terminal state.
**Fix sketch:** When a `ceo_scan_chip` terminal status (`done` / `failed`) lands, clear the form-area pinning and let the new `PendingSuggestion` (here: blueprint chip-row) replace it. Add a Playwright assertion: "after scan fails, the next phase's chips render without a reload".

### B-11 — Sidebar shows tools that are stub routes for the human (no entry surface for some, dead pages for others)
**Severity:** P1.
**Repro:**
- Most "TOOLS" labels navigate to `/#/apps/<id>` correctly (overview, console, graph, policies, calendar, skills, activity, receipts, health-check, settings).
- Two sidebar links — Wiki and Inbox — are listed under TOOLS but actually live at `/#/wiki` and `/#/inbox` (FIRST_CLASS routes). Hand-typed `/#/apps/wiki` and `/#/apps/inbox` both error to "Page not found", even though those are the labels a user would expect to type. (See `web/src/routes/routeRegistry.ts:18` — they intentionally aren't in `APP_PANEL_IDS`.)
**Fix sketch:** Either alias `/apps/wiki` → `/wiki` (and `/apps/inbox` → `/inbox`) so deep-linking by name works, or stop labeling them as Tools.

### B-12 — Boot banner advertises "4 agents" on the scratch path that seeds 1
**Severity:** P2.
**Repro:** Pick "Start from scratch" → launch → stderr says `Launching WUPHF Office web view (4 agents)... the browser is the office now.` but the office sidebar lists only CEO. The hard-coded "(4 agents)" in the boot banner does not reflect the actual roster.
**Fix sketch:** Read the roster size from the broker state before printing.

### B-13 — Agent-can't-authenticate failures look like normal CEO replies
**Severity:** P1.
**Repro:** When the runtime fails to authenticate, the broker writes a regular `From: ceo, Kind: text` message into the channel. From the user's perspective the CEO is the one saying "Not logged in", which is misleading — the CEO is an agent persona, not an authentication-error renderer.
**Fix sketch:** Render auth/runtime failures as a system error card (red/yellow band) with a "Reconfigure runtime" CTA. Keep agent messages reserved for actual agent output.

### B-14 — Onboarding form inputs lack `autocomplete="off"`, so 1Password offers to fill office name and description
**Severity:** P2.
**Repro:** Walk through onboarding step 1/2/3 with 1Password installed; the announcer reads "1Password menu is available. Press down arrow to select." each time. Not great for screen-reader users and useless for "Office name" / "Short description".
**Fix sketch:** Set `autocomplete="off"` (and `aria-autocomplete="none"`) on the wizard's text fields; the website-URL field can stay as `url` if helpful.

### B-15 — Newly filed issue detail view renders nothing in the body pane
**Severity:** P1.
**Repro:** `/#/issues/new` → fill title + details → File issue → redirect to `/#/issues/task-8` → main pane shows only the breadcrumb + theme/search action buttons. No issue title, body, owner, status, or comment composer renders. The sidebar Issues list does pick up the new entry.
**Evidence:** `/tmp/wuphf-qa-shots/ui-task-8-detail.png`; `main`'s rendered HTML contains only `channel-header`.
**Fix sketch:** `IssueDocumentRoute` needs an empty-state at minimum (title + body + "no events yet" placeholder + comment input). Today the user files an issue and sees a blank page.

### B-16 — Issue-creation form's "Assignee" placeholder says "leave blank to self-assign" — but the human is not an agent
**Severity:** P2.
**Repro:** New-issue form, Assignee input placeholder: `agent slug — leave blank to self-assign`. The actor filing the issue is the human; "self-assign" is wrong copy here.
**Fix sketch:** Replace with `agent slug — leave blank for CEO to triage`.

### B-17 — Wiki Notebooks subtab stays on "Loading article…" forever even when the per-agent notebook directories exist
**Severity:** P2.
**Repro:** `/#/wiki/notebooks` after fresh onboarding. Same loader placeholder as B-08, never resolves. The directories `wiki/agents/{ceo,planner,reviewer,executor}/notebook/` exist on disk (each with a `.gitkeep`), so the empty-state path is what should render.
**Fix sketch:** Notebooks tab needs an explicit "no entries yet — agents will write here as they work" empty state, plus the same timeout+retry treatment as B-08.

### B-18 — Bundle size: main chunk exceeds the documented app budget by ~3×
**Severity:** P2 (perf budget violation).
**Repro:** `bun run build` in `web/` reports `dist/assets/index-*.js  824.59 kB │ gzip: 242.05 kB`. CLAUDE.md "Web frontend specifics" sets the app budget at `< 300kb / 50kb` (gzipped). RichWikiEditor adds another 352 KB un-gzipped chunk.
**Fix sketch:** Code-split the hot path of the conversation surface from RichWikiEditor (it already is, but the main chunk still drags in too much). Audit `index-*.js` content via the rolldown reporter.

### B-19 — Wiki index shows "1 articles" (grammar)
**Severity:** P2.
**Repro:** Wiki app top-right counter literally reads `1 articles` (always plural). Cosmetic but trivial to fix.
**Fix sketch:** Pluralize: `{n} {n === 1 ? "article" : "articles"}`.

---

## C. Usability findings

### U-01 — User-typed answers never appear in the CEO conversation transcript
**Repro:** During the wizard, the chat shows only CEO bubbles ("Office name?", "What does Acme Postmark do?", "Got a website I can scan for context?", …). The values the human just submitted are not echoed back. This breaks the "feels like Slack" illusion the office is designed around.
**Fix sketch:** After each `POST /onboarding/answer`, append a user-side bubble to the DM transcript with the sanitized value. The transcript code already supports human messages elsewhere.

### U-02 — No visible confirmation that the website scan started
**Repro:** Submit `https://example.com` → wizard immediately re-renders the next CEO question. The "Scanning…" chip appears later (when PhaseScan fires) but on a slow connection there is a noticeable dead window. Compare to GitHub Actions / Linear, where the affordance changes the moment you submit.
**Fix sketch:** As soon as the wizard accepts the URL, show an inline "Scanning {url}…" pill in the just-answered slot, even before the chip lands.

### U-03 — CEO's first-issue affordance ("Start an issue" / "Look around first") buries the spec workflow
**Repro:** End of onboarding step 5: the two options are equally weighted. The product principle "Tell, don't ask" would say: have the CEO assume the human wants to start, with "look around first" as the secondary path.
**Fix sketch:** Promote "Start an issue" to the primary button; demote "Look around" to a link. Or auto-route to `#general` and let the human pick on their own terms.

### U-04 — The "STEP X OF 5" header is anti-magical when the LLM is supposed to be conversational
**Repro:** A linear wizard chrome around the conversational chat surface is fighting itself. If we want the CEO to feel like a colleague, the visible "STEP 3 OF 5" turns it back into a form-flow.
**Fix sketch:** Drop the step counter; or move it to a thin progress hint that only appears when the user looks idle.

### U-05 — Boot warnings ("other wuphf binaries are on PATH and may shadow this one", "gh installed but not authenticated") are noisy and irrelevant to a first-run user
**Repro:** Stderr at boot. For a fresh-install user, those lines are scary; for the developer, they are stale.
**Fix sketch:** Gate behind `--verbose`; route to `wuphf doctor` instead.

### U-06 — Theme: light is fine, but no visible affordance distinguishes "task is failing" from "task is just quiet"
**Repro:** With CEO failing every turn, the page sidebar still reads "all quiet". For a first-run user, "all quiet" feels like success when it's actually total failure.
**Fix sketch:** Surface persistent agent errors as a banner at the channel header, not just an Activity-log entry.

### U-07 — "RECENT" section in the left sidebar (Workspace / Skills / notebooks etc.) is noise for a first-run user
**Repro:** After visiting Skills and Notebooks, both appear under RECENT. For a user with one channel and four pages total, RECENT is everything they've seen — useless. (There is an in-flight worktree `feat/remove-recent-sidebar` that addresses this; flagging here to support landing it.)

### U-08 — "Inbox zero" empty-state is good; activity log empty-state ("No agents are visibly active right now.") is also good — but Notebooks/Wiki use generic "Loading article…" instead. Inconsistent voice.
**Fix sketch:** Pick a single voice for empty states across the app: "agents will write here when they work" / "you'll see X here once Y" / etc. Today three different placeholders coexist.

---

## D. Quality / hygiene

### Q-01 — `materializeScratchWikiStubs` is dead code
See B-07. Either wire it or delete it.

### Q-02 — `/onboarding/state` returns `{"error":"unauthorized"}` to an unauthenticated curl
This is fine in production but breaks the "click around in dev tools to debug" loop. Consider a `--unsafe-dev` flag that opens read-only state endpoints to localhost.

### Q-03 — Wiki article "About This Team" links to `company.md` and `owner.md` that do not exist
The README claims `- [company.md](company.md) — what this company does` and `- [owner.md](owner.md) — who is running this workspace`, but those files are not created by the scratch seed path. The auto-seeded README points at dead links from day 1.

### Q-04 — `dist/assets/index-*.js` is over the app bundle budget (B-18)
Already captured; restating for the hygiene list.

### Q-05 — Build banner says "STEP 5 OF 5 · FIRST TASK" even on the path that has no STEP 4 prompt
The numerator/denominator should at minimum be consistent. See B-05.

### Q-06 — Wiki app pluralization issue (B-19)
Trivial, but symptomatic of the absence of an empty/one/many helper across the UI.

### Q-07 — JS bundles do not split per app panel cleanly
Some app-panel chunks are small (`PoliciesApp` 3.9 KB, `ReceiptsApp` 6 KB) — good. But the main `index` chunk pulls in RichWikiEditor and others. Worth one optimization pass.

---

## E. Product improvement ideas (drawn from the workflow the user described)

These are not bugs against the current build; they are ideas the workflow demands that the surface barely supports today.

### I-01 — Make the CEO authentication state a runtime concern, not an agent concern
A first-run user does not separate "the CEO is an LLM" from "Claude Code needs to be logged in". Have a single Runtime panel that owns auth, model selection, and budget — the CEO never reports its own runtime errors. (Addresses B-04 + B-13 + B-03 together.)

### I-02 — Move the "spec drafting" loop into the Issue document itself
The user's workflow says: post in #general → CEO opens an issue → CEO asks questions inside the issue → human comments → CEO revises → human approves → execution begins. Today the only place that supports inline iteration is a DM chat; the Issue document view (B-15) does not even render its own body. Treat the Issue page as the spec-canvas the way GitHub issues + Linear documents are.

### I-03 — Make "human reads vs agent reads" visible everywhere
The user's existing memory (`wiki_agent_reads_first_class.md`) says agent reads of wiki articles count equally. The Overview/Activity surfaces today track agent activity but not "this article was read by 3 agents and the human in the past 7 days". Surface it.

### I-04 — Rich wiki: HTML-in-MD components for human articles, plain MD for skills
The workflow asks for rich images + components in wiki articles for human consumption, while skills stay as `.md`. Today `RichWikiEditor` ships at 352 KB but no current article uses HTML embeds. Define a small `<wuphf-*>` component set (callout, embed, contact-card, playbook-step) and render them in the wiki view; leave skills as plain markdown.

### I-05 — Approval cards must show what the agent saw before deciding
The workflow says: when an agent asks to call an integration, the approval card must include the full context. Define a single ApprovalCard schema with: agent identity → task ID → intent → tool args → expected effect → cost — and have one ApprovalCard renderer used in both Inbox and the agent-thread view.

### I-06 — "Notebook → review → promote to wiki" is a state machine that deserves a single UI
Today notebooks live under `agents/<slug>/notebook/`, the wiki proper is `team/`, and "reviews" is its own tab. The promotion flow the user described (agent writes → asks for promotion → CEO reviews → optionally re-routes to "update existing article" instead of "create new") is a single workflow. Build it as one component with explicit `draft → in_review → promoted` states and Slack-style review threads, instead of three side-by-side tabs.

### I-07 — Policy authorship: same approval pipeline as playbooks, but tagged
"Agent proposes policy → CEO reviews → canonicalize" is the same loop as playbooks. Reuse the same Issue-document surface, with a `kind: policy` flag. Policies App becomes a filtered view, not a separate authoring surface.

### I-08 — Onboarding website-scan should never silently fail
Three branches today: scan succeeds (no example yet), scan fails (B-06, opaque), scan skipped (works). The failed branch should still offer a path forward: "I couldn't read example.com — try another URL, paste text directly, or skip for now." Treat it as a recoverable interview step, not a terminal chip.

### I-09 — The "looking at memes" / "waiting for work" CEO presence labels are charming, but a stuck CEO should change the avatar/status to "needs attention"
Right now even after CEO has been failing for 30 seconds the status flicks between "looking at memes" and "waiting for work". The fact that the agent has tried twice and produced an error should be a first-class signal in the sidebar.

### I-10 — Single source of truth for "what apps exist" between sidebar labels and `APP_PANEL_IDS`
The sidebar TOOLS list and `routeRegistry.ts:APP_PANEL_IDS` are maintained in two different places (see B-11). Drive the sidebar from the registry so labels and routes can never drift.

### I-11 — Provider tile should be a stateful card, not a one-shot picker
Once a runtime is picked, the same card belongs in Settings as a live status (signed in / token expires in N days / monthly spend / fallback runtime), and an in-line "Switch runtime" should be possible without going back through onboarding.

---

## F. Reproducibility for future QA runs

Setup used (verbatim, so the next run can reproduce):

```bash
git fetch origin main
git worktree add .worktrees/qa-icp-walkthrough -b qa/icp-walkthrough-2026-05-20 origin/main
cd .worktrees/qa-icp-walkthrough
(cd web && bun install && bun run build)
go build -o ./wuphf ./cmd/wuphf
rm -rf ~/.wuphf-qa-icp && mkdir -p ~/.wuphf-qa-icp
HOME="$HOME/.wuphf-qa-icp" WUPHF_BROKER_PORT=7901 \
  ./wuphf --broker-port 7901 --web-port 7902 --no-open \
  > /tmp/wuphf-qa-icp.log 2>&1 &
```

Browser driven via `browser-harness`; screenshots collected under `/tmp/wuphf-qa-shots/`.

Tracking the auth-blocker (B-04): copying `~/.claude/` and `~/.claude.json` into the isolated `HOME` was not sufficient because the OAuth tokens live in the macOS keychain under a key tied to `HOME`; running `claude login` once under the isolated `HOME` would unblock further QA. We did not do that — the auth surface itself is the most interesting bug.

---

## G. Open questions for the team

1. Is the "STEP 5 OF 5" framing intentional even when blueprint = scratch? If so, the missing STEP 4 needs a placeholder line ("Team: just CEO for now") rather than a silent skip.
2. Is `materializeScratchWikiStubs` (B-07) supposed to fire today, or has the company-context seeder superseded it? The dead code should be removed or the seeder should be the one referenced in the doc comment.
3. The wiki article loader hanging on `Loading article…` (B-08, B-17) — is there an open issue for this, or is it new on `main`?
4. Was the bundle budget in CLAUDE.md (300 KB / 50 KB gzipped) intended as a hard limit or a north star? Today's `index-*.js` is well past it (B-18).

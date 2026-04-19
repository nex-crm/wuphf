# Testing the LLM Wiki

Reproducible steps to verify the `feat/llm-wiki` feature end-to-end, plus reference instructions for recording demo GIFs.

## Contents

1. [Prerequisites](#prerequisites)
2. [Automated tests](#automated-tests)
3. [Manual end-to-end test (smoke)](#manual-end-to-end-test-smoke)
4. [Live demo — scripted (Path A)](#live-demo--scripted-path-a)
5. [Live demo — real agents (Path B)](#live-demo--real-agents-path-b)
6. [Manual test checklist](#manual-test-checklist)
7. [Recording GIFs for launch](#recording-gifs-for-launch)
8. [Troubleshooting](#troubleshooting)

---

## Prerequisites

- Go 1.22+ (the repo's target)
- Node 18+ and `npm` (for the web bundle)
- `git` on your PATH (non-negotiable; the wiki IS a git repo)
- `curl`, `python3` (used by the demo script)
- Optional: `ffmpeg` if you want to convert recordings to GIFs

Everything runs locally on dev ports to avoid colliding with a prod `npx wuphf` if you have one running:

| | Binary | Broker port | Web UI port | Isolated HOME |
|---|---|---|---|---|
| Prod | `npx wuphf` | 7890 | 7891 | `~/.wuphf` |
| Dev | `./wuphf-dev` (this worktree) | 7899 | 7900 | any path you set via `HOME=` |

Never run the dev binary without `--broker-port 7899 --web-port 7900` and an overridden `HOME` — see `docs/LOCAL-DEV-PROD-ISOLATION.md` (local-only) for rationale.

---

## Automated tests

### Go

Backend tests run via standard `go test`:

```bash
cd /path/to/feat-llm-wiki

# Wiki-specific (fast, always green)
go test ./internal/team/... ./internal/teammcp/... ./internal/operations/... \
  -run "Wiki|BuildArticle|BuildCatalog|Backend|Materialize|ParseWikilink|RelPathToSlug|ExtractTitle|CountWords|UniqueAuthors|GroupFromPath|Onboarding" \
  -count=1 -timeout 120s
```

Expected: three `ok` lines, no failures.

Full backend suite (includes unrelated headless-codex tests that have pre-existing flakiness):

```bash
go test ./... -count=1 -timeout 300s
```

With `-race`:

```bash
go test ./internal/team/... -run "TestE2EWiki" -race -count=1 -timeout 120s
```

### Web (Vitest + React Testing Library)

```bash
cd web
npm install   # first time only
npm test
```

Expected: **100/100 tests pass across 25 test files.**

Coverage:

```bash
npm run test:coverage
```

Expected: >80% on `src/components/wiki/**`, `src/lib/**`, `src/api/wiki.ts`.

TypeScript strict check:

```bash
npx tsc --noEmit
```

Expected: clean (no output).

### Single command — full gate

```bash
go build ./... && \
go test ./internal/team/... ./internal/teammcp/... ./internal/operations/... -run "Wiki|BuildArticle|BuildCatalog|Backend|Materialize|ParseWikilink|RelPathToSlug|ExtractTitle|CountWords|UniqueAuthors|GroupFromPath|Onboarding" -count=1 && \
( cd web && npx tsc --noEmit && npm test && npm run build )
```

If all four steps pass, the feature is safe to ship.

---

## Manual end-to-end test (smoke)

Verifies wiki works as a user would experience it on a fresh install.

### 1. Build the binary

```bash
cd /path/to/feat-llm-wiki
( cd web && npm run build )                  # embedded web bundle
go build -o wuphf-dev ./cmd/wuphf
```

### 2. Fresh dev home

```bash
DEMO_HOME="/tmp/wuphf-demo"
rm -rf "$DEMO_HOME"
mkdir -p "$DEMO_HOME"
```

### 3. Launch

```bash
HOME="$DEMO_HOME" ./wuphf-dev \
  --broker-port 7899 --web-port 7900 \
  --memory-backend markdown
```

The browser auto-opens at `http://localhost:7900`. The onboarding wizard appears.

### 4. Complete onboarding

Either drive the UI:

1. "Open the office"
2. Fill company name + description + priority → "Choose a blueprint"
3. Pick **Niche CRM** (richest wiki schema for the demo) → "Review the team"
4. Continue past team review
5. Pick **Markdown (default)** memory backend → paste any value into `ANTHROPIC_API_KEY` → "Ready"
6. Click "Open the office without a first task"

…or complete programmatically (useful for scripting CI/demos):

```bash
TOKEN=$(curl -s http://127.0.0.1:7899/web-token | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))")

curl -s -X POST "http://127.0.0.1:7899/onboarding/progress" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"step":"identity","partial":{"company_name":"Smoke Test","description":"testing the wiki","priority":"ship"}}'

curl -s -X POST "http://127.0.0.1:7899/onboarding/complete" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"skip_task":true,"blueprint":"niche-crm","agents":["operator","planner","builder","growth","reviewer"]}'
```

### 5. Verify wiki state on disk

```bash
# Blueprint skeletons should exist
find "$DEMO_HOME/.wuphf/wiki/team" -name "*.md" | sort

# Git repo should be initialized with at least one commit
git -C "$DEMO_HOME/.wuphf/wiki" log --oneline

# Index should auto-regenerate
cat "$DEMO_HOME/.wuphf/wiki/index/all.md"
```

Expected:
- 5 skeleton markdown files under `team/` (customers/onboarding, inbox/raw-feedback, playbooks/renewal, product/roadmap-rationale, decisions/product-log for niche-crm)
- git log shows `wuphf: init wiki` commit plus a skeleton-materialization commit
- `index/all.md` lists the articles grouped by dir

### 6. Verify wiki UI

1. In the running app, click **📖 Wiki** in the left sidebar APPS panel.
2. Confirm the catalog renders 5+ articles across 4-5 group cards (PLAYBOOKS, DECISIONS, INBOX, CUSTOMERS, PRODUCT).
3. Click any article. Confirm:
   - Breadcrumb shows `Team Wiki › group › title`
   - Title renders in Fraunces (warm serif, not Inter)
   - Amber status banner at top shows "Live: … is editing" (pulsing dot)
   - Body renders in Source Serif 4 (serif)
   - Pixel avatar in the byline matches the agent slug
   - Right rail shows TOC + page stats + cite this page + referenced by panels
   - Bottom edit-log footer shows the most recent commit in amber

### 7. Clean shutdown

```bash
# Back in the launch terminal, Ctrl-C
# Or from another terminal:
lsof -ti:7899 | xargs kill
```

Smoke test passes if all steps above complete without errors.

---

## Live demo — scripted (Path A)

Fastest and most reliable. Good for Reddit GIFs, quick pitches, regression checks.

**Terminal 1 — keep WUPHF running** (same as the smoke test above).

**Terminal 2 — run the demo script:**

```bash
cd /path/to/feat-llm-wiki
./scripts/demo-wiki-live.sh
```

The script:
1. Fetches the broker token from `/web-token`
2. Fires 8 `team_wiki_write` requests across 5 different agent slugs (`operator`, `planner`, `growth`, `reviewer`, `builder`)
3. Spaces them 3 seconds apart so the UI animation is visible
4. Mixes `create`, `replace`, and `append_section` modes
5. Cross-links articles via `[[wikilinks]]` so backlinks populate in real time

**Tuning flags:**

| Env var | Default | What it does |
|---|---|---|
| `BROKER` | `http://127.0.0.1:7899` | Point at prod ports, remote broker, etc. |
| `DELAY` | `3` | Seconds between writes (set to `5` for talks, `1` for fast GIFs) |
| `DRY_RUN` | `0` | Set to `1` to see the plan without writing |

**Expected UI behavior:**

- The rolling edit-log at the bottom of the wiki pulses amber as each commit lands
- The "X articles" stat ticks up from 5 → 13
- New cards appear on the catalog grid (CUSTOMERS, PEOPLE)
- Navigate to `/#/wiki/team/customers/customer-x.md` before the demo and watch the "Referenced by" panel fill in live as later articles link to it
- `git -C ~/.wuphf/wiki log --oneline` after the demo shows all 9 commits with per-agent authorship

**Reproducibility check** — run the demo twice against the same dev home:
- First run: all writes succeed
- Second run: the `create` writes fail with "already exists" (expected — this is the idempotency guarantee); `replace` and `append_section` writes succeed

---

## Live demo — real agents (Path B)

Slower but more demonstrably "real." Good for a YouTube walkthrough or the Karpathy-style launch pitch where "these are real AI agents" is load-bearing.

### 1. Launch WUPHF as above

Onboarding must complete. Do NOT use `--unsafe` — let the agents go through the normal permission flow.

### 2. Ensure an API key is configured

Either enter one during onboarding, or set it in `.env` before launch:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

### 3. Send a prompt to the office

In the `#general` channel, paste:

```
Welcome to the team. We just signed two mid-market logistics customers
(Customer X in Cincinnati, Meridian Freight in Columbus).

I want each of you to contribute one wiki article based on your role.

@operator — write team/people/sarah-chen.md. Sarah is the Customer X
            champion, Director of Ops.
@planner  — update team/playbooks/churn-prevention.md with the new
            dispatcher-burnout signal we noticed across both accounts.
@growth   — write team/decisions/2026-q2-pricing.md. We are adding
            read-only seats at 30% of full seat price.
@reviewer — append an "Update — 2026-04-20" section to
            team/customers/customer-x.md summarizing what you see.
@builder  — write team/decisions/wiki-as-default.md. Explain why new
            installs default to markdown wiki.

Each of you commits one article in parallel. Use [[wikilinks]] to
reference each other's pages so the backlinks panel populates.
```

### 4. Watch

The agents will work through the `team_wiki_write` MCP tool. Expected:
- Each agent's pixel avatar appears in the edit-log footer as it commits
- Articles appear in the catalog with genuine prose (not scripted)
- Cross-references render as dashed-blue wikilinks; if an agent references an article that doesn't exist yet, it renders red + marker

**Cost:** ~$0.15-0.30 in Anthropic credits (5 agents × 1-2 turns each on Sonnet)
**Time:** 3-7 minutes depending on how verbose the agents get

---

## Manual test checklist

Walk through these by hand after any change that touches wiki code:

### Catalog view (`/wiki`)

- [ ] Correct article count in header
- [ ] Thematic group cards render (at least PLAYBOOKS + DECISIONS on a fresh niche-crm install)
- [ ] Blueprint-specific groups show (e.g. CUSTOMERS for niche-crm, VIDEOS for youtube-factory)
- [ ] Article titles visible in full, not truncated
- [ ] Pixel avatars match agent slugs (Operator ≠ Planner visually)
- [ ] Clicking an article navigates to `/wiki/<path>`
- [ ] Edit-log footer shows recent commits
- [ ] Most recent edit-log entry pulses amber

### Article view (`/wiki/<path>`)

- [ ] Breadcrumb reflects the path (`Team Wiki › group › title`)
- [ ] Title in Fraunces, large, serif
- [ ] Italic "From Team Wiki, your team's encyclopedia" strapline under title
- [ ] Thick horizontal divider below strapline
- [ ] Amber status banner at top with pulsing dot
- [ ] Hat-bar tabs (Article / Talk / History / Raw markdown)
- [ ] Byline shows pixel avatar + agent display name (Title Case, not UPPERCASE) + amber timestamp
- [ ] Body in Source Serif 4, 18px, generous line-height
- [ ] Wikilinks render blue with dashed underline
- [ ] Broken wikilinks render red with trailing ⚬ marker
- [ ] Section headings numbered (1, 1.1, 1.2, 2, …)
- [ ] Right rail: Contents (TOC), Page stats, Cite this page, Referenced by
- [ ] "Referenced by" populates with articles that link TO this one
- [ ] Sources section at bottom shows git commits with per-agent avatars
- [ ] Page footer shows "last edited by" + actions (view history, cite, download, clone)

### Backend / MCP

- [ ] `curl http://127.0.0.1:7899/web-token` returns a token
- [ ] `GET /wiki/catalog` with token returns `{articles: [...]}` with real data
- [ ] `GET /wiki/article?path=team/customers/customer-x.md` returns `{path, title, content, last_edited_by, revisions, contributors, backlinks, ...}`
- [ ] `GET /wiki/read?path=...` returns raw markdown bytes
- [ ] `GET /wiki/list` returns raw markdown index (not JSON)
- [ ] `POST /wiki/write` with a body creates/replaces/appends + commits
- [ ] Write payload saturation (64+ simultaneous) returns "wiki queue saturated"

### Crash recovery

- [ ] Launch wuphf, write one article, `kill -9` the process mid-session
- [ ] Relaunch. Check `git log` — if there was uncommitted work, a `wuphf-recovery` author commit should appear

### Backend switch (IRON regression)

- [ ] Launch with `--memory-backend markdown` — MCP subprocess has `team_wiki_*` tools only
- [ ] Launch with `--memory-backend nex` — MCP subprocess has legacy `team_memory_*` tools only
- [ ] Launch with `--memory-backend none` — no shared memory tools
- [ ] Two tool sets never coexist on the same subprocess

---

## Recording GIFs for launch

### Quick GIF (30-45s) for Reddit or Twitter

1. Launch WUPHF + complete onboarding as per smoke test
2. Navigate to `http://localhost:7900/#/wiki`
3. Start screen recording (macOS: `Cmd-Shift-5`; Linux: `peek` or `kazam`)
4. Run `./scripts/demo-wiki-live.sh` with `DELAY=2`
5. Stop recording ~25 seconds after demo finishes
6. Crop to the wiki area (exclude the rest of the WUPHF chrome if it's too busy)
7. Convert to GIF:

```bash
ffmpeg -i recording.mov \
  -vf "fps=15,scale=1200:-1:flags=lanczos" \
  -loop 0 \
  demo.gif
```

Target: <2 MB. For Reddit, 4 MB hard cap.

### Longer demo (60-90s) for YouTube / launch post

1. Navigate to `/wiki/team/customers/customer-x.md` (article view, not catalog)
2. Start recording
3. Run demo script — the "Referenced by" panel on Customer X fills live
4. After the script finishes, click back to the catalog and show the populated groups
5. Click into `team/playbooks/churn-prevention.md` — show the wikilinks back to Customer X and Meridian Freight
6. Drop to a terminal, run `cat ~/.wuphf/wiki/team/customers/customer-x.md` — real markdown on disk
7. Run `git -C ~/.wuphf/wiki log --oneline` — show per-agent authorship
8. Stop recording

Export as MP4, 1080p. The "file on disk + git log" reveal at the end is the money shot for the Karpathy-wiki pitch.

---

## Troubleshooting

**Problem:** `./wuphf-dev: command not found`
**Fix:** You haven't built yet. Run `go build -o wuphf-dev ./cmd/wuphf`.

**Problem:** Browser opens to a splash screen, never reaches the wiki
**Fix:** Onboarding wizard is active. Complete it (company name + blueprint + memory backend + API key). For scripting, use the programmatic `POST /onboarding/complete` sequence above.

**Problem:** Wiki tab missing from the left sidebar
**Fix:** Stale web bundle. Rebuild: `( cd web && npm run build ) && go build -o wuphf-dev ./cmd/wuphf`, then relaunch.

**Problem:** Demo script prints `broker not reachable at http://127.0.0.1:7899`
**Fix:** wuphf-dev isn't running, or it bound to different ports. Check with `lsof -i :7899`. If empty, relaunch. If your broker is on a different port, set `BROKER=http://127.0.0.1:<port>`.

**Problem:** Catalog shows fake names (Sarah Chen / David Kim / Nazz) I never wrote
**Fix:** `fetchCatalog()` is hitting its fixture fallback. That means `/wiki/catalog` returned an error. Check:
```bash
TOKEN=$(curl -s http://127.0.0.1:7899/web-token | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))")
curl -s "http://127.0.0.1:7899/wiki/catalog" -H "Authorization: Bearer $TOKEN" | head -20
```
If this returns 503 "wiki backend is not active", the broker didn't start the wiki worker — launch with `--memory-backend markdown`.

**Problem:** `fatal: cannot create directory` when launching
**Fix:** Your dev HOME points into a read-only path or inside a git worktree with conflicting untracked files. Use a fresh `/tmp/wuphf-demo` as the HOME.

**Problem:** Live edit-log footer shows fixture entries, not my writes
**Fix:** Known follow-up — Lane C's SSE subscription isn't wired to the real `/wiki/stream` endpoint yet; the footer uses a synthetic replay. Real writes DO commit (verified via `git log`); only the footer display is stale. Refresh the page to see updated counts in the catalog and correct per-article backlinks.

**Problem:** Some pre-existing Go tests fail with git worktree errors
**Fix:** `TestEnqueueHeadlessCodexTurnBypassesLeadHoldForReviewReadyTask` and a few others have pre-existing test-environment races when the whole suite runs in a git worktree. Run the wiki-specific subset (see "Automated tests" section) to confirm wiki code is green.

---

## What's NOT tested here (intentional gaps, v1.1 items)

- **Mobile responsive** — desktop-first per `DESIGN-WIKI.md`. Three-column layout at <768px is currently broken; resizing breakpoints are a v1.1 task.
- **LLM merge-resolver** — v1 uses serialized writes, so concurrent-write conflicts are impossible. The merge-resolver code path has no test coverage because it doesn't exist yet.
- **Per-agent wikis** — cut from v1. Test coverage for `agents/{slug}/` paths is intentionally absent.
- **SSE live-update** of the edit-log footer from real broker events — known gap; see Troubleshooting above.
- **Playwright E2E** — not wired. Current E2E lives in `internal/team/wiki_e2e_test.go` as Go integration tests over the broker HTTP surface.

---

Last reviewed: 2026-04-20 against commit `0523cfde`.

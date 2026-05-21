# ICP Walkthrough Runbook

The reproducible test plan we run against `main` to validate the 17-step
"ideal workflow" described by the founder (file emails task → CEO drafts spec
→ approval → fan-out → integration approvals → notebooks → artifacts →
playbook promotion → skills). When findings surface, log them in a dated
report under `docs/qa/<date>-icp-walkthrough.md` and link the issues back to
this runbook.

Re-run before each release candidate, and after any change to onboarding,
the wizard, the CEO message pipeline, the wiki seeder, or routing.

## 0. Setup (clean room, isolated home, alt ports)

```bash
# Fresh worktree from latest main.
git fetch origin main
git worktree add .worktrees/qa-icp-walkthrough -b qa/icp-walkthrough-$(date +%Y-%m-%d) origin/main
cd .worktrees/qa-icp-walkthrough

# Web bundle must exist before the Go binary embeds it.
(cd web && bun install && bun run build)
go build -o ./wuphf ./cmd/wuphf

# Isolated home so the run does not touch the user's real ~/.wuphf
# or the wuphf-dev home. Ports 7901/7902 stay free of the usual 7890/7891.
rm -rf ~/.wuphf-qa-icp && mkdir -p ~/.wuphf-qa-icp

# If you need a logged-in Claude Code under this HOME (else expect "Not logged in"):
# HOME=~/.wuphf-qa-icp claude login   # interactive; do this once

HOME="$HOME/.wuphf-qa-icp" WUPHF_BROKER_PORT=7901 \
  ./wuphf --broker-port 7901 --web-port 7902 --no-open \
  > /tmp/wuphf-qa-icp.log 2>&1 &
echo "broker pid=$!"
```

Browser surface: open `http://127.0.0.1:7902` in your real Chrome (or drive
via the `browser-harness` agent skill). Screenshots go in `/tmp/wuphf-qa-shots/`.

Tear-down: `kill <pid>; rm -rf ~/.wuphf-qa-icp; git worktree remove .worktrees/qa-icp-walkthrough`.

## 1. Walkthrough

For every step, record: did it complete? did the UI match the expected
state? did the disk-side artifacts get written? Capture a screenshot per
state-change.

### Step 1 — Pick a provider
- Land on the pre-pick screen.
- Verify each detected runtime tile has a true detection signal (version +
  signed-in status, not just "binary on PATH").
- Pick Claude Code (or whichever is signed in).
- **Expect:** tile shows version *and* "signed in"; if not signed in, an
  inline action to sign in (no advancement to the wizard).

### Step 2 — Onboarding chat wizard
- Submit company name.
- Submit company description.
- Submit company website URL.
- **Expect:** the wizard's chat transcript echoes each answer the human
  submitted (not only the CEO's questions).
- **Expect:** the website phase posts an inline "Scanning {url}…" pill
  immediately on submit; that pill resolves to "Wiki updated ✓" with one
  bullet per article written, OR to a recoverable failure card with a
  specific reason ("not enough text", "timed out", etc.).
- **Expect:** the wizard step counter is consistent — no jumps from
  "STEP 3 OF 5" to "STEP 5 OF 5" with a silent skip in between.
- Pick "Start from scratch".
- Land in the office at `/#/channels/general`.
- **Expect on disk** (`HOME=~/.wuphf-qa-icp`):
  - `~/.wuphf/wiki/team/about/README.md` exists, links resolve.
  - `~/.wuphf/wiki/index/all.md` lists every scratch-seeded article.
  - `~/.wuphf/onboarded.json` `form_answers` includes `company_name`,
    `description`, `website_url`.

### Step 3 — Post the email-drafting directive to #general
- Compose this message verbatim in #general:
  ```
  @ceo please draft email replies for all emails that came in this week.
  For every contact, extract any context from the email and create a wiki
  article for that person, plus a reply playbook on the wiki. Output a
  report listing the drafts, the wiki articles, and the playbook.
  ```
- **Expect:** CEO does NOT reply as a chat bubble with a runtime error.
  Runtime/auth failures surface as a system error card with a "sign in" CTA.
- **Expect:** CEO opens a NEW issue (sidebar Issues list grows by one) and
  links to it in the channel.

### Step 4 — CEO interviews you inside the issue
- Open the new issue document.
- **Expect:** the issue page renders title, body, owner, status, comment
  composer. (Today's `IssueDocumentRoute` renders an empty body — fix
  before re-running.)
- CEO posts questions inside the issue document.
- Answer one of them inline.
- **Expect:** the CEO revises the spec (visible diff or new revision in
  the document timeline) and posts a new approval request to Inbox.

### Step 5 — Approve the spec
- Open Inbox → Decisions tab.
- **Expect:** an approval card with: the issue link, the diff of what the
  CEO is asking you to approve, the agents it will spin up, the
  integrations it expects to touch.
- Click Approve.
- **Expect:** the issue's status changes to `approved`; the agent fan-out
  starts.

### Step 6 — Agents fan out, ask for integration approvals
- Watch #general (and the issue's thread) for the spawned agents.
- The first time an agent needs an integration (e.g. Gmail), it should
  post an approval card with: agent identity, the exact tool args, the
  expected effect, the cost estimate.
- **Expect:** approving once does NOT pre-approve future calls to a
  different tool / argument set — scope is per-call.

### Step 7 — Each agent has a notebook
- Open Wiki → Notebooks.
- **Expect:** one notebook per active agent, populated with the agent's
  notes as it works (not "Loading article…" forever).

### Step 8 — Final artifacts
- When the run finishes, the CEO posts a single artifact report linking:
  - every email draft (with deep link / preview),
  - every wiki person-article created (per contact),
  - the reply-playbook article.
- **Expect:** the report is a real artifact (Artifacts app shows it),
  not a wall of CEO chat bubbles.

### Step 9 — Playbook promotion to canonical wiki
- The reply playbook starts as a notebook entry on the authoring agent.
- The agent asks to promote it to the team wiki.
- CEO reviews; if changes needed, CEO can either request changes or, if
  an existing canonical article should be updated instead, redirect the
  request from "create new" to "modify existing".
- **Expect:** humans see the review status in Wiki → Reviews with the
  explicit verdict (approved / changes-requested) and a diff.

### Step 10 — Canonical playbooks become skills
- Once a playbook is canonical, the authoring agent can promote it to a
  skill it owns.
- **Expect:** the skill is owned by that agent only; other agents can
  *see* it (via #15) but cannot run it.

### Step 11 — Agents grounded in notebooks + wiki + skill awareness
- On the next task, the agent visibly cites its notebook + wiki articles
  in its plan / reasoning.
- **Expect:** the read counter on the wiki article goes up (both human
  reads and agent reads, displayed separately).

### Step 12 — Rich wiki articles
- Pick a person-article the run produced.
- **Expect:** rich rendering (avatar, contact card component, email
  snippets as embeds) — not plain markdown.

### Step 13 — Skills in the wiki
- Open Wiki → Skills (filter / section).
- **Expect:** each agent's skills listed as editable `.md` files alongside
  the rich articles.

### Step 14 — Skill scoping
- Open Agent A's surface. The skills list shows only A's own skills.
- **Expect:** trying to invoke B's skill from A's tools returns "not
  available — owned by @B" and offers to ask @B.

### Step 15 — Cross-agent visibility
- Open the Office Overview.
- **Expect:** a graph or list showing each agent + the skills/tasks they
  own; agents can read this surface to decide who to delegate to.

### Step 16 — Self-heal on agent failure
- Provoke a failure: deny an integration approval mid-run.
- **Expect:** the agent posts a self-heal request to CEO with what failed
  and why; CEO routes to a relevant peer (or asks the human).

### Step 17 — Policy authorship
- During the run an agent proposes a policy (e.g. "always confirm contact
  preferred-name before drafting"). It opens a Policy issue.
- CEO reviews; on approval the policy lands in the Policies app AND as a
  markdown file under `wiki/team/policies/`.
- **Expect:** the policy contributes back to every agent's context on the
  next run.

## 2. Verification cheatsheet

Quick post-run sanity checks (each should pass with no errors):

```bash
# State file has every answer that was submitted.
jq .form_answers ~/.wuphf-qa-icp/.wuphf/onboarded.json

# Wiki has its seed stubs and they actually exist on disk.
ls -la ~/.wuphf-qa-icp/.wuphf/wiki/team/about/
test -s ~/.wuphf-qa-icp/.wuphf/wiki/index/all.md && grep -c '\.md' ~/.wuphf-qa-icp/.wuphf/wiki/index/all.md

# Per-agent notebook directories exist for every seeded agent.
find ~/.wuphf-qa-icp/.wuphf/wiki/agents -name notebook -type d

# Broker did not log a panic or scan error you didn't see in the UI.
grep -iE 'panic|fatal|onboarding scan|wiki' /tmp/wuphf-qa-icp.log
```

## 3. Filing findings

Each finding goes into the QA report for the run date
(`docs/qa/<date>-icp-walkthrough.md`) with: severity (P0/P1/P2), repro
steps, evidence path or screenshot, and a fix sketch. From that report,
file or update GitHub issues — prefer grouping where two findings share a
fix, and link every issue back to the dated report.

## 4. Known starting state (2026-05-20)

This runbook was authored alongside `docs/qa/2026-05-20-icp-walkthrough.md`,
which records the first full pass and the 45 findings that came out of it.
Re-runs should compare against that report and the issues it spawned.

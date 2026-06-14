# Core-Loop ICP Live Evaluation — Re-run Protocol (v2)

**Status:** Re-run protocol. Supersedes `sota-uplift-icp-eval.md` (the 2026-06-10 morning run that graded **5/10**; its observation log and grading report live in `.icp-eval/`). This run grades the rebuilt system **against the 9-step core loop** (`core-loop.md`) — every journey walks the loop's stages explicitly, and the grader scores each stage's delivery.
**Personas:** Maya (PRIMARY — RevOps operator, Series B B2B SaaS; CRO is her secondary buyer, audit-walkability is purchase-critical) runs Journeys 1–2. Sam (secondary — technical founder, artifact + definition-of-done lens) runs Journey 3.
**Method:** runner drives the real web UI in a browser as a human, reads every screen, records raw timestamped observations + screenshots, never grades. A separate grader agent scores afterward.

## The rubric: the 9 loop steps (grader scores each, per journey, 1–5 + evidence)

| # | Loop step | What "delivered" looks like to the human |
|---|---|---|
| 1 | Create a task | Composer → task exists, visible, immediately oriented |
| 2 | Define the task clearly | CEO infers goal/deliverables+format/success criteria; ONE batched interview only for genuine gaps; a structured Definition appears on the task (rail + packets) — no spec ceremony, no plan gate |
| 3 | Gather context + tool access with human help | Interview lands in task chat AND Inbox; access needs asked up front; answers absorbed into the work |
| 4 | Spin up the team | Owner assigned, subtasks where warranted, dependency edges visible and truthful |
| 5 | Agents execute with focused context | Pre-task notebook note exists (Definition + retrieved context); turn activity shows **context: learning:X, wiki:Y** lines; agents cite what they used; no keyhole amnesia between turns |
| 6 | Deliver an artifact | Task cannot claim done without a wiki artifact; done-post in chat + Inbox notice with the artifact LINK (readable, not [REDACTED], not buried); Verified badge state honest |
| 7 | Deterministic learning hook | After done: entity facts in the graph; entity wiki articles exist — cited (footnotes→tasks/artifacts), [[linked]], infobox; playbook draft appears for repeatable work; updates not duplicates |
| 8 | Skills & policies activated for relevant agents | Playbook compiles to a skill + atomic policies assigned to the right agents (visible in Skills/Policies surfaces); human chat feedback can mint a policy instantly |
| 9 | Repeat with retrieval + dedupe | The NEXT similar task visibly uses accumulated knowledge (named names, playbook steps), and the system UPDATES existing articles/playbooks instead of duplicating |

Secondary dimensions per step (same as v1, kept): Speed, Legibility, Trust, Control, Feel.

---

## Journey 1 (Maya) — first hour: renewal motion through the full loop

1. Fresh office, onboarding (AI RevOps pack), land in office. [steps 0 prep — note first-paint honesty: demo seeds are gone]
2. Ask verbatim: *"We have three renewals coming up: Acme Corp (Q3, $48k, champion left in May), Brightline (Q3, $22k, usage up 40%), and Corti Labs (Q4, $61k, two unresolved support escalations). Draft a tailored renewal email for each, capture a per-account brief on the wiki, and write a renewal-outreach playbook we can reuse every quarter."* [steps 1–2: watch the define flow — Definition on the task? batched interview only for gaps?]
3. Answer interviews (champion name: "Dana Whitfield, VP Revenue Operations"; data source: wiki for now). [step 3 — chat AND Inbox?]
4. Watch staffing + execution. [steps 4–5 — owner/subtasks; pre-task notebook note; context-used lines in activity; honest progress (no joke-only waits)]
5. Drive to done. [step 6 — artifact gate: does the office deliver the emails+briefs+playbook AS wiki artifacts with links in the done-post? Can Maya READ everything from the approval surfaces — the v1 run's [REDACTED] disasters must be gone]
6. Inspect knowledge. [step 7 — Acme/Brightline/Corti entity articles: infobox, citations, mutual links; playbook draft exists; B5 wiki: does it read like Wikipedia — search-first, TOC, infobox, blue/red links, references?]
7. Check Skills & Policies. [step 8 — compiled skill from the playbook (trigger the compile if interval hasn't fired — note whether Maya CAN); policies assigned; give chat feedback "never email renewals on Fridays" → policy appears instantly]
8. Standing automation: *"Every Monday at 9am, check which renewals are within 60 days and post a risk summary in #general."* [is it visible/editable in Scheduled Tasks now?]

## Journey 2 (Maya) — does the office remember? (same workspace)

1. Ask: *"Using our account briefs and the renewal playbook, prepare a QBR one-pager for Acme Corp. Then as a separate task that depends on it, draft the exec-sponsor email pitching the QBR meeting — it must reference the one-pager's key points, not repeat them."* [steps 1–5 again, faster now]
2. Memory check: Dana Whitfield referenced unprompted WITH source citation. [step 9]
3. Coordination check: email task blocked on one-pager; carries its content. [step 4/9]
4. Update-not-duplicate: Acme's entity article UPDATED (same file, new facts/citations), playbook gains a worked example — no duplicates. [steps 7/9]
5. Fleet view + CRO walk: board/dashboard counts consistent? Audit log attribution (still all "Human"? — known open issue, grade honestly). [secondary]
6. Stay-or-churn verdict material.

## Journey 3 (Sam) — ship something real with a definition of done

1. Ask: *"Build a single-file landing page for 'MeetingMind' (an AI meeting-notes tool): hero with tagline, 3 feature cards, an email-capture form. Definition of done: a file landing/index.html exists and contains the form. Don't tell me it's done unless that check passes."* [step 2: does the CEO encode the DoD as machine verification THIS time? v1 run: it didn't, and the task fake-done'd in 40s]
2. Watch execution: stalls distinguishable from work; reopen-and-complain must re-engage (v1: dead zone). [steps 5–6]
3. Done with proof: Verified badge flips green with readable check output; the file exists on disk; artifact in the wiki. [step 6]
4. Bounce test: "make the tagline punchier" — revision without re-explaining. [step 9]

---

## Grader contract (separate agent; the runner never grades)

Inputs: this spec, the raw observation log, screenshots, AND the v1 baseline artifacts (`.icp-eval/observations.md` + grading report, grade 5/10).
Outputs:
1. **Loop-step scorecard**: 9 rows × journeys — each step 1–5 with evidence citations; "delivered / partially / broken" verdict per step.
2. **Delta table vs v1**: every v1 disappointment point (redacted pointers, dead Accept, fake-done, plan-gate confusion, count drift, joke-only status, audit attribution, meter lag…) → FIXED / IMPROVED / UNCHANGED / REGRESSED, with evidence.
3. Happy/disappointment points (new ones found this run).
4. Persona verdicts: Maya keep/churn, Sam keep/churn, CRO renewal approval — each grounded.
5. **Overall 0–10 with the v1 5/10 as baseline**, plus the top 3 remaining fixes.
Rules: evidence-only, no benefit of the doubt, classify negatives as BUG / DESIGN FLAW / MISSING FEATURE.

## Protocol notes

- Fresh scratch runtime home (WUPHF_RUNTIME_HOME isolation), source build of the branch, dev ports 79xx (never 7890/7891), real claude-code provider, real LLM turns.
- Durable artifact paths (NOT /tmp): `.icp-eval/v2/` for log + shots.
- Waits recorded in wall-clock; dead-ends recorded as what the persona would do before any runner workaround.
- The interval skill-compile may need manual triggering (cron is 30m) — the runner may trigger it via the UI/endpoint and MUST record whether a real user could discover how.

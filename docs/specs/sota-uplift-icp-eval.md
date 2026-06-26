# ICP Live Evaluation — Three Full Journeys (Sam)

**Status:** Evaluation protocol — run live against `feat/sota-uplift-phase0`, observed as a human, graded by an independent sub-agent.
**Personas (per founder direction 2026-06-10: Maya primary, Sam secondary):**
- **Maya (PRIMARY, per persona memory)** — RevOps operator at a Series B B2B SaaS. Owns CRM hygiene, lead routing, forecasting; runs renewal motions, pipeline cleanup, sales enablement. Operates a fleet of agents; does not write code. Her secondary buyer is the CRO, who approves renewal after walking the audit trail — so legibility of WHO did WHAT and WHY is purchase-critical, not nice-to-have. Natural pack: revops. Journeys 1–2 are hers.
- **Sam (secondary)** — founder of a 3-person startup; wants a team that takes initiative and produces visible artifacts, approves the right thing in 30 seconds. Journey 3 is his (the buildable-artifact + definition-of-done path).

Both are actively deciding whether to keep this software — every step is also an evaluation of whether WUPHF earns a place in their stack.

**Sam's scoring lens at every step (the grader scores each step on these):**
- **Speed** — how long did I wait, and did the system tell me why?
- **Legibility** — did I understand what was happening without insider knowledge?
- **Trust** — when it said something was done/known/sent, did I believe it? Was there proof?
- **Control** — could I steer, veto, or correct without fighting the system?
- **Feel** — moments of delight vs. moments of jank/confusion (copy, layout, dead ends, jargon).

The runner records raw observations only (what was on screen, what was clicked, what was read, waits, reactions). The grader — a separate agent — assigns scores and the keep/churn verdict.

---

## Journey 1 (Maya) — "First hour": npx → first trustworthy renewal motion

Maya heard about WUPHF from a founder friend. She gives it one hour to prove it can run a real piece of her renewal motion.

1. **Boot & onboarding.** Launch a fresh office (clean workspace). Go through whatever first-run flow appears, reading every screen like a stranger: does he know what he's setting up, why a provider is needed, what the packs mean? Pick the default/starter path.
2. **First paint.** Land in the office, choosing the RevOps team/pack if onboarding offers one (Maya's natural fit). Does the office feel staffed and real, or staged? Real vs. seeded messages distinguishable? Obvious where to type?
3. **First real ask.** In the CEO chat, type exactly:
   > "We have three renewals coming up: Acme Corp (Q3, $48k, champion left in May), Brightline (Q3, $22k, usage up 40%), and Corti Labs (Q4, $61k, two unresolved support escalations). Draft a tailored renewal email for each, capture a per-account brief on the wiki, and write a renewal-outreach playbook we can reuse every quarter."
4. **Watch the machine think.** Does the CEO acknowledge fast? Does it ask a smart question or just run? Does an Issue/task appear somewhere Sam can find? Is the spec it wrote faithful to the ask?
5. **Approval moment.** If an approval/plan gate appears: is it clear what he's approving, and why this needs him?
6. **Work happens.** Watch agents work: can Sam tell who is doing what right now? Are status signals honest (not "reviewing work packet" forever)? Does anything visibly collaborate?
7. **The deliverable.** When the office says done: where is the one-pager? Is it readable, good, and findable without help? Did the wiki actually get competitor articles? Is "done" marked verified or unverified — and does Sam understand the difference (VerificationBadge)?
8. **Standing automation.** Maya asks: "Every Monday, check which renewals are within 60 days and post a risk summary in #general." Does the office create a visible scheduled/recurring task she can find and trust?
9. **The hour verdict.** Could Maya forward an artifact to her VP saying "my AI office drafted these"?

## Journey 2 (Maya) — "Does the office remember?": compounding + fleet coordination

A few days later, same workspace. The moat test through an operator's eyes: is week-2 output visibly informed by week-1 knowledge, and can she run dependent work as a fleet?

1. **The dependent chain.** In CEO chat:
   > "Using our account briefs and the renewal playbook, prepare a QBR one-pager for Acme Corp. Then, as a separate task that depends on it, draft the exec-sponsor email that pitches the QBR meeting — it must reference the one-pager's key points, not repeat them."
2. **Memory check.** Does the QBR one-pager actually use Journey 1's wiki briefs (champion left in May, etc.) without Maya pasting anything?
3. **Coordination check.** Does the email task wait for the one-pager, and does it visibly build on it (upstream handoff)? Or disconnected outputs?
4. **Knowledge legibility.** Maya goes looking for "what does my office know now?" — wiki, notebooks, anywhere. Can she find and trust the accumulated account knowledge? Canonical vs. draft clear?
5. **Interrupt & steer.** Mid-task: "keep the one-pager to a single page, lead with the usage data." Absorbed or lost?
6. **Fleet view.** Maya checks: what is running right now, what is scheduled (her Monday check from J1), what needs her (inbox)? Can she answer all three in under a minute?
6b. **The CRO walk.** Maya imagines showing her CRO "here's what the office did and why" — is there an audit-walkable trail (who did what, what was approved, what proof exists) she'd be comfortable presenting?
7. **The stay-or-churn moment.** After two journeys: does this workspace feel like a compounding asset she'd protect, or disposable chat logs?

## Journey 3 (Sam) — "Ship something real": a buildable artifact with a definition of done

Sam's run (separate lens, same workspace mechanics): can the office produce a working artifact, and does "done" mean anything?

1. **The ask with acceptance criteria.** In CEO chat:
   > "Build a single-file landing page for 'MeetingMind' (an AI meeting-notes tool): hero with tagline, 3 feature cards, an email-capture form. Definition of done: a file landing/index.html exists and contains the form. Don't tell me it's done unless that check passes."
2. **Scoping.** Does the CEO turn the acceptance criterion into a machine check (verification on the task)? Is the definition of done visible in the task detail rail?
3. **Plan gate.** If plan-first fires: is the plan real or filler? Approve.
4. **Execution.** Is worktree progress legible? Do stalls look different from work?
5. **Done with proof.** Verification badge with readable proof; file actually exists in the worktree.
6. **The bounce test (control).** "Make the tagline punchier" — does the revision loop work without re-explaining?

---

## Protocol

- Fresh scratch workspace (isolated HOME), source build of the branch, dev ports (79xx — never 7890/7891), real `claude-code` provider, real LLM turns.
- Runner drives the actual web UI in a browser, reads every screen, and appends raw timestamped observations (+ screenshots) to the run log. No scoring, no benefit-of-the-doubt: if a thing is confusing, the log says so in Sam's voice.
- Time-boxing honesty: waits are recorded in wall-clock minutes. If a step dead-ends, the log records what Sam would have done (given up / asked support / poked around) before the runner works around it.
- After all three journeys, a **separate grader sub-agent** receives: this spec, the full observation log, and screenshots. It scores every step (Speed/Legibility/Trust/Control/Feel, 1–5), lists happy points and disappointment points, and renders the verdict: does Sam keep WUPHF? It also separates "design flaw" from "missing feature" from "bug".

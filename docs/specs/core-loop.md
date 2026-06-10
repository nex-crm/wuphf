# Spec — The Core Loop (v4): Subtraction-First Rebuild

**Status:** ACTIVE — supersedes `sota-uplift.md` phasing (its shipped substrate — verification gate, context assembler, turn journal, auto-distill, eval harness — is the foundation this builds on). Supersedes all spec/plan-mode/skill-proposal surfaces.
**Author:** Najmuzzaman (directive) + Claude (decisions), 2026-06-10
**Doctrine:** Everything that does not support the core loop is waste. Remove those surfaces from FE and BE. The system stays super focused on delivering this one loop excellently.

▶ **RESUME HERE:** Phase R (removals) not started. Execution order: R1 redactor → R2 spec surface → R3 plan mode → R4 office-hours intake → R5 skills-from-playbooks-only → B-phases. Read "Decisions" before touching anything.

---

## The Core Loop (canonical, verbatim intent)

1. **Create a task.**
2. **Understand** the goal, deliverables (and deliverable format), and success criteria; get access to needed tools and context **with human help** (human interview posted in chat AND Inbox).
3. **Spin up the team and subtasks** around it.
4. Agents work in **their own context space — their notebooks** — documenting pre-task research and post-task deliverables/learnings. Keeps each agent focused; nobody consumes everyone's context.
5. **When done:** summarize what was delivered in the parent task and link the created artifact. **Every task must deliver an artifact in the wiki**, posted to chat and Inbox on completion.
6. **Learn from completion deterministically** (a hook, not ad-hoc wiki writes): extract entities, associated entities, and their insights → add to the **knowledge graph**.
7. The knowledge graph **writes wiki articles on entities**, associated to each other via links — exactly like Wikipedia.
8. Detect **human-readable rich playbooks** in that knowledge → document as wiki articles linked to the right entities and other playbooks.
9. The wiki reaches **full Wikipedia parity**: information architecture, UX, features, editing, article formatting, media richness, citations, navigation, appearance, flow, associations. No random folders — Wikipedia-style IA. You should not be able to tell you're not inside Wikipedia.
10. **At regular intervals, compile playbooks into skills**, activated only for the most relevant agents. **This is the only way skills are created. No playbook → no skill.**
11. Playbook compilation also creates **policies**, tied to the agents they're activated for. Policies can also be created on the fly in chat — **but only from human feedback**.
12. **Skills and policies are always loaded** in an agent's system context.
13. All other context is retrieved **on-demand** via hybrid search (BM25 + vector) from the agent's notebooks and the team wiki — at work start or mid-work. Agents **clearly show in chat the context they read and used**.
14. On every subsequent run: **update existing** notebooks/articles/policies/skills first — prune outdated items, extend existing ones — instead of creating new. Reduce redundancy and slop.

## Removals (R-phases — the subtraction)

| ID | Remove | Notes |
|---|---|---|
| R1 | **The redactor, completely** | The secrets-redaction pipeline that produced "[REDACTED]" over the user's own plans/drafts/artifact pointers (4 blind approvals in the ICP eval). Distinct check during execution: the approval-card **sanitizer** (PR #684, confused-deputy fix for agent-controlled strings inside structured approval payloads) is a separate security boundary — verify separation; if intertwined, preserve the sanitizer's invariant and flag. |
| R2 | **Spec creation + the spec surface** | IssueDraftSpec, CEO draft-writer, spec rail/freeze on task detail, intake Spec LLM ceremony. The task's understanding lives in the R4 intake interview + the task title/description. |
| R3 | **Plan mode** | PlanFirst toggle, LifecycleStatePlanning, plan packets, plan-approval gates, composer toggle. Replaced by R4's structured understanding (think first, but as dialogue, not a gated document). |
| R4 | **(Replacement)** Bake office-hours-style structured thinking into task intake | The CEO interrogates the task with forcing questions adapted from the office-hours method — goal, deliverable + format, success criteria, status-quo/why-now, narrowest first slice, what to observe — resolving gaps via human interview (chat + Inbox). Output is structured fields on the task (goal/deliverables/success_criteria/access_needed), not a spec document. Success criteria map onto the existing machine-verification gate where checkable. |
| R5 | **Agent skill proposals & request_skill_enable** | team_skill_create action=propose, skill-proposal interviews (the dead-Accept surface), skill nudges. Skills come ONLY from playbook compilation (B3). Keep + harden the SkillOpt mechanics (protected invariants, slow-update markers, bloat gates, consolidation/dedup) as the compiler's quality layer. |
| R6 | **Every other non-loop surface, FE+BE sweep** | Inventory in execution: Dashboard/activity app, DMs beyond task channels & #general, decision-packet surfaces not used by interviews/done-posts, legacy /issues remnants, demo seeds, channel ceremony, settle-checklist, unused apps. Opt-in transports (Telegram/OpenClaw/Hermes) deferred to a second sweep — load-time optional, not interfering. Each removal lands as its own commit with the eval suite green. |

## Builds (B-phases — what the loop needs that doesn't exist)

| ID | Build | Builds on |
|---|---|---|
| B1 | **Deterministic completion hook**: on verified done — summarize into parent task, require + link the wiki artifact, post to chat + Inbox, extract entities/associations/insights → knowledge graph | task_distill.go seam (U4.1), verification gate (U1.1) |
| B2 | **KG → entity wiki articles** with Wikipedia-style linking; kill folder taxonomy (Companies/People/… folders become categories/links) | wiki worker, entity graph (broker_entity*.go), Graph surface |
| B3 | **Playbook detection → playbook articles → interval compilation into skills + policies**, scoped to most-relevant agents | skill_compile cron, SkillOpt, policies store |
| B4 | **Notebooks as the agent's working context space** (pre-task research note + post-task deliverable/learnings note, per task) + on-demand hybrid retrieval (wire `internal/embedding` dense path into the U2 retrieval spine) + **"context I used" transparency posts in chat** | context_assembler.go, notebook system, U2 |
| B5 | **Wikipedia-parity wiki UX** | Tiptap (already in PR #1018 lineage). **DECISION FLAG:** docmost is AGPL-3.0; WUPHF is MIT — embedding docmost as a library imposes AGPL obligations on every distribution. Recommended: docmost as the UX reference (it's also Tiptap-based), native implementation. If literal docmost embedding is wanted anyway, that's a licensing decision for the founder to make explicitly. |
| B6 | **Update-first knowledge discipline**: retrieval-before-write on every store (notebook/article/policy/skill); prune/extend instead of create; staleness pruning | relevantLearnings seam, wiki lint/archiver |

## How the ICP-eval grader's top-3 fixes are absorbed

1. *Evidence pipeline into gates* → R1 removes the censor; B1 makes the artifact link mandatory in every done post; interviews already carry context.
2. *Make done mean done* → R4 turns success criteria into verification checks at intake; B1 requires the wiki artifact before done; U1.1 gate already enforces machine checks; Reopen must re-engage the owner (fix rides with B1).
3. *Dead skill Accept* → moot: R5 deletes the proposal surface entirely.

## Decisions log

| # | Decision | Why |
|---|---|---|
| C1 | Redactor removal preserves the PR-#684 approval-card sanitizer if separable | Different threat: confused-deputy via agent-controlled strings in approval payloads, not secret-masking |
| C2 | docmost = UX reference, not dependency (pending founder override) | AGPL-3.0 vs MIT distribution |
| C3 | Transports deferred to sweep 2 of R6 | Opt-in at load time; removing them now risks churn without serving the loop |
| C4 | Verification gate + eval harness + turn journal + context assembler + auto-distill SURVIVE | They are the loop's enforcement substrate (success criteria, artifacts, notebooks-as-memory, deterministic learning); eval harness checks get rewritten to assert the loop |
| C5 | Office-hours adoption = its method (forcing questions, narrowest wedge, design-doc discipline), not its gstack plumbing | The preamble/telemetry/config machinery is gstack-specific |

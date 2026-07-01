# wuphf Operator — Prototype Brief

**This is the single, self-contained spec for building the wuphf operator
prototype.** An AI or engineer can build the whole clickable prototype from this
document alone, no other file required. It is **frontend-first with mock data**:
build the clickable UI with fixtures, get the shape right, then wire a backend.

Status of the reference build: the prototype described here exists at
`web/src/operator/**` on branch `pivot/operator-mlp`. This doc is both the spec
and the record of decisions, so a fresh build reproduces the same product.

---

## 1. What it is

**wuphf** lets a non-technical operator build deterministic internal tools by
**talking to an AI**, with no engineering. The operator describes a workflow in
plain language; the AI figures it out and assembles a tool that watches for
something, decides what to do, and acts, the same way every time.

The spine of the product is one moment: **you explain a workflow in chat and the
AI builds it out in front of you.** Everything else supports that.

**Core principle:** *agentic freedom to FIGURE OUT the workflow; determinism to
EXECUTE it.* Discovery and authoring are conversational and flexible. Execution
is a compiled, audited, repeatable spec, not an open-ended AI tool-call chain.

### Persona and the one scenario to build against

- **Maya, RevOps.** Not an engineer. Lives in a CRM and Slack. Wants leverage
  without filing tickets to engineering.
- **Acceptance scenario:** inbound demo-request routing. A demo request comes in,
  the tool looks up the company, scores fit 0 to 100, routes strong leads to an
  account executive in Slack, nurtures the rest, and flags anything it cannot
  place. Build the whole prototype around this scenario.

---

## 2. Naming and voice (locked)

- The **app/brand is `wuphf`** (lowercase). The sidebar shows the real wuphf
  pixel-art logo (a pink/purple mark) plus the wordmark `wuphf` and the eyebrow
  `OPERATOR`.
- The **in-product AI is named `Nex`.** Chat is "talking to Nex." The primary
  teach-by-voice call CTA reads **"Teach your workflow to Nex"** (this replaced
  an earlier "Build on a call"). Chat author entry reads "Describe a new tool" /
  "Build a tool."
- Voice of copy: honest, plain, lightly funny, never hype. **No em dashes**
  anywhere in copy (use commas, colons, periods, parentheses). Oxford comma.
- Color is used for meaning only, never decoration. Buttons are black and white.

---

## 3. The five surfaces

The product is deliberately small. A left sidebar with five nav items, an
identity block, and the two build CTAs. No agents, channels, skills, or wiki
vocabulary.

1. **Chats** — talk to Nex. Tune existing tools, or start a new one. Entry to the
   chat-to-build flow ("Describe a new tool") and the teach-by-voice call.
2. **Internal tools** — the list of tools (a live "hero" tool, then Drafts, then
   "Suggested by your AI"). Opening one shows the tool detail with three tabs:
   - **UI** — the mini-app the operator runs (for inbound: a scored request
     table with a Route action that triggers the approval card).
   - **Workflow** — the deterministic spec as a vertical pipeline of steps, plus
     run history and version history.
   - **Data** — operator-owned typed tables the tool produces.
3. **Knowledge** — a Wikipedia-style reader of the company brain. Articles with
   an infobox, inline `[n]` citations, a References list, categories, and "see
   also" wikilinks. Every fact cites its source.
4. **Integrations** — connected / available / needs-attention apps (HubSpot,
   Slack, Gmail, Salesforce, Zendesk, Stripe).
5. **Settings** — workspace basics plus voice: bring your own OpenAI Realtime key
   or let wuphf host voice. No key means the call is optional and chat authoring
   is the keyless way in.

---

## 4. THE SPINE: chat-to-build authoring (build this first and best)

A dedicated two-pane builder. **Left: the conversation. Right: the workflow,
which assembles itself step-by-step as Nex understands the description.** This is
the product's core moment; spend the craft here.

### Interaction beats

1. **Enter the builder** from any "Build a tool" / "Describe a new tool" CTA
   (sidebar, Internal Tools header, Chats). The right pane shows a faint
   placeholder ("Your workflow takes shape here"). Nex opens with: *"Tell me what
   you want to automate. Walk me through how you would do it by hand, start to
   finish, and I will build the workflow as you talk."*
2. **Seed the magic in one click:** show 2 to 3 starter descriptions as chips
   below the composer. Clicking one sends it. Free typing also works.
3. **Nex "thinks"** (a brief typing indicator), then narrates a reflect-back
   ("Here is the workflow I put together from that, N steps...").
4. **The pipeline assembles live:** each step reveals on the right one at a time
   with a short stagger (a compositor-only translateY + opacity reveal). Steps
   carry their integration chip (`@HubSpot`, `@Slack`) and a gate badge (`asks
   you before it sends`) on external sends. A faint "ghost" node trails the last
   revealed step while more are coming.
5. **Nex asks exactly ONE sharp clarifying question** (the fit threshold, or
   which channel a handoff posts to). One, not a form. This is what makes it feel
   collaborative instead of a dump.
6. **The operator answers, and the matching step updates in place** (the decision
   node flashes and becomes, for example, `Is the score ≥ 70?`). Nex confirms.
7. **Hand-off:** Nex presents a draft-tool card in the conversation: the proposed
   tool name, a Draft badge, the step count, and two actions, "Open the tool" and
   "Run on test data." Nothing runs until the operator says so.

The right-pane header always shows the tool name (once known) and a status line:
"Reading your description" -> "Building the workflow" -> "One detail to confirm"
-> "Draft ready," with a small status LED.

### The "understanding" (mock compiler)

In the prototype the understanding is a **pure, deterministic, keyword-driven
compiler** that turns the description into an ordered list of steps. It is
explainable and testable, and it stands in for the real agentic build phase.
Build it as a pure function, `planWorkflow(description) -> WorkflowPlan`, plus
`applyClarify(steps, field, answer) -> steps`.

Compiler rules (canonical step order, only emit a step when its signal appears,
always emit a trigger and an action):

- **trigger** (always): derive the subject noun from the words (demo request,
  support ticket, expense, lead, inbound item) and name the tool from it.
- **enrich** if the text mentions look up / find / enrich / CRM / company /
  account; attach `HubSpot` when CRM-ish.
- **ai** if it mentions score / rate / qualify / fit (-> "Score fit 0 to 100"),
  or classify / severity / triage / categorize (-> "Classify and explain").
- **decision** if there is an AI step or words like if / above / threshold /
  good / urgent. Detect a numeric cutoff (`70`) or an amount (`$5k`). If a
  decision is implied but no number was given, this is the clarifying question.
- **action** (always): route / assign / page / notify / email / post. Attach
  `Slack` or `Gmail` from the words; mark external sends gated (they hit the
  human approval card). If a send has no named channel, that is the clarifying
  question.
- **branch** if it mentions otherwise / else / nurture / queue.

Pick at most one clarifying question, preferring a missing numeric cutoff, then a
missing channel. Map the finished tool to the matching scenario tool on hand-off
(ticket -> escalation tool, expense -> expense tool, else -> inbound routing).

Starter chips to ship:
- "When a demo request comes in, look up the company, score how good a fit they
  are, and send the strong ones to an AE in Slack. Nurture the rest."
- "When a support ticket is tagged urgent, classify its severity and page the
  on-call engineer for the worst ones."
- "When an expense over $5k comes in, check it against policy and route the
  exceptions to me to approve."

---

## 5. The tool object (detail view)

- **Status model (locked): Enabled / Disabled / Draft / Suggested.** Header
  actions are status-aware: Enabled -> Disable + Publish new version; Disabled ->
  Enable + Publish new version; Draft -> Run on test data + Publish; Suggested ->
  Build it. There is also an **Edit with AI** button on any non-suggested tool.
  Do not use "live / pause."
- **Edit with AI** opens a right-docked chat beside the tool (the tool stays
  visible). The operator describes a change, Nex shows a concrete proposed-change
  card ("Proposed change, v(n+1)" with a bullet list), and "Apply & publish"
  bumps the version and records it in version history. Same build-with-you feel
  as authoring, applied to an existing tool.
- **Human approval gate (CQ1):** any external mutation, at build time or run
  time, surfaces a shared approval card ("Route Acme to an AE in #ae-handoffs?"
  with the reason) with Approve / Skip. The AI observes and reasons freely but is
  gated to act.
- **Workflow tab** renders the steps as the same pipeline used in the builder,
  plus Recent runs and Version history rails.

---

## 6. Design system (locked, do not relitigate)

Converged over many rejected iterations. Honor it up front.

- **Dark, always.** Use **shadcn `nex-dark` tokens** consumed at the shell root
  (`--background / foreground / card / border / primary`). Do not invent a
  parallel palette.
- **Contrast is terminal-MUTED, not harsh.** Soft off-white (~82% L) on dark gray
  (~#111213), never pure `#000` or `#fff`. Bright-white-on-black fatigues the
  eyes and was explicitly rejected. Status colors are toned so nothing glows.
- **Typography: UI sans for the interface; monospace ONLY for real data**
  (scores, counts, IDs, timestamps, code). The all-monospace +
  `[brackets]` + `❯`-prompts look reads as "AI terminal costume" and is the #1
  slop tell. Terminal soul lives in small details, not a full-screen costume.
- **Buttons are black and white.** Near-white primary, neutral outline secondary.
  No colored button fills. The shadcn cyan `--primary` is the accent for
  **links, citations, and highlights only**.
- **Color means something** (green = good/routed, amber = needs you, red =
  danger/recording, cyan = links/info). Everything else is neutral.
- **Brand mark:** the real wuphf pixel-art logo, rendered crisp
  (`image-rendering: pixelated`).
- **Anti-slop bans:** no hero-metric stat-tile rows (use an inline readout like
  "today 14 processed 5 routed..."), no side-stripe accent borders (a colored
  `border-left/right` > 1px), no card-everything (prefer editorial flush layout
  with hairline rules), no gradient text, no glassmorphism, no em dashes. Cards
  only when truly the best affordance.
- **Motion:** one or two purposeful moments (the step cascade, the in-place
  refine flash, a thinking indicator). Compositor-only properties (`transform`,
  `opacity`). Ease-out curves, no bounce. Honor `prefers-reduced-motion`.
- **Accessibility:** focus-visible rings, modal Escape + focus trap, `role=tab` /
  `aria-controls` on tabs, `aria-label` on inputs, >=44px touch targets.

---

## 7. Mock data shapes (reference)

Build all of this as fixtures centered on the inbound scenario. Types:

```ts
type ToolStatus = "enabled" | "disabled" | "draft" | "suggested";
type WorkflowStepKind =
  "trigger" | "enrich" | "ai" | "decision" | "action" | "branch";

interface WorkflowStep {
  id: string; kind: WorkflowStepKind; title: string; detail: string;
  integration?: string;   // "HubSpot" | "Slack" | "Gmail"
  gated?: boolean;        // external mutation -> approval card
}
interface InternalTool {
  id: string; name: string; status: ToolStatus; summary: string;
  version: number; lastRun: string; runsToday: number;
  builtFrom: "call" | "chat" | "detected";
  steps: WorkflowStep[]; runs: WorkflowRun[]; versions: ToolVersion[];
  dataColumns: DataColumn[];
}
interface InboundRequest {
  id: string; company: string; contact: string; email: string; source: string;
  receivedAt: string; fitScore: number | null;
  status: "new" | "scored" | "routed" | "nurturing" | "needs-you";
  reason: string; routedTo?: string;
}
// Knowledge is Wikipedia-shaped: a page has an infobox, a lead, sections whose
// prose carries [[n]] citation markers, a references list, categories, see-also.
```

Seed at least: one Enabled inbound-routing tool (6 steps, runs, 4 versions), one
Draft support-escalation tool, one Suggested expense tool; six inbound requests
spanning routed / scored / nurturing / needs-you; the integrations list; and 2 to
3 Knowledge pages (ICP, AE routing policy, nurture).

---

## 8. Stack and mounting

- React 19 + Vite + TypeScript, Bun for tooling, Tailwind v4 + shadcn
  (`nex-dark`). State is local component state for the prototype (no store
  needed). All styles under one prefixed sheet (the reference uses `opr-`).
- Mount the operator shell **full-bleed at a hash route `/#/operator`**, bypassing
  any surrounding app shell / onboarding / backend. The whole thing must run with
  no server: the only "intelligence" is the mock compiler.
- Verify: typecheck clean, no console errors, and the inbound scenario is
  click-through end to end, including the chat-to-build flow producing a draft.

---

## 9. Build checklist (prototype is "done" when)

1. The five surfaces render and navigate, dark, on-brand, with the real logo.
2. The chat-to-build builder: type or click a starter, watch the pipeline
   assemble live, answer one clarifying question, see a step refine in place, get
   a draft-tool hand-off card that opens the tool.
3. A tool detail shows all three tabs; the inbound UI tab routes a lead through
   the approval card; Edit-with-AI bumps a version.
4. Knowledge renders a cited, Wikipedia-style article.
5. Settings shows the voice BYOK / wuphf-hosted choice.
6. The design system holds: muted contrast, sans + mono-for-data, B&W buttons,
   color only for meaning, none of the anti-slop tells.

---

## 10. What is deferred (do not build in the prototype)

The real backend: gbrain (the knowledge + context brain over MCP), the
deterministic workflow engine, the teach-by-voice capture call (screen-share +
voice -> API discovery), and integration auth. Those come after the founder
validates this shape. Keep the prototype mock and honest about it.

**Harness decision (BE, founder, 2026-06-27): clean deepagents-native rewrite.**
This is a pivot stage; the software is being rewritten. The directive:

- **deepagents (LangChain) is the NATIVE core agentic harness.** One well-built,
  optimized, fast agent that quickly builds a workflow. No bolt-on.
- **Move off multi-agents entirely. The Go broker goes** (no office, channels,
  lifecycle, coordination, sub-task kernel). It no longer fits the product.
- **Keep only:** provider detection (multi-provider inference + BYOK), settings,
  and scaffolding that genuinely support the product (build/install, the Electron
  shell, the operator FE).
- **A new repo is authorized** if it is the cleaner start. This new product
  eventually replaces wuphf entirely.
- The agentic-build / deterministic-execute principle still holds: the deep agent
  FIGURES OUT the workflow; a deterministic executor RUNS the compiled spec.
- This supersedes most of the in-flight LangGraph multi-agent migration (the
  goal/coordinate/decompose kernel and broker re-hydrate are moot). Salvage the
  seam only: Python reaching tools over MCP, the FastAPI service pattern, the
  Claude Agent SDK wiring, and provider config.

The prototype does not depend on any of this; the mock `planWorkflow` compiler
stands in for the agentic build until the harness is built. This is a recorded
note, not a task for the prototype.

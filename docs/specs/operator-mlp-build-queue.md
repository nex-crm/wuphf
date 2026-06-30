# Operator MLP — Build Queue

**Branch:** `pivot/operator-mlp` · Companion to `operator-mlp-plan.md`.

**Sequencing rule (locked 2026-06-26):** *frontend-first with mock data.* Every
slice ships the clickable UI with fixtures FIRST so the founder can react to the
shape; the data model and backend are built only after the shape is validated.
See memory `feedback_frontend_first_mock_data`.

> **⚑ BACKEND REWRITE DECISION (founder, 2026-06-27) — NOTE, not yet started.**
> This is a pivot stage; the backend is being rewritten clean.
> - **deepagents (LangChain) is the native core agentic harness** — one fast,
>   optimized agent that quickly builds a workflow. Agentic-build /
>   deterministic-execute still holds.
> - **Move off multi-agents; the Go broker goes** (no office, channels,
>   lifecycle, coordination kernel).
> - **Keep only** provider detection (multi-provider inference + BYOK), settings,
>   and product scaffolding (build/install, Electron shell, the operator FE).
> - **New repo authorized** if cleaner; this product eventually replaces wuphf.
> - Supersedes most of the in-flight LangGraph multi-agent migration
>   (`docs/specs/deepagents-migration-plan.md`); salvage the seam only (Python↔
>   tools over MCP, FastAPI service, Claude Agent SDK wiring, provider config).
> - The backend slices below are **rewritten under this lens** when BE starts;
>   the FE prototype is unaffected and remains the product UI. Hand-off spec:
>   `operator-mlp-prototype-brief.md` §10.

---

## ✅ Slice 1 — Operator shell, mock data (DONE, awaiting founder review)

The whole product shape, clickable, no backend. Mounted full-bleed at
`/#/operator` (bypasses the office Shell / onboarding / broker).

- Files: `web/src/operator/**`, `web/src/styles/operator-shell.css`, one branch
  in `web/src/routes/RootRoute.tsx`.
- Surfaces: Chats · Internal Tools (UI · Workflow · Data) · Knowledge ·
  Integrations · Settings, all centered on the Maya inbound-routing scenario.
- Shows: the fit-score → route table (hero), the deterministic workflow + run +
  version history, the **approval card** (CQ1), the **magical call** mock
  (primary activation), the **voice BYOK / Nex-hosted** settings (CEO #5).
- **Chat-to-build authoring (the spine), built 2026-06-27:** a two-pane
  `WorkflowBuilder` (conversation left, workflow that assembles live on the
  right). Describe a workflow in plain language → the AI compiles a deterministic
  pipeline that cascades in step-by-step → it asks ONE clarifying question →
  refines the matching step in place → hands off a draft tool. This is the FE for
  Slice 6 authoring, prototyped early because it is the product's core moment.
  Pure compiler in `operator/builder/planWorkflow.ts`.
- **Brand + copy locked:** real wuphf pixel logo (`/favicon.svg`) in the
  sidebar; the in-product AI is named **Nex**; the call CTA reads **"Teach your
  workflow to Nex"** (was "Build on a call"). App = wuphf, AI = Nex.
- Status model: **Enabled / Disabled / Draft / Suggested + "Publish new
  version"** (not live/pause). Edit-with-AI dock inside each tool.
- Status: typecheck clean; screens captured in `scratchpad/operator-shots/`.
- **Gate:** founder validates the shape (and requests changes) before any
  backend work begins.

> **Hand-off doc:** the single self-contained spec another AI can build this
> prototype from is **`operator-mlp-prototype-brief.md`** (same folder). It
> folds in the product, the surfaces, the chat-to-build flow, the status model,
> the design system, the naming, and the mock data shapes.

---

## Queue (build only after the shape is locked)

### Slice 2 — FE iteration from founder feedback
Tighten the shape: surfaces to cut/add, the call flow, the tool-detail tabs, copy.
Still mock data. Re-screenshot.

**Design follow-ups (tracked from /plan-design-review, 2026-06-26):**
- **Responsive < 900px** (deferred): the 2-col tool detail (`1fr 320px`), chat, and
  knowledge grids break; sidebar is fixed-width. Desktop-first is fine for v1, but
  make it degrade (collapse rail → 1-col, stack grids) before any non-desktop use.
- Storybook stories for the operator components (`UI / Operator / *`).
- Loading skeletons for the tool list / table / chat once real data replaces mock.
- (Fixed already: empty states, keyboard focus-visible, small-text contrast → AA,
  Space-key on rows, tabpanel roles, live-tool primary-action emphasis.)

### Slice 3 — Strip the office cruft + operator shell becomes the app (Phase 0)
Make `/operator` the real app surface (route the old office bundle out); delete
notebooks/skills/policies/roster/wizard engine paths the operator does not need.
`go build` + web build green; LOC down. *No new backend yet — wire mock to thin
read endpoints.*

### Slice 4 — gbrain stood up + Knowledge over it (Phase 0; tests A1, A2)
gbrain as a bundled subprocess (PGLite + local-ollama default), reached over MCP.
Knowledge reader points at gbrain pages.
- **Test (A1):** parallel-run vs the karpathy-wiki compile backend → reader-compatible pages + retrieval/lint bar, THEN retire old backend.
- **Test (A2):** all broker→gbrain MCP calls off-lock; gbrain-unreachable degrades gracefully (never deadlock / hard-fail).

### Slice 5 — The deterministic loop, hand-authored (Phase 1; test CQ1)
One inbound-routing spec by hand → engine run on 10 fixtures → digest. Wire the
UI/Workflow/Data tabs to real run records + the Data store.
- **Test (CQ1):** build- and exec-time external mutation → the shared human approval card.

### Slice 6 — Operator authoring via chat + detection (Phase 2)
AI drafts the spec + UI from a chat; chat-to-edit; publish = freeze + version.
Detection repointed to operator activity proposes a candidate.
*FE already prototyped in Slice 1 (`WorkflowBuilder` + `planWorkflow`); this slice
swaps the keyword compiler for a real agentic build and persists the draft.*

### Slice 7 — The magical call (Phase 3; the demo gate; tests A3, A4)
Electron renderer screen-share + free-voice → capture (CDP/chromedp) →
`browsersniff` (lift) → workflow-spec draft into the build chat. Composio-first
build; sniffed-API / UI-replay / CUA-heal fallback.
- **Test (A3):** capture HAR scoped to active tab + secrets stripped (piiplaceholders) + ephemeral; auth = typed ref, never value on disk.
- **Test (A4):** sniffed auth classified — stable key stored, rotating session flagged "needs a live session".
- **GATE:** no external "this is the product" demo until this lands.

### Slice 8 — Proactive improvement (Phase 4)
Context change → "suggested change + why" card → approve → overlay/refreeze,
version+1, visible in history.

---

## The 5 critical tests (must land with their slice)
A1 gbrain page-IA parallel-run · A2 gbrain-unreachable degrade · A3 HAR
secret-strip · A4 auth-classify rotating→session · CQ1 build+exec
mutation→approval-card. (Mapped to slices 4/5/7 above.)

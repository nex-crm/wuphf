# Agent HTML Artifacts

WUPHF keeps the team wiki git-native and markdown-first. That remains the
source of truth for durable facts, briefs, and playbooks. HTML artifacts are a
companion format for work that humans need to inspect, compare, tune, or share
without reading a long linear markdown file.

Use HTML artifacts when the output benefits from visual structure:

- implementation plans with timelines, risk maps, data flow, or mockups
- PR explainers with annotated diffs, call graphs, and review focus areas
- design explorations, component sheets, and interaction prototypes
- incident reports, status reports, research explainers, and diagrams
- throwaway editors that export a prompt, JSON, markdown, or patch summary

Keep markdown for:

- canonical wiki articles and facts
- short notes that are naturally linear
- files humans are expected to edit by hand
- contracts where a compact text diff matters more than visual review

## Human I/O Rationale

Treat HTML artifacts as a better output channel, not just a richer file
extension. A useful current-stage pattern is: humans often prefer high-bandwidth
input to agents through speech, pointing, screenshots, and gestures, while
agents should increasingly return high-bandwidth visual output: layout,
diagrams, animation, simulation, and interaction.

The progression is practical:

1. raw text for compact answers
2. markdown for structured prose
3. HTML for visual, spatial, and interactive review

WUPHF adopts step 3 where it helps humans stay in the loop. The goal is not to
make every response decorative. The goal is to use the browser as a review
surface when code, architecture, planning, or product decisions are easier to
understand visually than linearly.

## Repository Convention

Store durable repo-local HTML artifacts under `docs/agent-artifacts/` with a
date-prefixed filename:

```text
docs/agent-artifacts/2026-05-11-runtime-explainer.html
```

If an HTML artifact makes a decision durable, add a short markdown summary or
link from the owning doc. The HTML can carry the readable argument, but the
canonical decision needs to remain discoverable by text search and wiki
ingestion.

Use `scripts/new-html-artifact.sh` to start from the repo template:

```bash
bash scripts/new-html-artifact.sh runtime-explainer "Runtime explainer"
```

The script writes `docs/agent-artifacts/YYYY-MM-DD-runtime-explainer.html`.
Open the file directly in a browser for local review.

## Artifact Rules

- Make the artifact self-contained: inline CSS and JavaScript, no build step.
- Prefer no network dependencies. If an external image, script, or font is
  necessary, document why in the artifact metadata.
- Do not include secrets, bearer tokens, private customer data, or raw logs
  with credentials. Redact before rendering.
- Include provenance: source files, commands, PR number, issue, or user prompt.
- Include an export path for interactive editors, such as "copy JSON", "copy
  markdown", or "copy prompt".
- Keep generated artifacts reviewable. Split a very large artifact into a
  small index page plus focused linked pages.
- Use accessible structure: semantic headings, labels, visible focus states,
  keyboard-operable controls, and sufficient contrast.
- Keep WUPHF product language precise. Nex is a context graph platform for AI
  agents, not a CRM.

## Recommended Shapes

| Use case | Artifact shape | Required affordance |
|---|---|---|
| Implementation plan | Timeline, dependency map, risk table, code snippets | decision log and verification plan |
| PR explainer | File tour, annotated diff, reviewer checklist | copyable PR summary |
| Architecture review | System diagram, boundary table, failure modes | source references |
| Design exploration | Side-by-side variants, token swatches, state sheets | tradeoff labels |
| Incident report | Timeline, blast radius, evidence, follow-up table | owner/status export |
| Custom editor | Form, board, sliders, preview | copy/export button |

## Adoption Boundary

This convention does not change the wiki rebuild contract in
`docs/specs/WIKI-SCHEMA.md`: markdown remains the durable source of truth for
the local wiki. HTML artifacts can be linked from wiki pages, attached to PRs,
or used as review material, but they are not the authoritative fact store unless
a future product change explicitly adds that contract.

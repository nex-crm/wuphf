# Rich HTML Artifact Acceptance Criteria

These criteria turn the original HTML-first artifact vision into three tutorial
examples that can be checked in a browser. The intent is not "HTML everywhere";
it is "HTML where the user is trying to review, compare, tune, explain, or
share something too dense for plain markdown."

## Vision Coverage

| Vision area | Expected WUPHF behavior | Covered by |
|---|---|---|
| Notebook-first creation | Agents write a durable markdown note and attach a self-contained HTML visual companion to that notebook source. | RevOps Launch Risk Room |
| Wiki-first review surface | Promoted artifacts render as the wiki article's visual view with provenance and trust level visible. | Support Incident Command Room |
| Chat handoff | Agents post a terse artifact marker, and chat renders a rich artifact card while hiding the raw marker from the message body. | Sales Engineering Review Brief |
| Interactivity | HTML artifacts include local controls for tuning, tabbing, copying, or exporting useful follow-up prompts. | All three examples |
| Safety | HTML is self-contained, iframe-rendered, and unable to fetch network resources from the artifact body. | All three examples plus sandbox tests |
| Shareability | Markdown keeps the durable source and summary; HTML carries the dense visual explanation users actually review. | All three examples |
| Visual taste | HTML artifacts use the WUPHF paper-manual style: warm real-paper feel, serif reading copy, exact Making Software cobalt shades for figure ink, faint figure grids, mono metadata, muted complementary state colors, and sparse instrumentation controls. | All three examples |

## Tutorial 1: RevOps Launch Risk Room

User story: a RevOps operator asks an agent to turn a launch readiness note into
a visual decision room before committing a go-to-market plan.

Acceptance criteria:

1. The agent first creates or updates
   `agents/revops-agent/notebook/revops-launch-risk.md` as the durable notebook
   source.
2. The agent then creates an HTML artifact titled `RevOps Launch Risk Room`
   attached to that source note.
3. The notebook article shows a `Visual artifacts` section with a card for the
   artifact, its trust level, creator, and an inline preview iframe.
4. The HTML contains a scannable launch-risk table, owner-load data, three risk
   sliders, and a clear decision state.
5. Moving the sliders updates the displayed score and decision copy in the
   browser without a page reload.
6. The artifact includes a `Copy mitigation prompt` control so the user can take
   tuned output back into chat.
7. The artifact remains self-contained: inline CSS/JS only and no external
   network fetches, fonts, scripts, or images.

Browser check:

- Open `fixtures/revops-launch-risk/artifact.html`.
- Confirm `Launch Risk Room`, `Owner load`, and `Copy mitigation prompt` are
  visible.
- Drag at least one slider and confirm the score/decision text changes.

## Tutorial 2: Support Incident Command Room

User story: a support ops lead promotes an incident timeline from an agent's
notebook into the team wiki so leadership and customer-facing teams can read one
clear command-room view.

Acceptance criteria:

1. The agent first creates or updates
   `agents/support-agent/notebook/support-incident-room.md`.
2. The agent creates an HTML artifact titled `Support Incident Command Room`.
3. The agent promotes the reviewed artifact to
   `team/playbooks/support-incident-command-room.md`.
4. The wiki article has a visual tab/view that renders the promoted HTML
   artifact, not only the markdown summary.
5. The wiki visual view shows the promoted trust level and artifact path so the
   user understands the provenance.
6. The HTML gives the user fast navigation between `Customer impact`,
   `Timeline`, and `Copy update` views.
7. The `Copy update` view exposes editable customer-facing copy and a copy
   button for handoff.

Browser check:

- Open `fixtures/support-incident-room/artifact.html`.
- Confirm `Incident Command Room`, `Customer impact`, and `Copy update` are
  visible through the tabs.
- Switch to the timeline and update views, then confirm only the selected panel
  is active.

## Tutorial 3: Sales Engineering Review Brief

User story: a sales engineer posts a brief chat update about a technical review.
The agent includes a rich artifact reference so reviewers get a compact card and
can open the full annotated explainer when needed.

Acceptance criteria:

1. The source chat message contains `visual-artifact:ra_aaaaaaaaaaaaaaaa` on its
   own line.
2. The chat UI strips that standalone marker from the rendered message body.
3. The chat UI renders a rich artifact card labeled as a notebook or wiki
   visual, with title, summary, trust level, and source path.
4. Opening the card renders the artifact in the sandboxed modal iframe.
5. If the artifact has a promoted wiki path, the card/modal exposes an
   `Open wiki` link.
6. The HTML brief includes an annotated diff, buyer-risk lens, and a copyable
   reviewer prompt.
7. The artifact gives enough context that the reviewer can understand why the
   change matters without reading raw markdown or a GitHub diff first.

Browser check:

- Open `fixtures/sales-review-brief/artifact.html`.
- Confirm `Reviewer Brief`, `Diff focus`, and `Copy reviewer prompt` are
  visible.
- Confirm the reviewer prompt is editable/copyable in the browser.

## Current Scope Boundary

In scope for this phase:

- Notebook visual artifact creation, listing, reading, and promotion.
- Wiki visual view rendering for promoted artifacts.
- Chat artifact references from `visual-artifact:<id>` markers.
- Sandboxed HTML rendering with no network access from artifact bodies.
- Agent guidance that tells agents when and how to produce HTML artifacts.

Not yet in scope:

- Unified inbox artifact surfaces.
- AG-UI/CopilotKit-style two-way application state handoff.
- Hosted sharing outside the repo/wiki.
- Multi-artifact decks or generated video/simulation outputs.

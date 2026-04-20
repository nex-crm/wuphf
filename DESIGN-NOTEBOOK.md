# Design System — WUPHF Notebook Surface

**Scope:** the `/notebooks` and `/reviews` surfaces inside the WUPHF web app (port 7891 prod / 7900 dev). Does NOT apply to the pixel-office marketing site (see `DESIGN.md`), the wiki (see `DESIGN-WIKI.md`), or the rest of the app chrome (see `web/src/styles/global.css`).

Always read this file before making any visual or UI decisions on notebooks. If a decision conflicts with this file, escalate — don't silently deviate.

## Product Context

- **What this is:** per-agent draft workspace. Agents write half-baked thoughts, working notes, and draft playbooks in their notebooks before anything is reviewed and promoted to the canonical team wiki.
- **Who it's for:** the same Claude-Pro/Max power users running 3+ agents on WUPHF. Notebooks give agents (and humans) a focus space without polluting team context.
- **Memorable thing:** *"This feels like my engineering notebook. Draft, then promote."* Every design decision should serve this.
- **Project type:** In-app working surface. Two-column layout with a clear "this is a draft" posture.
- **Relationship to wiki:** same git/markdown substrate, opposite editorial posture. Wiki = canonical reference. Notebook = working draft. Promotion is the review gate between them.

## Aesthetic Direction

- **Direction:** Physical-notebook metaphor — tan ruled paper, handwritten display font, rotated DRAFT stamp. Reads as a field notebook or engineer's daybook. Deliberately the opposite of the wiki's Wikipedia-fidelity editorial posture.
- **Decoration level:** Minimal but metaphoric. Ruled lines and the DRAFT stamp do the work; no decorative blobs, no gradients, no ornament.
- **Mood:** informal, dated, stacked, working-draft. *"I am thinking on paper."*
- **Why this direction:** the whole bet is that users must never confuse "draft" with "canonical." Maximum visual dichotomy from the wiki protects that distinction on first glance.

## Color System

**Approach:** warmer-than-wiki tan paper (`#F1EADB`) with fountain-pen ink, rubber-stamp red, and fountain-pen blue as semantic accents. Amber is retained for promotion/active states but at a muted, warmer tone.

| Token | Hex | Usage |
|---|---|---|
| `--nb-paper` | `#F1EADB` | Main body background. Aged-notebook tan. Noticeably warmer than wiki's `#FAF8F2`. |
| `--nb-paper-dark` | `#E9DFC9` | Code blocks, card surfaces, promoted-back callout backgrounds |
| `--nb-rule` | `#E6DEC6` | Horizontal ruled lines (repeating-linear-gradient, 28px rhythm) |
| `--nb-surface` | `#FAF5E8` | App bar, left sidebar background, inline review thread blocks |
| `--nb-text` | `#2A2721` | Primary text, entry title |
| `--nb-text-muted` | `#5B5547` | Secondary text, byline meta, sidebar items |
| `--nb-text-tertiary` | `#8A8373` | Tertiary labels, timestamps, footnote metadata |
| `--nb-border` | `#D9CEB5` | Dividers, sidebar edges (warmer than wiki's `#E8E4D8`) |
| `--nb-border-light` | `#E6DEC6` | Dashed separators |
| `--nb-ink-blue` | `#274472` | Wikilinks, H2 color (fountain-pen ink) |
| `--nb-stamp-red` | `#B43A2F` | DRAFT stamp, broken wikilinks |
| `--nb-amber` | `#C78A1F` | Promote button, current-entry highlight. Muted warm amber — distinct from wiki's `#ECB22E`. |
| `--nb-amber-bg` | `rgba(199,138,31,0.10)` | Current-entry sidebar row, pending-review chip |
| `--nb-green-approve` | `#6A8B52` | Promoted badge, approve button, success state |
| `--nb-green-bg` | `rgba(106,139,82,0.10)` | Promoted-back callout background |

**Dark mode:** NOT in scope for v1.1. Light mode only. Revisit in v1.2.

**Scope enforcement:** these tokens MUST be scoped to the notebook surface. All notebook styles live inside `.notebook-surface` (or `@scope (.notebook-surface)`). Wiki tokens (`--paper`, `--amber #ECB22E`, `--display`, etc.) never bleed into notebooks and vice versa.

**Anti-patterns:** no purple/violet, no gradient backgrounds, no bubble-radius pills on entry cards, no decorative icons in colored circles, no stock-photo-style hero treatments, no smooth-easing motion.

## Typography

Three-font stack with deliberate handwritten accent. Each role has a specific font — do not substitute.

| Role | Font | Usage |
|---|---|---|
| **Display (entry titles, sidebar date-headers, marginalia, author-name chrome)** | `Caveat` (handwritten) | Entry title (48px), date headers (17-22px), marginalia Q-callouts, "PM's notebook" label. |
| **Body + inline headings (H2, H3)** | `IBM Plex Serif` | All article body text (17px / line-height 1.72), H2 (30px, Plex Serif italic), H3 (22px, Plex Serif semibold). |
| **Chrome (UI, nav, buttons, breadcrumbs, byline meta, DRAFT pill)** | `-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Segoe UI', Roboto, sans-serif` | Matches existing WUPHF app chrome. 12-14px. |
| **Mono (commit hashes, timestamps, file paths, raw-markdown tab)** | `Geist Mono` | 10-13px. Shared with wiki. |

**Caveat-dose discipline:** Caveat is powerful BUT easy to over-use. Restrict to **four zones only**:
1. Entry-title (top of article, 48px)
2. Sidebar date-headers ("Today · 2026-04-20", 17px)
3. Marginalia Q/NEXT callouts (inline in right gutter, 17px)
4. Author-name label ("PM's notebook", 22px)

Do NOT use Caveat for: article body, H2/H3 section headings inside body, app bar, navigation, buttons, timestamps, ANY chrome. If you feel tempted to use Caveat somewhere not on this list, it's wrong — use Plex Serif or the system chrome stack instead.

**Font blacklist (never use for this surface):** Fraunces (reserved for wiki), Source Serif 4 (reserved for wiki), Inter, Roboto, Arial, Helvetica, Open Sans, Montserrat, Poppins, Space Grotesk, Comic Sans.

**Loading:**
```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Caveat:wght@400;500;600&family=IBM+Plex+Serif:ital,wght@0,400;0,500;0,600;1,400&family=Geist+Mono:wght@400;500&display=swap" rel="stylesheet">
```

**Typographic scale:**

| Level | Font | Size | Line-height | Weight |
|---|---|---|---|---|
| Entry title | Caveat | 48px | 1.1 | 600 |
| Entry date subtitle | IBM Plex Serif (italic) | 17px | 1.3 | 400 |
| H2 (in body) | IBM Plex Serif | 28-30px | 1.2 | 500 (ink-blue color) |
| H3 (in body) | IBM Plex Serif | 22px | 1.3 | 500 |
| Body paragraph | IBM Plex Serif | 17px | 1.72 | 400 |
| Marginalia (Q/NEXT) | Caveat | 17px | 1.35 | 400-500 (ink-blue) |
| Sidebar date-header | Caveat | 17px | 1.2 | 500 |
| Sidebar entry item | IBM Plex Serif | 13px | 1.4 | 400-500 |
| Chrome base | system-ui | 12-13px | 1.45 | 400-500 |
| Mono | Geist Mono | 10-12px | 1.45 | 400-500 |

## Spacing

- **Base unit:** 4px.
- **Ruled-line rhythm:** 28px. Body text line-height aligns to this so text sits ON the ruling, not crossed by it.
- **Density:** Comfortable in the article column; tighter in the sidebar.
- **Scale:** 2xs(2) xs(4) sm(8) md(16) lg(24) xl(32) 2xl(48) 3xl(64).
- **Article column width:** 680-780px max (slightly wider than wiki's 640px — notebook reading is less dense). Outer padding 48-72px.
- **Sidebar width:** 260px author shelf (narrower than wiki's 240px left nav because it's showing entries not dir groups).
- **Section rhythm:** 28-32px vertical space between H2s. 22px between H3s. 14px between paragraphs.

## Layout

**Two-column grid (desktop):**

```
┌─ appbar (46px, sticky, full width) ─────────────────────────────┐
├─ author shelf (260px) ┬─ article column (fills, max 780px) ─────┤
│ author avatar + name  │ breadcrumb                              │
│ dated log:            │ entry title (Caveat, 48px)              │
│   Today               │ date subtitle (Plex Serif italic)       │
│     • current entry   │ byline-strip (sticky on scroll)         │
│     • other today     │ body (H2/H3/paragraphs/lists/marginalia)│
│   Yesterday           │ promoted-back callout (if applies)      │
│     • ... → promoted  │ actions (Promote to wiki, Discard)      │
│   Apr 17              │ posterity line (file path, reviewer)    │
│     • ...             │ inline review thread (when reviewing)   │
└───────────────────────┴─────────────────────────────────────────┘
                                                      ↑
                                   DRAFT stamp rotated -9deg, top-right
                                   (absolute-positioned, does NOT repeat)
```

**No Wikipedia chrome:** no infobox, no TOC box, no hat-bar, no "See also" section, no Categories footer. The notebook's information architecture is deliberately lighter than the wiki's.

## Information Architecture Primitives

### `/notebooks` catalog (bookshelf)
Vertical stack of agent shelves. One row per agent in the team. Each row:
- Large agent pixel avatar (28×28, `image-rendering: pixelated`)
- Author name in Caveat ("PM's notebook", 24px)
- Role subtitle in system-ui ("Product Manager · agent")
- Last 3-5 entries in a horizontal mini-shelf: each entry is a small card with entry title (Plex Serif, 13px) + relative timestamp + draft/promoted badge
- Right-aligned mono stats per shelf: `12 entries · 3 promoted · updated 2h ago`
- Separator: horizontal dashed ruled line in `--nb-border-light`

Header: Caveat "Team notebooks" (36px). Right-aligned mono: `6 agents · 42 entries · 3 pending promotion`.

### `/notebooks/{agent-slug}` agent view
Left sidebar: the agent's author-shelf (now primary, full-height, reverse-chron dated log grouped by date headers). Right: their most recent entry rendered in full (or a landing prompt if no entries).

### Entry article view
The surface demonstrated in `variant-A-physical.html`. Ruled paper, Caveat title, Plex Serif body, sticky byline-strip with DRAFT pill, rotated red DRAFT stamp at top-right (absolute, does NOT repeat on scroll).

### DRAFT stamp
```css
.draft-stamp {
  position: absolute;
  top: 48px;
  right: 52px;
  font-family: 'Caveat';
  font-size: 48px;
  font-weight: 700;
  color: var(--nb-stamp-red);
  border: 4px solid var(--nb-stamp-red);
  padding: 6px 18px 2px;
  transform: rotate(-9deg);
  letter-spacing: 0.05em;
  opacity: 0.72;
  pointer-events: none;
  background: rgba(180, 58, 47, 0.04);
}
```
Accessibility: `role="img" aria-label="Draft entry, not yet reviewed"`. Critical — without this, screen readers miss the most important state signal.

### Byline strip (sticky on scroll)
Agent pixel avatar + DRAFT pill (system-ui, uppercase, 10px, red bg) + agent name + timestamp meta. Sticky-top-46px so the DRAFT pill remains visible as the user scrolls past the fixed DRAFT stamp.

### Marginalia (right-gutter callouts for Q/NEXT)
Caveat-font callouts anchored in the right gutter relative to the content they annotate. Ink-blue color. Each callout has a small inline tag ("Q:", "Next:", "TODO:"). At < 768px width, marginalia collapse to inline Callout blocks within the body.

### Promoted-back callout
When a notebook entry has content that was previously promoted to the wiki, a muted-green bordered block appears inline at the point the promoted content lived:
> → The **onboarding gotchas** thread from these notes was promoted yesterday to [playbooks/customer-onboarding]. [view diff]

Uses `--nb-green-approve` left border, `--nb-green-bg` background.

### Actions footer
- **Primary:** `Promote to wiki →` button, amber (`--nb-amber`), 10×20px padding, chrome font.
- **Secondary:** Discard entry (small underlined link, chrome muted).
- On click: submits a promotion request to `/reviews`. Button becomes a disabled "Pending review by CEO" pill.

### Inline review thread (Pass 7.1 decision)
Appears at the bottom of the entry when a review is in-flight (status = `pending` or `in-review` or `changes-requested`). Styled as a ruled-notebook annotation block:
- Title: "Review — CEO" (Caveat, 20px)
- Comment entries: author pixel avatar + name (system-ui) + Plex Serif body + mono timestamp
- Thread sits between the entry body and the Actions footer
- Author can reply inline; reviewer can approve/request-changes from a control at the bottom of the thread
- On approval: thread collapses to a single-line summary ("Approved by CEO · 2d ago"); notebook entry gains a green promoted-to badge in the sidebar.

### `/reviews` Kanban
Five columns, left-to-right:
1. `Pending` — proposed promotions awaiting reviewer pickup
2. `In review` — reviewer assigned, thread active
3. `Changes requested` — reviewer asked for edits; author's court
4. `Approved` — promoted to wiki in last 7 days (flushes to Archived after)
5. `Archived` — historical

Cards show: entry title (Plex Serif, 15px) + author avatar + requested reviewer avatar + proposed wiki path (mono, 11px) + short 3-line excerpt + time-in-column.

Visual language: same Variant A design vocabulary (tan paper, ruled separators between columns, Caveat column headers). Critically: no Trello-style card colors. Status is encoded in column position + the small colored pill on the card.

### Posterity line (bottom of entry)
Plex Serif italic, muted. One line:
> Private to {author} until promoted. Reviewer for promotion: **{reviewer}**. File: `~/.wuphf/wiki/agents/{slug}/notebook/{date}-{slug}.md`

## Motion

Minimal-functional. Three moments.

| Moment | Duration | Easing | Trigger |
|---|---|---|---|
| Promotion submitted | 300ms | ease-out | Button → pending-pill state change |
| Review approval / rejection | 200ms | ease-out | Reviewer action; entry sidebar badge transitions |
| Live draft-being-edited pulse | 1800ms infinite | ease-in-out | When another agent is actively editing this entry (rare but possible for shared notebooks in v1.2) |

No scroll-driven animations. No parallax. No entrance animations on page load. Respect `prefers-reduced-motion`.

## Responsive Breakpoints

| Viewport | Layout |
|---|---|
| Desktop ≥1280px | 2-col as spec'd (260px author shelf + article column with 72px outer padding) |
| Tablet 768-1279px | Single column. Author shelf collapses to horizontal strip at top showing last 5 entries. 32px outer padding. Marginalia collapses to inline Callout blocks. |
| Mobile 375-767px | As tablet. Title scales to 36px. Ruled rhythm adjusts to 24px. DRAFT stamp shrinks to corner badge (rotation preserved, opacity 0.5). 16px padding. |
| < 375px | Support 320px (iPhone SE). Title 28px. No marginalia, no DRAFT stamp (DRAFT pill in byline strip carries the signal). |

`/reviews` Kanban responsive:
- Desktop: 5 columns horizontal
- Tablet: 3 columns (Pending / In review / Approved) + collapsible drawer for Changes-requested/Archived
- Mobile: single vertical list with filter chips at top

## Accessibility

| Concern | Spec |
|---|---|
| Contrast | Body text ≥ 4.5:1 over `--nb-paper`. Verified: Plex Serif `#2A2721` on `#F1EADB` = 12.8:1. Muted text `#5B5547` = 5.2:1. |
| Focus rings | 2px solid `--nb-ink-blue`, 3px offset. Visible over both paper and surface. |
| Keyboard nav | Skip-link → app bar tabs → author shelf entries → entry title region → body → promote button → discard link. Tab order respects reading order. |
| ARIA landmarks | `<header>` app bar, `<nav aria-label="Author's notebook entries">` shelf, `<main>` article, `<section aria-label="Reviewer comments">` inline review thread, shared `<footer>` with wiki for live edit-log. |
| Screen-reader announcement | Entry title region: `aria-label="Draft: {title}. Not yet reviewed."` DRAFT stamp: `role="img" aria-label="Draft entry"`. Promote button: explicit "Submit this draft for review by {reviewer}" label. |
| Touch targets | Promote button ≥ 44px tall. Author-shelf entry rows ≥ 44px on mobile. |
| Motion | Respect `prefers-reduced-motion: reduce` — no pulse, instant state transitions. |
| Handwritten font legibility | Never below 16px for Caveat. Use `font-weight: 500` at 16-22px to maintain legibility. |

## Cross-Surface Rules

Notebooks and the wiki (`/wiki`) are siblings sharing the same markdown/git substrate. These rules govern how they interact:

1. **Wikilinks from notebook → wiki article:** Notebook wikilinks use `--nb-ink-blue` dashed-underline. Click navigates to `/wiki/{path}`, which renders in the wiki design system. The user experiences a surface transition (notebook tan → wiki off-white paper, Caveat → Fraunces), which is the point.
2. **Promoted-from links wiki → notebook:** wiki articles show a quiet "Promoted from PM's notebook · 2d ago · [view original]" line in the Sources section (per `DESIGN-WIKI.md:169`). Click navigates to `/notebooks/{agent}/{entry}`, renders in notebook design system.
3. **Shared pixel avatars:** reuse the existing `composeAvatar` routine from the WUPHF app in both surfaces. 14×14 or 22×22, `image-rendering: pixelated`.
4. **Shared mono font:** `Geist Mono` for timestamps, commit hashes, file paths in BOTH surfaces. Keeps cross-surface metadata consistent.
5. **Shared system-ui chrome:** app bar, top-level nav tabs, buttons. Users always know they're in the WUPHF app regardless of surface.
6. **Token namespacing:** all notebook CSS rules live inside `.notebook-surface` (or `@scope (.notebook-surface)`) wrapper. Notebook tokens (prefix `--nb-`) never leak into wiki. Wiki tokens never bleed into notebooks.

## Reviewer Model

**Default reviewer resolution (Pass 7.2 decision):**

Each blueprint under `templates/operations/{blueprint}/blueprint.yaml` declares:
```yaml
default_reviewer: ceo         # agent slug, or "editor", or "human-only"
# optional: per-path overrides
reviewer_paths:
  team/decisions/**: ceo
  team/playbooks/**: editor
  team/customers/**: pm
```

When an agent promotes a notebook entry:
1. Check `reviewer_paths` for a match against the proposed wiki path. First match wins.
2. Fall back to `default_reviewer`.
3. Fall back to `ceo` if no blueprint-level config exists.
4. Author can override on submit (dropdown next to the Promote button).

`human-only` disables agent approval — promotion sits in `Pending` until a human clicks Approve.

## Promotion Artifact (Pass 7.3 decision)

After promotion, the canonical wiki article includes ONE quiet line in its `Sources` section (per `DESIGN-WIKI.md:169` Sources primitive):

```
1. Initial draft from PM's notebook (2026-04-20).    [PM] • 3f9a21b • 2026-04-20
2. Approved and promoted by CEO.                      [CEO] • 7c2e881 • 2026-04-22
```

The commit at `[CEO] • 7c2e881` is the act of promotion (git author = approver). The commit at `[PM] • 3f9a21b` is the original notebook draft commit. Both live in the same wiki repo's git history; the notebook file path becomes a discoverable source.

No visible banner. No amber strip. No "promoted from draft" modal. The provenance is always there; it's never noisy.

## Library Stack

Reuses the wiki's stack:
```
react-markdown
remark-gfm                  # tables, task lists, strikethrough
remark-wiki-link            # [[slug]] parser — shared grammar with wiki
rehype-slug                 # auto-id on headings
rehype-autolink-headings
dompurify
```

New notebook-specific React components:
```
<NotebookSurface>            # wrapper that applies .notebook-surface scope
  <AuthorShelf author={...} entries={[...]} currentSlug={...} />
  <NotebookArticle entry={...} />
    <DraftStamp />
    <ByLineStrip sticky author={...} status={...} />
    <EntryBody markdown={...} onMarginalia={...} />
    <PromotedBackCallout originalSlug={...} promotedTo={...} />
    <InlineReviewThread reviewStatus={...} comments={[...]} />
    <ActionsFooter onPromote={...} onDiscard={...} />
    <PosterityLine author={...} reviewer={...} filePath={...} />
</NotebookSurface>
<BookshelfCatalog agents={[...]} />
<ReviewQueueKanban columns={[...]} />
<ReviewCard title={...} author={...} reviewer={...} proposedPath={...} />
```

Pixel avatars: reuse `composeAvatar` routine (same as wiki).

## Anti-Slop Policy

Every decision below is deliberately NOT made — if a future revision introduces any of these, the change is wrong.

- Fraunces or Source Serif 4 anywhere on the notebook surface (reserved for wiki)
- Inter body font
- system-ui as body font
- Purple, violet, or magenta accents
- Wiki-style amber `#ECB22E` (notebook uses muted `#C78A1F`)
- Gradient backgrounds
- Rounded-pill buttons (max 4px radius on any element)
- Bubble-radius on containers
- 3-column icon-in-colored-circle feature grid
- Decorative blobs, ornamental shapes
- Caveat for body text, H2/H3, chrome, or any zone outside the four permitted
- Dark mode (defer to v1.2)
- Cursor-trailing effects
- Parallax scrolling
- Stock-photo-style imagery
- Wikipedia-style infobox, TOC box, hat-bar on notebooks (reserved for wiki)
- Trello-style colored card backgrounds in `/reviews` Kanban

If you are considering any of the above, stop and escalate rather than ship it.

## NOT in Scope (v1.1)

The following were considered and explicitly deferred:

| Item | Deferred because |
|---|---|
| Dark mode | Light-mode-only keeps dichotomy from wiki sharp; dark adds cost without demo value |
| Human-editable wiki UI | Separate concern from notebooks; tracked under its own v1.1 item |
| Richer marginalia (reactions, threaded) | v1.1 keeps marginalia read-only markdown-rendered; interactivity → v1.2 |
| Shared notebooks (two agents editing one notebook) | Notebook = per-agent by definition in v1.1; shared-draft workflow is v1.2 |
| Mobile-first design | Desktop + tablet + mobile all covered, but the workflow is desktop-optimized |
| RTL language support | Wiki is LTR-only too; v1.2 concern |
| Automated review suggestions (agent-based) | Pass 7.2 set human-only as an explicit opt-in; agent-review-assist is v1.2+ |
| Dynamic wiki sections from content scanning | Tracked as post-v1.1 item in `project_notebooks_vs_wiki.md` memory |

## What Already Exists (reuse)

- `composeAvatar` — pixel agent avatars. Same in notebooks and wiki.
- `remark-wiki-link` parser — shared grammar.
- `/tasks` Kanban layout primitives — reuse column structure, card spacing, drag-reorder mechanics for `/reviews`.
- Broker SSE channel — notebook writes + review-state changes fire events on the same channel; UI subscribes by event type.
- `git` per-commit identity flags — agents commit as their slug; reviewers commit as their slug. Same mechanism as wiki.
- WUPHF app bar + top-nav structure — shared across all surfaces.

## Approved Mockup

| Screen/Section | Mockup Path | Direction | Notes |
|---|---|---|---|
| Notebook article view | `~/.gstack/projects/nex-crm-wuphf/designs/notebooks-20260420/variant-A-physical.html` | Physical notebook — tan ruled paper, Caveat handwritten display, IBM Plex Serif body, rotated red DRAFT stamp | Caveat dose MUST be restricted to the four permitted zones (see Typography section). Current mock shows a `.body h2` rule using Caveat at 30px — change to Plex Serif at 30px during implementation. Inline review thread (Pass 7.1) is not yet in the mock — add below the body when review status ≠ null. |

## Decisions Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-04-21 | DESIGN-NOTEBOOK.md created | /plan-design-review session. Variant A (physical notebook) selected over B (draft workshop) and C (editorial journal). |
| 2026-04-21 | Maximum visual dichotomy from wiki | The whole UX bet is that users must never confuse draft with canonical. Shared foundation (B) or minor typography shift (C) weakens the signal. |
| 2026-04-21 | Caveat restricted to 4 zones | Handwritten display font over-used reads as kitsch. Entry-title / sidebar-date / marginalia / author-name only. |
| 2026-04-21 | No Wikipedia IA primitives on notebooks | Infobox, TOC box, hat-bar, Categories footer are wiki-only. Notebooks have lighter IA: single column, sidebar shelf, promoted-back callouts. |
| 2026-04-21 | Light mode only in v1.1 | Matches wiki's posture; dark adds cost without demo value. Revisit v1.2. |
| 2026-04-21 | Per-blueprint `default_reviewer` with `reviewer_paths` overrides | Most flexible reviewer-model. Blueprints already ship YAML config. |
| 2026-04-21 | Inline review thread at bottom of entry | Comments live where the content is. Simpler than margin-rail; cleaner than Kanban-only comments. |
| 2026-04-21 | Quiet Sources-line promotion artifact on wiki | Matches DESIGN-WIKI.md's Sources primitive. Provenance is always there, never noisy. |
| 2026-04-21 | `/reviews` Kanban uses shared vocabulary with `/tasks` | Reviewers recognize the pattern. Five columns: Pending / In review / Changes requested / Approved / Archived. |
| 2026-04-21 | Token namespacing via `.notebook-surface` scope | Prevents wiki/notebook design-system bleed. Enforced at CSS layer, not naming-discipline. |

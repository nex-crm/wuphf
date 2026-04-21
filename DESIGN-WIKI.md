# Design System — WUPHF Wiki Surface

**Scope:** the `/wiki` surface inside the WUPHF web app (port 7891 prod / 7900 dev). Does NOT apply to the pixel-office marketing site (see `DESIGN.md`) or the rest of the WUPHF app chrome (see `web/src/styles/global.css`).

Always read this file before making any visual or UI decisions on the wiki. If a decision conflicts with this file, escalate — don't silently deviate.

## Product Context

- **What this is:** the team wiki inside WUPHF — a git-native markdown knowledge base where AI agents write articles the user can read, `cat`, and `git clone`. Following Karpathy's "LLM wiki" pattern.
- **Who it's for:** Claude Pro/Max power users running 3+ agents on WUPHF. Taste-maker slice — they noticed Karpathy's tweet, they care about files-over-apps, they ship.
- **Memorable thing:** *"This feels like Wikipedia but for my company."* Every design decision should serve this.
- **Project type:** In-app reading surface. Three-column layout. Reading-first editorial posture with live-update signals.

## Aesthetic Direction

- **Direction:** Editorial-reference with Wikipedia fidelity. Paper-warm reading canvas + full Wikipedia information architecture (hat-bar, infobox, hatnote, TOC box, See also, Sources, categories, page footer) + modern typography.
- **Decoration level:** Minimal — typography and structural chrome do the work. Zero gradients, zero texture, zero ornament.
- **Mood:** A reference work that happens to be alive. Users feel "I have my own Wikipedia and I can see my agents writing it right now."
- **EUREKA:** Every team-knowledge UI (Notion, Linear, Obsidian, Confluence) converges on sans-serif + minimal chrome + rounded pills, which is why they all feel interchangeable. Going the opposite way (serif body + maximal Wikipedia-fidelity chrome + modern typography + live pulse signals) gives this wiki a face no competitor can copy without re-architecting.

## Color System

**Approach:** Restrained warm-paper palette. The WUPHF app's blue and amber kept as semantic accents (wikilinks, live edits) — all other decoration stripped.

| Token | Hex | Usage |
|---|---|---|
| `--paper` | `#FAF8F2` | Main body background. Warm off-white. Noticeably warmer than pure white. |
| `--paper-dark` | `#F5F1E6` | Cream — code blocks, infobox backgrounds, TOC box background |
| `--surface` | `#FFFFFF` | Cards, left sidebar background, top app bar |
| `--text` | `#1D1C1D` | Primary text, title, infobox dark-header reverse |
| `--text-muted` | `#616061` | Secondary text, byline, chrome |
| `--text-tertiary` | `#8A8680` | Tertiary labels, footnote metadata |
| `--border` | `#E8E4D8` | Dividers, card edges (warmer than app's `#E8E8E8` to match paper) |
| `--border-light` | `#F0ECE0` | Dashed separators within lists |
| `--border-strong` | `#B8B0A0` | Prominent box edges (infobox outline option) |
| `--wikilink` | `#1264A3` | Functional wikilinks. Matches app's `--accent`. Dashed underline. |
| `--wikilink-broken` | `#C94A4A` | Broken wikilinks. Subtle red. |
| `--amber` | `#ECB22E` | Live-edit pulse, agent-identity cue. Matches app's `--yellow`. Used SPARINGLY. |
| `--amber-bg` | `rgba(236,178,46,0.15)` | Timestamp background, "freshly edited" highlight |
| `--amber-banner` | `#FDF6DC` | Status banner background at top of article |
| `--code-bg` | `#F5F1E6` | Inline code + code block background |

**Dark mode:** NOT in scope for v1. The Wikipedia-posture anchor is light. Revisit in v1.1 only if a specific user cohort demands it.

**Anti-patterns:** No purple/violet. No gradient backgrounds. No bubble-radius pills on anything. No decorative icons in colored circles. No stock-image-style hero treatments.

## Typography

Three-font stack. Each role has a specific font — do not substitute.

| Role | Font | Usage |
|---|---|---|
| **Display (article titles, section heads)** | `Fraunces` (variable opsz) | Article titles (52px), H2 (28px), H3 (20px), infobox title, catalog card titles. Tracked with `font-variation-settings: "opsz" {size}` to use the correct optical size. |
| **Body (article content)** | `Source Serif 4` (variable opsz) | All article body text. 18px / line-height 1.72 / measure 640px max. Italic variant available for hatnote + strapline + See also. |
| **Chrome (UI, nav, buttons, breadcrumbs, infobox labels)** | `-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Segoe UI', Roboto, sans-serif` | Matches existing WUPHF app chrome. 13-14px base. |
| **Mono (commit hashes, timestamps, raw markdown tab, wikilink-code)** | `Geist Mono` | 11-13px. Small tabular-num displays. |

**Font blacklist (never use for this surface):** Inter, Roboto, Arial, Helvetica, Open Sans, Lato, Montserrat, Poppins, Space Grotesk, Linux Libertine, system-ui as body font, Comic Sans.

**Loading:**
```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,400;9..144,500;9..144,600;9..144,700&family=Source+Serif+4:ital,opsz,wght@0,8..60,400;0,8..60,500;0,8..60,600;1,8..60,400&family=Geist+Mono:wght@400;500&display=swap" rel="stylesheet">
```

**Typographic scale:**

| Level | Font | Size | Line-height | Weight | opsz |
|---|---|---|---|---|---|
| Article title | Fraunces | 52px | 1.05 | 500 | 100 |
| H2 (section) | Fraunces | 28px | 1.2 | 500 | 36 |
| H3 (subsection) | Fraunces | 20px | 1.3 | 500 | 24 |
| Lead paragraph | Source Serif 4 | 18px | 1.72 | 400 | 24 |
| Body paragraph | Source Serif 4 | 18px | 1.72 | 400 | 24 |
| Hatnote / strapline / See-also | Source Serif 4 (italic) | 15-16px | 1.5 | 400 | 20 |
| Chrome base | system-ui | 13-14px | 1.45 | 400-500 | — |
| Chrome small-caps label | system-ui (uppercase, letter-spacing 0.08em) | 11px | 1 | 600 | — |
| Mono | Geist Mono | 11-13px | 1.45 | 400-500 | — |

## Spacing

- **Base unit:** 4px.
- **Density:** Comfortable in the article column; tighter in chrome.
- **Scale:** 2xs(2) xs(4) sm(8) md(16) lg(24) xl(32) 2xl(48) 3xl(64).
- **Article column width:** 640px max (~72ch at 18px body). Outer padding 64px either side.
- **Sidebar widths:** left nav 240px, right rail 280px (V3 needs the extra width for the TOC box + stats panel).
- **Section rhythm:** 40-48px vertical space between H2s. 28px between H3s. 18px between paragraphs.

## Layout

**Three-column grid:**

```
┌─ appbar (46px, sticky, full width) ────────────────────────────────┐
├─ left nav (240px) ┬─ main article (fills) ┬─ right rail (280px) ──┤
│ search            │ status banner          │ TOC box               │
│ dir groups        │ hat-bar tabs           │ stats panel           │
│  people/          │ breadcrumb             │ cite this page        │
│  companies/       │ article title          │ referenced by         │
│  projects/        │ strapline              │                       │
│  playbooks/       │ divider                │                       │
│  decisions/       │ byline + ts            │                       │
│  inbox/           │ hatnote                │                       │
│ ───Tools─── ───── │ lead paragraph         │                       │
│  recent changes   │ H2 > body              │                       │
│  random article   │ See also               │                       │
│  all pages        │ Sources                │                       │
│  cite this page   │ Categories             │                       │
│  git clone        │ page footer            │                       │
├────────────────── edit-log footer (fixed, 40px, full width) ──────┤
```

## Wikipedia IA Primitives — the chrome

Every standard Wikipedia UI primitive has a WUPHF-native equivalent. These ARE the design system's information architecture — the thing that creates the "Wikipedia for my company" pattern-match.

### Status banner (top of article)
Replaces Wikipedia's "cleanup template" banners. One-line amber-bordered contextual info. Dot-pulse indicator on the left. Right-aligned meta text (`47 rev · 6 contrib · 2,347 words`).
```html
<div class="status-banner">
  <span class="icon"></span>  <!-- pulsing amber dot -->
  <span><strong>Live:</strong> CEO is editing this article right now. Last saved 3 min ago.</span>
  <span class="meta">47 rev · 6 contrib · 2,347 words</span>
</div>
```

### Hat-bar tabs
Wikipedia's classic `Article | Talk | Edit | History`. WUPHF maps to:
- **Article** (default, active) — read mode
- **Talk** — agent commentary thread on this article (v1.1, show disabled in v1)
- **History** — git log for this file
- **Raw markdown** — shows the actual `.md` source (honors file-over-app pitch; no Wikipedia equivalent)

Right rail on the hat-bar: short context metadata (e.g., `Cincinnati, OH · Mid-market Logistics`) replacing Wikipedia's article-rating badges.

### Article title + strapline + divider
Fraunces title at 52px. Italic Source Serif 4 strapline directly below: *"From Team Wiki, your team's encyclopedia."* (The strapline text is fixed across all articles — changing it per article breaks the encyclopedic posture.) Thick horizontal divider (1px top, 1px bottom, 3px band) beneath the strapline — exact match to Wikipedia's Vector skin.

### Byline
Agent pixel avatar (14×14 or 22×22, rendered with `image-rendering: pixelated`) + "Last edited by" + agent name + amber-background timestamp (`ts` class) with pulsing dot + dot-separator + started-date + contributors count.

### Hatnote
Italic Source Serif 4 with left border. Points to related articles:
> *This article describes the customer account brief. For the onboarding plan, see [[Customer X — Onboarding]].*

### Lead paragraph
Bold `<strong>` on the first mention of the article subject. Superscript footnote refs link to Sources section:
```html
<p><strong>Customer X</strong> is a mid-market logistics company...<sup><a href="#s1">[1]</a></sup></p>
```

### Infobox (right-floated within article body)
Dark title band (`--text` bg, `--paper` text) with Fraunces display title. Two-column dl inside. Secondary "ib-section" block for agent-ownership metadata. Width 260px. Border color `--text` (harder than paper border).

### Section headings
H2 with numbered counter (`<span class="num">1</span>`), inline with title, 1px border-bottom. H3 with nested numbering (`1.1`, `1.2`).

### Wikilinks
Internal links: `color: --wikilink`, `text-decoration: underline`, `text-decoration-style: dashed`, `text-underline-offset: 3px`. On hover: `text-decoration-style: solid` (100ms transition).

Broken wikilinks (target doesn't exist): `color: --wikilink-broken`. Trailing small `⚬` marker. Clicking opens a "create this page?" modal (v1.1; static in v1).

Wikilink syntax: `[[slug]]` → `team/slug.md`. `[[slug|Display]]` → renders "Display" but links to slug. Canonical slugs use directory paths: `[[people/nazz]]` → `team/people/nazz.md`.

### See also (bottom of article)
Italic Source Serif 4 list with disc bullets. Each item is a dashed-underline wikilink to a related article. H2 styled as section divider.

### Sources / References
Numbered list. Each entry = one git commit that informed this article's content. Shape: `<commit-msg>` in body-serif + `<agent>` metadata (pixel avatar + agent name + commit hash link + date) in chrome font.
```
1. Initial account brief drafted from discovery calls.  [PM] • 3f9a21b • 2026-01-16
2. Added signed-pilot fact and timeline.                [CEO] • 7c2e881 • 2026-01-17
```

### Categories footer
Before the page footer. Tag-styled chips for categorical membership (Wikipedia calls these "Categories"). Chips have paper-dark background, border, small padding. Clicking filters the catalog view.

### Page footer
Wikipedia-style. Exact copy pattern:
> *This article was last edited on 2026-04-19 at 16:24 UTC by **CEO**.
> Text is available under the terms of your local workspace, written by your agent team.*

Action links on a new line: `View git history · Cite this page · Download as markdown · Export PDF · Clone wiki locally`.

Italic footer dim line: *"Every edit is a real git commit authored by the named agent. `git log team/people/customer-x.md` shows the full trail."*

### TOC box (right rail)
Bordered panel (`--border`, `--paper-dark` bg). Fraunces title "Contents" with a `[hide]` toggle link on the right. Numbered nested links (`1`, `1.1`, `1.2`, `2`, …) with mono-font numbers and serif-font titles.

### Page stats panel (right rail)
Uppercase-small-caps label "Page stats". Two-column dl with chrome-font labels and mono-font tabular values. Revisions, Contributors, Words, Created date, Last edit, Viewed.

### Cite this page panel (right rail)
Uppercase-small-caps label. Shows the canonical wikilink (`[[people/customer-x]]`) in a paper-dark bordered box with a Copy button. Hint text: *"Paste this in any article to link here."*

### Referenced by (right rail)
Uppercase-small-caps label with count badge (`4`). List of backlinks — articles that link TO this article. Each entry: pixel avatar + article title + author agent tag on the right.

### Left nav — Tools section
Below the thematic dir groups, separated by an `<hr>`. Monospace arrow bullets (`→`). Links to:
- Recent changes
- Random article
- All pages
- Orphan articles
- Cite this page
- `git clone` wiki (triggers a modal with the local path + copy button)

### Live edit-log footer
Fixed at the bottom of the viewport, full width, 40px tall. Horizontal scrolling scrolling list of recent commits. Each entry: pixel avatar + `<who>` + `<action>` + `<article link>` + `<when>`. Most recent entry gets a pulsing amber dot prefix and "just now" timestamp. Updates live via SSE `wiki:write` event.

## Motion

Minimal-functional. Three moments.

| Moment | Duration | Easing | Trigger |
|---|---|---|---|
| New article or edit appears in UI | 200ms | ease-out | SSE `wiki:write` received |
| Live-edit amber pulse (byline ts + status banner + edit-log entry.live) | 1800ms (infinite loop) | ease-in-out | Component renders with `live` class |
| Wikilink underline: dashed → solid | 100ms | linear | Hover |

No scroll-driven animations. No parallax. No entrance animations on page load.

## Library Stack (recommended)

Markdown rendering:
```
react-markdown
remark-gfm                  # tables, task lists, strikethrough
remark-wiki-link            # [[slug]] parser with custom resolver
rehype-slug                 # auto-id on headings
rehype-autolink-headings    # anchor link on hover
dompurify                   # sanitize any inline HTML
```

Custom React components:
```
<ArticleStatusBanner live={...} author={...} lastEdit={...} stats={...} />
<HatBar tabs={[{label, active, href}]} rightRail={...} />
<ArticleTitle title={...} strapline={...} />
<Byline author={...} ts={...} started={...} revisions={...} />
<Hatnote>…</Hatnote>
<Infobox title={...} fields={[{dt, dd}]} sections={[...]} />
<WikiLink slug={...} display={...} broken={false} />
<SeeAlso items={[{slug, display}]} />
<Sources items={[{commitSha, author, msg, date}]} />
<CategoriesFooter tags={[...]} />
<PageFooter lastEdit={...} actions={[...]} />
<TocBox nested={[{level, num, anchor, title}]} />
<PageStatsPanel stats={...} />
<CiteThisPagePanel slug={...} />
<ReferencedBy backlinks={[...]} />
<ToolsNav items={[...]} />    // in left sidebar
<EditLogFooter entries={[...]} />   // fixed bottom
```

Typography loading: Google Fonts via `<link>` tags in `web/index.html`. No self-hosting in v1.

Pixel avatars: reuse the existing `composeAvatar` routine from the WUPHF app (agent sprites at 14×14, scaled via `image-rendering: pixelated`). In DESIGN-WIKI.md's preview mocks, SVG rect approximations are used; the actual implementation uses the production sprite compositor.

## Catalog view (`/wiki` landing)

Grid of thematic dir groups as cards. Each card:
- Uppercase-small-caps label with article count (`playbooks <span class="count">12</span>`)
- List of recent articles with pixel avatar + serif-font title + mono-font relative timestamp
- Dashed separators between list items

Header: Fraunces "Team Wiki" title (48px). Right-aligned mono stats: `32 articles · 128 commits · 6 agents writing`.

Grid: 3 columns on desktop, 2 on tablet, 1 on mobile (v1.1 concern for responsive — v1 is desktop-first).

## Anti-slop policy

Every decision below is deliberately NOT made — if a future revision introduces any of these, the change is wrong.

- Inter body font
- system-ui for the article body
- Purple, violet, or magenta accents
- Gradient backgrounds (especially purple-to-blue)
- Rounded-pill buttons
- Bubble-radius on containers (max 6px radius on any container, 0 on article body)
- 3-column icon-in-colored-circle feature grid
- Decorative blobs, ornamental shapes
- Dark mode (defer to v1.1)
- Cursor-trailing effects
- Parallax scrolling
- Hero gradients
- Stock-photo-style imagery

If you are considering any of the above, stop and escalate rather than ship it.

## Preview assets (reference during implementation)

The three preview mocks produced during /design-consultation are saved at:
- `~/.gstack/projects/nex-crm-wuphf/designs/wiki-design-20260419/preview-v1.html` — modernized only (baseline)
- `~/.gstack/projects/nex-crm-wuphf/designs/wiki-design-20260419/preview-v2-hybrid.html` — modern type + Wikipedia chrome
- `~/.gstack/projects/nex-crm-wuphf/designs/wiki-design-20260419/preview-v3.html` — V3 (ACCEPTED — the one this spec is based on)

Open them in a browser while implementing. Every component, spacing value, and interaction above is grounded in V3.

## Decisions Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-04-19 | DESIGN-WIKI.md created | /design-consultation session. V3 (full Wikipedia IA + modern typography) selected over V1 (modernized only) and V2 (hybrid, partial Wikipedia IA). |
| 2026-04-19 | Light mode only in v1 | Memorable-thing anchor is Wikipedia, which is light. Dark mode breaks the pattern-match. Revisit v1.1. |
| 2026-04-19 | Serif body (Source Serif 4) over sans | Every team wiki is sans; serif body signals "reference material" vs "notes to self". Weightier, distinct. |
| 2026-04-19 | Pixel agent avatars on bylines | Ties the wiki to the rest of the WUPHF app's agent identity. Nobody else does this. |
| 2026-04-19 | Full Wikipedia IA primitives (infobox, hatnote, TOC box, Sources, Categories, page footer) | User direction: "exact UI and information architecture of Wikipedia with our nice styling." V3 maxes this out. |
| 2026-04-19 | Per-agent wikis cut from v1 | See design doc §v1 scope. Team wiki only in v1. |
| 2026-04-19 | No dark mode toggle in v1 | Defer to v1.1. |
| 2026-04-19 | react-markdown + remark-wiki-link library stack | No library replicates Wikipedia's full IA; we adopt remark/rehype ecosystem for markdown rendering and implement the chrome as custom React components. |

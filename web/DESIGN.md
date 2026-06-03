# WUPHF Web — Design Notes

Living seed for the web app's design system. Add sections as new surfaces
land. Keep entries tight; deep specs live in `~/.gstack/projects/...`
design docs.

## Design tokens

CSS custom properties live in `web/src/styles/global.css` under `:root`.
Themes override token values in `web/public/themes/*.css`. Never hardcode
colors, type, or spacing in components — read from a token.

Token families today:

- Color ramps: `--neutral-*`, `--tertiary-*`, `--olive-*`, `--cyan-*`,
  `--success-*`, `--error-*`, `--warning-*`.
- Semantic aliases: `--bg`, `--bg-card`, `--text`, `--text-secondary`,
  `--text-tertiary`, `--text-disabled`, `--accent`, `--border*`,
  `--green*`, `--red*`, `--yellow*`, `--blue*`.
- Typography: `--font-sans`, `--font-serif`, `--font-mono`, `--font-logo`.
- Radius: `--radius-sm|md|lg|xl|full`.
- Bubble (agent rail): see below.

## Agent rail event pills

Per-agent live state surfaces inline on the agent rail row, replacing the
previous `.sidebar-agent-task` text line. One pill per agent, anchored to
the row's secondary text slot. The pill never causes row reflow.

### Visual states

| State    | Look                                                                     | When                                                                               |
| -------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- |
| halo     | Brief drop-shadow glow (`--bubble-halo-radius`) in state color           | Within ~600ms of a new event arriving                                              |
| holding  | Full opacity, normal weight                                              | Within hold window — routine 60s, milestone 120s                                   |
| dim      | Opacity 0.7                                                              | Hold expired, still in the 60s dim window — routine 60–120s, milestone 120–180s   |
| idle     | Opacity 0.5, italic                                                      | After the dim window — routine >120s, milestone >180s; copy from `officeIdleDictionary.ts` |
| stuck    | 1px `--bubble-stuck` border, `--bubble-stuck` text, weight 600           | Backend emits `kind=stuck`; user cannot dismiss client-side                        |

### Motion budget

- Halo decay: `--bubble-halo-duration` (600ms).
- All transitions use compositor-only properties — `opacity`, `filter`,
  `transform`. Never animate width/height/top/left/border-width.
- New event crossfade: `--bubble-text-crossfade` (180ms).
- Idle dim transition: `--bubble-idle-dim-duration` (600ms).

### Reduced motion

Under `prefers-reduced-motion: reduce`:

- All transitions become `none`. State changes snap instantly.
- The halo `filter` is suppressed.
- The stuck-state border still renders — it is structural chrome, not
  motion, and is the contrast signal that survives the reduced-motion path.

### Idle copy

Source: `web/src/lib/officeIdleDictionary.ts`. Lookup order:

1. Slug overrides for canonical agents (e.g., `tess`, `ava`, `sam`).
2. Role table — `engineer`, `designer`, `pm`, `devops`, `marketing` and
   their aliases.
3. Generalist fallback — never returns empty.

Copy rotates ~every 12s based on `idleMs` so a long idle does not stare
at the same line forever.

### Pill content rules

- SSE event snapshots win. The pill renders `snapshot.activity` (the
  short scannable headline), falling back to `snapshot.detail` only
  when activity is absent. The richer `detail` text is reserved for
  the planned Tier 2 hover peek card.
- `member.task` is a one-shot initial-paint seed only — used before the
  first SSE event arrives.
- When no event has ever arrived and the agent is idle, the pill renders
  Office-voice idle copy.

## Tier 2 hover peek

Popover card surfacing richer agent state on hover/long-press/Space.
Rendered by `AgentEventPeek.tsx` via `createPortal` to `document.body`
so it floats above the sidebar without causing row reflow.

### Anchor and positioning

| Rule | Detail |
| ---- | ------ |
| Anchor | `anchorRef` — the rail row element passed by the opener (Slice D) |
| Default position | 8px to the RIGHT of the anchor's right edge, top-aligned to anchor top |
| Viewport clamp | If `anchorRight + 8 + 320 > window.innerWidth`, flip to left of anchor |
| Recalculate on | `open` flip, `resize`, `scroll` (capture phase) |
| Portal target | `document.body`; `z-index: 60` (above rail popovers at 40) |

### Structure

```text
.sidebar-agent-peek [role="dialog"]
├── .sidebar-agent-peek-header
│   ├── .sidebar-agent-peek-avatar       24×24 CSS-driven circle, first letter
│   ├── .sidebar-agent-peek-identity
│   │   ├── #peek-name-{slug}            agent name (aria-labelledby target)
│   │   └── .sidebar-agent-peek-role     optional role string
│   └── .sidebar-agent-peek-blocked-chip "BLOCKED" — stuck variant only
├── .sidebar-agent-peek-state-row        state chip + relative time
├── #peek-current-{slug}                 detail block — omitted when detail === activity
│   .sidebar-agent-peek-detail
├── .sidebar-agent-peek-recent-section   omitted when list would be empty
│   ├── .sidebar-agent-peek-recent-header "RECENT"
│   └── .sidebar-agent-peek-recent (ul)  ≤6 entries; stuck pin at top
└── .sidebar-agent-peek-footer           "⏎ Open workspace" — plain text, not a button
```

### Motion budget

| Property | Value |
| -------- | ----- |
| Open animation | `peek-in` — 0.14s ease-out, `opacity` 0→1 + `translateY(6px)` → 0 |
| Compositor-only | Only `opacity` and `transform` animated. Never width/height/top/left |
| Clock | 1Hz `setInterval` owned by the peek; mounts/unmounts with it |

### Reduced motion

Under `prefers-reduced-motion: reduce`: `animation: none`, `transition:
none`. The stuck-state border and `--bubble-stuck` color still render —
they are structural chrome, not motion.

### Keyboard

| Key | Action |
| --- | ------ |
| `Escape` | `onClose()` — strips listener on close/unmount |
| `Enter` | `onOpenWorkspace()` |
| Outside `mousedown` | `onClose()` — ignores clicks inside dialog or inside anchor |

Focus moves into the dialog (`tabIndex={-1}`) when `open` flips true.
Focus return on close is managed by Slice D's chevron toggle path, not
by the peek itself.

### Stuck variant

`data-stuck="true"` on the root element. Border color swaps to
`--bubble-stuck`. `BLOCKED` chip appears in the header. The current
stuck snapshot is pinned as the first entry in the RECENT list with a
`BLOCKED:` prefix (a duplicate entry — the full thought block above and
the pin below serve different scan modes).

### Footer rationale (not a button)

The footer line `"⏎ Open workspace"` is a plain `<div>`, not a
`<button>`. Making it a button would create a second interactive target
inside the dialog, splitting keyboard intent between `Enter` (global
peek action) and a button press. `Enter` on the focused dialog fires
`onOpenWorkspace` directly; the footer is a visible affordance hint, not
a clickable control. Screen readers read it as static text, which is
correct — the action hint is already announced in the dialog label.

### Single-instance discipline

At most one peek is open at a time. Opening a second peek closes the
first. Managed by Slice D's opener state; the peek component itself has
no instance registry.

## Onboarding office tour

Post-onboarding orientation: a full-viewport modal (`--z-modal`) that
overlays the real office once `onboarded` flips true. Auto-opens once per
browser (`localStorage: wuphf.office-tour-done`), replayable from Help via
the `wuphf:show-office-tour` event. Files: `components/onboarding/tour/*`,
styles `office-tour.css` (shell) + `office-tour-slides.css` (slides).

### Composition

Editorial, not card-pile. Each slide is a two-column grid
(`minmax(0,1fr) minmax(0,1.15fr)`): copy left, a live mock of the real
surface right (mock sidebar, agent card, composer fan-out, wiki tree). It
collapses to one column at `≤760px`. Headlines use `--font-logo` (serif);
slugs and typed commands use `--font-mono`. The footer is a `1fr auto 1fr`
grid so the progress dots stay optically centered while Back / Next / Finish
size to their labels.

### Color + theme

Token-only, verified in `nex`, `nex-dark`, and `noir-gold`. The overlay
carries an atmosphere wash:
`radial-gradient(... color-mix(in srgb, var(--accent) N%, transparent) ...)`
layered over a flat `background-color: var(--bg)` fallback. Completion ticks
and heartbeats use `--green` / `--green-bg`.

### Motion budget

| Element | Animation |
| ------- | --------- |
| Slide morph | `document.startViewTransition` when available, synchronous fallback otherwise, never under reduced motion |
| Entrances (copy, stage, cards, ticks) | `transform`/`opacity`, `cubic-bezier(0.22, 1, 0.36, 1)`, replays per slide via `data-active` |
| Composer typewriter | `clip-path: inset(0 100% 0 0)` → `inset(0)` (paint, not layout). Caret follows via `transform: translateX(var(--typed-ch))`, `--typed-ch` set inline from the command length so it never goes stale |
| Heartbeat / caret blink | `opacity` only |

Under `prefers-reduced-motion: reduce`: every entrance lands on its final
state, the typed command shows in full, the caret rests at the line end.

### Accessibility

- `role="dialog"` + `aria-modal` + `aria-label`; focus moves in on open,
  returns on close, and is **trapped** while open (Tab cycles within the
  overlay using the shared `FOCUSABLE_SELECTOR`, matching `SidePanel`).
- Esc skips, `←`/`→` navigate slides. The dot rail is one labeled
  `role="img"` ("Step N of M"); individual dots and the step marker are
  `aria-hidden`. The mock surfaces are `aria-hidden` (decorative).
- A visually-hidden live region announces the active slide headline.
- Coarse pointers get 44px minimum hit areas on skip + footer buttons.

## Getting-started checklist

Dismissible "Settle into your office" panel (`GettingStartedChecklist.tsx`)
shown to onboarded-but-not-settled users; renders the dormant Go
`DefaultChecklist`. A `--green` `role="progressbar"` fill visualizes the
N/total count. Completion is exposed to screen readers with `.sr-only`
"Done." text, never by strikethrough alone (the tick is `aria-hidden`).
External actions open in a new tab with `rel="noopener noreferrer"`;
"Connect a GitHub repo" is internal nav to the seeded how-to wiki page, not
the source repo. Action controls reach 44px on coarse pointers.

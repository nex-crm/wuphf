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
| dim      | Opacity 0.7                                                              | Hold expired but within 120s of last event                                         |
| idle     | Opacity 0.5, italic                                                      | More than 120s since last event; copy comes from `officeIdleDictionary.ts`         |
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

- SSE event snapshots win. The pill renders `snapshot.detail`, falling
  back to `snapshot.activity`.
- `member.task` is a one-shot initial-paint seed only — used before the
  first SSE event arrives.
- When no event has ever arrived and the agent is idle, the pill renders
  Office-voice idle copy.

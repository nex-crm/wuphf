# WUPHF Paper-Manual Artifact Style

Rich HTML artifacts should feel like old mathematics and physics books printed
on real paper, not generic SaaS dashboards or digital spec sheets. Use this
style when an agent produces an explainer, plan, incident room, review packet,
comparison grid, or tuning surface.

## Visual Language

- Warm paper background with subtle grain, like aged textbook stock.
- Near-black serif body copy for dense reading.
- Monospaced labels for artifact metadata, figure numbers, inputs, outputs, and
  source/trust information.
- Use the Making Software cobalt family as the primary artifact ink. Known
  sampled values:
  - `--ms-cobalt-950: oklch(21% .034 264.665)`
  - `--ms-cobalt-700: oklch(48.8% .243 264.376)`
  - `--ms-cobalt-600: oklch(50.58% .2886 264.84)` / `rgb(19, 66, 255)`
  - `--ms-cobalt-500: oklch(63.79% .1899 269.89)`
  - `--ms-cobalt-400: oklch(76.12% .1218 272.4)`
  - `--ms-cobalt-300: oklch(83.76% .0774 273.32)`
  - `--ms-cobalt-100: oklch(94.22% .0275 274.66)`
  - `--ms-cobalt-50: oklch(97.05% .0054 274.97)`
- Use the cobalt family for figure strokes, mono labels, active controls,
  selected tabs, and diagram fills. The page should still feel like real paper,
  not a digital dashboard.
- Secondary colors should be muted complements: ochre for warning or medium
  severity, oxide red for high risk, and archival green for resolved/low states.
- Inline SVG diagrams should feel like textbook figures: construction lines,
  axis ticks, measured annotations, equations, figure captions, and faint grids
  inside figure plates.
- Flat layouts with hairline borders, dotted separators, and restrained
  controls.
- Tables and lists should read like reference material: compact, scannable, and
  precise.

## Interaction Pattern

- Controls should feel like instrumentation printed into the page: rulers,
  sliders, tabs, editable lab notes, small copy/export buttons, and state
  readouts.
- Every interactive artifact should end with a useful handoff control, such as
  `Copy prompt`, `Copy update`, `Copy diff`, or `Export JSON`.
- Prefer diagrams that clarify the decision or flow over decorative imagery.

## Boundaries

- Keep artifacts original to WUPHF. Do not copy external logos, illustrations,
  exact type assets, or brand-specific layouts.
- Keep artifacts self-contained: inline CSS/JS only, no external images,
  scripts, fonts, or network fetches.
- Markdown remains the durable source summary. HTML carries the visual and
  interactive review surface.

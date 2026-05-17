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
- Graphite, sepia, or faded-ink line art. Color is secondary; the feel should
  come from paper, typesetting, and diagrams.
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

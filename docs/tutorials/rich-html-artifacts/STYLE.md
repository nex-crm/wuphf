# WUPHF Technical-Manual Artifact Style

Rich HTML artifacts should feel like clear technical manuals, not generic SaaS
dashboards. Use this style when an agent produces an explainer, plan, incident
room, review packet, comparison grid, or tuning surface.

## Visual Language

- Warm off-white paper background.
- Near-black serif body copy for dense reading.
- Monospaced labels for artifact metadata, figure numbers, inputs, outputs, and
  source/trust information.
- Technical blue line art, inline SVG diagrams, ruler ticks, dotted grids, and
  small figure captions.
- Flat layouts with hairline borders, dotted separators, and restrained
  controls.
- Tables and lists should read like reference material: compact, scannable, and
  precise.

## Interaction Pattern

- Controls should feel like instrumentation: sliders, tabs, editable prompts,
  small copy/export buttons, and state readouts.
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

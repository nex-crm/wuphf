# Surface coverage

> WUPHF has two human-facing surfaces: the **TUI** (`cmd/wuphf/`) and the **web UI** (`web/`). They are *not* meant to be at parity. Some features are terminal-shaped, some are pixel-shaped, and some are intentionally one-sided.

This doc is the canonical map. If you ship a feature on one surface, update the table and decide — *with reasoning* — whether the other surface gets it.

## Status legend

- `✓` — feature exists and is supported on this surface
- `—` — feature does not exist on this surface
- `⏳` — planned, tracked
- `⊘` — intentionally not on this surface (see Reasoning)

## Feature × Surface matrix

| Feature | TUI | Web | Reasoning |
|---|---|---|---|
| Office channels | ✓ | ✓ | Core; bidirectional. |
| Inbox / Outbox | ✓ | ✓ | Core. |
| 1:1 DM mode | ✓ | ✓ | Core. |
| Threads | ✓ | ✓ | Core. |
| Tasks | ✓ | ✓ | Core. |
| Requests / Approvals | ✓ | ✓ | Core; both surfaces show pending interview gates. |
| Calendar | ✓ | ✓ | Day/week toggle on both. |
| Artifacts | ✓ | ✓ | Web has richer rendering of structured artifacts; TUI shows summaries. |
| Skills / Playbooks | ✓ | ✓ | Core. |
| Policies | ✓ | ✓ | Core. |
| Settings | partial | ✓ | TUI has minimal integration UI; richer config (provider keys, integrations) lives in web. |
| Wiki / Codex | — | ✓ | ⏳ TUI version is text+search shaped → terminal-friendly. Tracked for Phase 8. |
| HealthCheck | — | ✓ | ⏳ TUI version `top`-style for broker health → terminal-friendly. Tracked for Phase 8. |
| Notebook | — | ✓ | Pending decision: if data model is structured-text, ⏳ TUI; if rich-text+embeds, ⊘. |
| Graph / Insights | — | ✓ | ⊘ Pixel-shaped (decision trees, signal graphs). ASCII-art port would rot. |
| Receipts | — | ✓ | ⊘ Image-upload + OCR; not terminal-shaped. |
| Onboarding wizard | partial | ✓ | TUI has bootstrap commands; rich step-by-step wizard is web-only. ⊘ on TUI. |
| Local LLM provider config | — | ✓ | ⊘ Provider testing requires browser-side fetch + provider URL probing; not terminal-shaped. |

## Decision rules for new features

When adding a feature to either surface, choose one of the following dispositions and write it into the matrix:

- **Both** — feature is core to the product. Land Go-side first; the web UI consumes the API. TUI consumes the same API. Same rendering contract via the shared `internal/team/api/` types (Phase 5).
- **Web-only-by-design** — the feature is fundamentally pixel-shaped (visualization, image, rich text with embeds, drag-and-drop, multi-pane). Document why in the matrix. Don't speculatively port.
- **TUI-only-by-design** — the feature is fundamentally text/keyboard-shaped (single-screen `top`-style monitors, command-line workflows, keyboard-first power-user views).
- **Asymmetric on purpose** — the feature exists on both but with intentionally different UX (e.g., Settings: minimal in TUI, comprehensive in web). Document the asymmetry.

## When to update this doc

- Adding a new top-level feature → add a row, fill all columns.
- Promoting a `⏳` to `✓` → update the row and link the PR.
- Reclassifying (e.g., deciding Graph could land in TUI as a sparkline summary) → update with reasoning. Past decisions can be revisited.

## What this doc is not

- A roadmap. We don't promise the `⏳` items will land.
- A backlog. Tracked tickets live in the issue tracker.
- A justification for parity-for-parity's-sake. The point is *intentional* asymmetry.

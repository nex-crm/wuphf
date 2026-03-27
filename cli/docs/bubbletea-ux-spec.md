# Bubbletea UX Spec — Reference for Ink Parity

## Layout
```
┌─ Header (animated banner) ─────────────────────────────┐
│  wuphf-cli · powered by wuphf.ai                           │
├─ Content (scrollable viewport) ────────────────────────┤
│  [Tables, pickers, details, charts, etc.]              │
├─ Status Bar ───────────────────────────────────────────┤
│ NORMAL │ company > Acme Corp │ 45% │ Esc/back         │
├─ Input ────────────────────────────────────────────────┤
│ wuphf:workspace▸ [cursor]                                │
└────────────────────────────────────────────────────────┘
```

## Keybindings
- Normal: j/k=scroll, h/l=columns, gg/G=top/bottom, 1-9=quick-select, i=insert, /=search, ?=help, Esc=back, q=quit
- Insert: Esc=normal, Enter=execute, Tab=autocomplete, Up/Down=history
- Ctrl: C=quit, D/U=half-page, F/B=full-page, N/P=history, L=redraw

## Pickers
- Arrow-key navigable, highlighted row (blue bg, white text, bold)
- Number column (1-based), name column, detail column
- Quick-select with 1-9 digits
- Bottom hint: "j/k navigate · enter select · i insert · esc back"

## Tables
- Auto-sized columns, alternating row colors (bright/dim)
- Header: blue text, bold, underlined
- Footer: "N rows" count

## Status Bar
- [MODE BADGE] [breadcrumb] [scroll%] [hint]
- NORMAL=blue bg, INSERT=green bg
- Breadcrumb: "company > Acme Corp"

## Colors
- Brand: NexBlue #2980fb, NexPurple #cf72d9, NexGreen #97a022
- Status: Success #03a04c, Warning #df750c, Error #e23428, Info #4d97ff
- Text: Value #cfd0d2, Label #999a9b, Muted #838485

## Views
- Help: 4-column grid (Explore, AI, Write, Config)
- Record detail: purple title, key-value attributes
- Timeline: icons by event type, vertical connectors
- Task board: 3-column kanban (To Do, In Progress, Done)
- Graph: force-directed + tree + radial modes
- Insights: priority badges [CRIT/HIGH/MED/LOW]
- Ask/chat: multi-turn, user=blue ▸, AI=neutral, sources listed

## Loading
- Animated braille spinner + contextual hint text

## Input
- Prompt: "wuphf:workspace▸ "
- 256 char limit
- Tab autocomplete (commands, subcommands, object slugs, workspace names)
- History: Up/Down

## Banner
- Procedurally generated ASCII art with dots/connectors
- Regenerates every 10 spinner ticks
- Brand text centered

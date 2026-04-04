# PR Blueprint 01: Snappy Office Ergonomics & Vim Motions

## 1. Objective
Make the TUI as fast and keyboard-friendly as CC-agent. Users should never have to take their hands off the home row to mention an agent, switch a channel, or edit a message.

## 2. Key Features
- **Fuzzy Picker Component:** A reusable Bubble Tea component for filtering lists (Agents, Channels, Commands).
- **Vim Motions for Input:** Basic `h/j/k/l`, `w/b`, `0/$` support in the message input field.
- **Agent Presence Summary:** A visual indicator of "Who is thinking" and "What are they doing" (e.g., "FE Engineer • Reading internal/tui/model.go").

## 3. Targeted Files
- `internal/tui/picker.go` (New): Reusable list filter.
- `internal/tui/channel.go`: Integrate the picker for `@` mentions and `/` commands.
- `cmd/wuphf/channel_member_draft.go`: Add Vim-style navigation to the text input.
- `internal/tui/statusbar.go`: Enhance to show real-time agent activity summaries.

## 4. Implementation Details
- **Fuzzy Matching:** Use a simple Go fuzzy matching library (like `sahilm/fuzzy`) for the picker.
- **Status Summary:** The `StreamModel` already has access to agent phases (e.g., `PhaseExecuteTool`). Render these as a "Currently Doing" line in the status bar or a dedicated "Thinking" pane.

## 5. Validation
- Press `@` in the main input and fuzzy-search for an agent.
- Navigate the message input field using Vim motions.
- Ensure the status bar updates when an agent starts a tool operation.

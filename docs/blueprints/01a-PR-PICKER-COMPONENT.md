# PR Blueprint 01a: Fuzzy Picker Component

## 1. Objective
Create a reusable "Fuzzy Picker" Bubble Tea component. This will not yet be integrated into the UI, but will serve as the foundation for channel and agent switching.

## 2. Key Features
- **Fuzzy Filtering:** Takes a list of strings and a search query, returns filtered results.
- **Selection UI:** A simple vertical list with a cursor and "Enter" to select.
- **Keybindings:** `Up/Down` for navigation, `Enter` for selection, `Esc` to close.

## 3. Targeted Files
- `internal/tui/picker.go` (New): Implementation of the picker model and update loop.

## 4. Implementation Details
- Use a simple Go library like `sahilm/fuzzy` for matching.
- The component should be a sub-model that can be embedded in the main `ChannelModel`.
- Focus on the `Model`, `Update`, and `View` functions for the list filtering specifically.

## 5. Validation
- Run unit tests in `internal/tui/picker_test.go` to verify fuzzy filtering logic.
- Verify that the cursor moves correctly through the list.

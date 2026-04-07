# PR Blueprint 01b: Channel Picker Integration

## 1. Objective
Integrate the Fuzzy Picker from 01a into the main Channel TUI to allow fast switching between channels using the `/switch` command.

## 2. Key Features
- **Trigger:** Typing `/switch ` (with a space) or `/s ` opens the picker.
- **Populate:** Load the list of active channel names from the Broker.
- **Action:** Selecting a channel triggers the `switch:` message currently handled in `channel.go`.

## 3. Targeted Files
- `internal/tui/channel.go`: Update the command parser to trigger the picker.
- `cmd/wuphf/channel.go`: Handle the result of the picker selection.

## 4. Implementation Details
- Connect the `picker.Model` to the `ChannelModel`.
- Ensure the main chat input is disabled while the picker is active.

## 5. Validation
- Type `/switch`, search for a channel, and press Enter. Verify the UI switches to that channel.

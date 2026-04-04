# PR Blueprint 02: High-Rigor Coding Tools & Summary Rendering

## 1. Objective
Equip agents with professional local execution tools and ensure the office channel remains readable even during heavy code output.

## 2. Key Features
- **Local Toolset:** Implement robust `read_file`, `grep_search`, `glob`, `write_file`, and `bash` tools.
- **Summary Rendering (Folded Output):** Large tool results (e.g., build logs, 50-line file reads) must be "folded" in the TUI by default. Show a 1-line summary like `[Tool] bash: Exit 0 (124 lines)` with an option to expand.
- **Task Output to Disk:** Every tool execution should log its full raw output to `~/.wuphf/office/tasks/<id>/output.log`.

## 3. Targeted Files
- `internal/agent/tools.go`: Add the new local tools.
- `internal/tui/stream.go`: Implement the "folded" rendering logic for tool results.
- `internal/agent/loop.go`: Ensure task outputs are captured and persisted to disk.

## 4. Implementation Details
- **Bash Tool:** Must support a "WorkingDirectory" parameter. Use `os/exec` and capture both `stdout` and `stderr`.
- **Folding Logic:** If the tool result is >10 lines or >500 characters, truncate the display in the TUI but keep the full content accessible (e.g., via a detail view or by reading the task log).
- **Summary Generation:** For `grep`, show "Found 12 matches in 3 files." For `bash`, show the exit code and first/last line of output.

## 5. Validation
- Ask an agent to "Grep for 'BubbleTea' in internal/tui". Verify the TUI shows a clean summary, not 50 lines of matches.
- Ask an agent to "Read internal/agent/tools.go". Verify it summarizes the file content.
- Check that the full output exists on disk.

# PR Blueprint 04: Office Memory & Reactive Compaction

## 1. Objective
Prevent office channels from crashing due to context window limits while ensuring agents "remember" the organizational context.

## 2. Key Features
- **Reactive Compaction:** Automatically summarize the oldest 50% of the conversation when the context window is 80% full.
- **Office Insights:** The compaction summary should be posted as a special "Office Insight" message in the channel so humans and agents stay aligned on what was "archived."
- **Persistent Memory:** Summaries should be saved to the Nex-backed organizational memory.

## 3. Targeted Files
- `internal/agent/loop.go`: Monitor token usage and trigger the summarization tool.
- `internal/agent/prompts.go`: Define the "Compaction Prompt" (e.g., "Summarize the mission, key decisions, and current blockers of this thread").
- `internal/chat/router.go`: Handle the injection of "Office Insights" back into the stream.

## 4. Implementation Details
- **Token Counting:** Use a simple heuristic or a library (like `tiktoken-go`) to estimate token count before each turn.
- **Archival:** When compacting, the agent should produce a "State of the Union" summary that becomes the new starting context for the next turn.

## 5. Validation
- Force a compaction by lowering the threshold.
- Verify the agent can still answer questions about a "decision" made 100 turns ago using the archived summary.
- Ensure the "Office Insight" appears in the TUI stream.

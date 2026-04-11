---
description: Stream live events from a managed agent run to check its current status and activity
---
Handle agent status streaming based on $ARGUMENTS (expected: `<run_id>`):

**Stream events from the run:**
Run the following bash command:
```
nex agent events <run_id> --json
```

(The `events` subcommand streams JSON lines regardless of `--json`; the flag applies to the final summary line.)

This streams SSE events as JSON lines. Display each event as it arrives. Watch for:
- `session.status_idle` — run completed normally; display "Run <run_id> completed."
- `approval_needed` — run is paused waiting for human approval; display "⚠️ Run <run_id> requires approval. Use /nex:agent-approve <run_id> to approve." (exit code 2)
- `error` events — display the error details

**If no run_id provided**, display:
"Usage: /nex:agent-status <run_id>"

**Notes:**
- The stream closes automatically when the run reaches a terminal state
- Exit code 2 means approval is required — use `/nex:agent-approve <run_id>` to resume
- Exit code 0 means the run completed normally

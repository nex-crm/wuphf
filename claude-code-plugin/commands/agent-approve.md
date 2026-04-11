---
description: Approve a pending managed agent run that is waiting for tool confirmation
---
Handle agent approval based on $ARGUMENTS (expected: `<run_id>`):

**Approve the run:**
Run the following bash command with the given run_id:
```
nex agent approve <run_id> --json
```

Parse the JSON response. Display:
- Success: "Run <run_id> approved. New status: <status>"
- Error: show the error message from the response

**If no run_id provided**, display:
"Usage: /nex:agent-approve <run_id>"

**Notes:**
- Only works when the run status is `waiting_for_approval`
- Returns 409 if the run is already complete — surface this clearly
- Use `--json` flag for machine-readable output

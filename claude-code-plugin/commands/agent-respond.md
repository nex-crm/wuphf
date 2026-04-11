---
description: Send a message to a managed agent run that is paused and waiting for user input
---
Handle agent response based on $ARGUMENTS (expected: `<run_id> <message>`):

**Send the message:**
Run the following bash command:
```
nex agent respond <run_id> <message...> --json
```

Parse the JSON response. Display:
- Success: "Message sent to run <run_id>. Status: <status>"
- Error: show the error message from the response

**If missing arguments**, display:
"Usage: /nex:agent-respond <run_id> <message>"

**Notes:**
- Only works when the run is paused (`idle` state after an approval or mid-run pause)
- The message is forwarded to the agent as a `user.message` event
- Use `--json` flag for machine-readable output

---
description: Check processing status of an ingested artifact
---
Check artifact status: $ARGUMENTS

If no artifact ID provided, respond: "Usage: /artifact <artifact_id>"

Use the `mcp__nex__get_artifact_status` tool with the provided artifact_id.

Show:
- **Status**: processing state (pending, processing, completed, failed)
- **Extracted entities**: list if available
- **Relationships**: list if available
- **Error**: details if status is failed

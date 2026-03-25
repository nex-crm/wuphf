---
description: Get recent insights from the context graph
---
Get insights: $ARGUMENTS

Use the `mcp__nex__get_insights` tool. If $ARGUMENTS specifies a time window (e.g. "2h", "1d", "30m"), use it as the `last` parameter. Default to "1h" if not specified.

Format each insight as:
- **Type**: insight content
  - Confidence: X% | Source entities
  - Timestamp

If no insights found, respond: "No recent insights found in the specified time window."

---
description: Update a field (attribute) on an object type
---
Update a field: $ARGUMENTS

If insufficient arguments, respond: "Usage: /update-field <object_slug> <attribute_slug> [--label <label>] [--required true|false] [--description <desc>]"

Parse the object slug, attribute slug, and fields to update from arguments.
Use the `mcp__nex__update_attribute` tool with the parsed parameters.
Confirm the update with the returned details.

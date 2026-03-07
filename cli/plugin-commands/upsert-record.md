---
description: Create or update a record (upsert)
---
Upsert record: $ARGUMENTS

If insufficient arguments, respond: "Usage: /upsert-record <object_slug> --match <attribute> <field=value> [field=value ...]"
Example: /upsert-record contacts --match email name="Jane Doe" email="jane@example.com"

The `--match` flag specifies the matching_attribute used to find existing records. If a record with the same value for that attribute exists, it will be updated; otherwise a new record is created.

Parse the object slug, matching attribute, and field key=value pairs.
Use the `mcp__nex__upsert_record` tool with object_slug, matching_attribute, and attributes.
Report whether the record was created or updated.

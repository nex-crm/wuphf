---
description: Scan and ingest project files into Nex knowledge base
---
Scan the current project directory for business documents (md, txt, csv, json, yaml)
and ingest new or changed files into the Nex knowledge base.
Use the mcp__nex__context_add_text tool for each file found.
If $ARGUMENTS contains a path, scan that directory instead.
Report which files were ingested and which were skipped.

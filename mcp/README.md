# WUPHF MCP Server

Give any MCP-compatible AI client access to your WUPHF CRM — contacts, companies, deals, notes, tasks, and the context graph.

## Quick Start

```bash
cd mcp
bun install
bun run build
```

## Setup

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `WUPHF_API_KEY` | Yes | — | Your WUPHF Developer API key |
| `MCP_TRANSPORT` | No | `stdio` | Transport mode: `stdio` or `http` |
| `MCP_PORT` | No | `3001` | HTTP server port (only when `MCP_TRANSPORT=http`) |

### Get Your API Key

1. Log in to [WUPHF](https://app.nex.ai)
2. Go to **Settings > Developer**
3. Create a new API key with `record.read` and `record.write` scopes

## Client Configuration

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "wuphf": {
      "command": "node",
      "args": ["/absolute/path/to/wuphf/mcp/dist/index.js"],
      "env": {
        "WUPHF_API_KEY": "nex_dev_your_key_here"
      }
    }
  }
}
```

### Claude Code

```bash
claude mcp add wuphf -- node /absolute/path/to/wuphf/mcp/dist/index.js
```

Set the env var before launching, or add it to your shell profile:

```bash
export WUPHF_API_KEY="nex_dev_your_key_here"
```

### Cursor

Add to `.cursor/mcp.json` in your project root (or `~/.cursor/mcp.json` for global):

```json
{
  "mcpServers": {
    "wuphf": {
      "command": "node",
      "args": ["/absolute/path/to/wuphf/mcp/dist/index.js"],
      "env": {
        "WUPHF_API_KEY": "nex_dev_your_key_here"
      }
    }
  }
}
```

### ChatGPT (Desktop)

ChatGPT uses Streamable HTTP transport. Start the server in HTTP mode:

```bash
WUPHF_API_KEY="nex_dev_your_key_here" MCP_TRANSPORT=http bun start
```

Then in ChatGPT Desktop, add a new MCP server pointing to:

```
http://localhost:3001/mcp
```

### Any Streamable HTTP Client

Start the server in HTTP mode and point your client at the `/mcp` endpoint:

```bash
WUPHF_API_KEY="nex_dev_your_key_here" MCP_TRANSPORT=http MCP_PORT=3001 bun start
```

- MCP endpoint: `http://localhost:3001/mcp`
- Health check: `http://localhost:3001/health`

## Tools (46)

### Context (5 tools)

| Tool | Description |
|------|-------------|
| `query_context` | Ask natural language questions about contacts, companies, and relationships |
| `add_context` | Ingest unstructured text (meeting notes, emails, transcripts) into the context graph |
| `get_artifact_status` | Check processing status of a submitted text artifact |
| `create_list_job` | AI-powered list generation from natural language queries |
| `get_list_job_status` | Poll for AI list generation results |

### Search (1 tool)

| Tool | Description |
|------|-------------|
| `search_records` | Search records by name across all object types |

### Schema (8 tools)

| Tool | Description |
|------|-------------|
| `create_object` | Create a new custom object type (e.g. Project, Deal) |
| `get_object` | Get an object type definition with its attributes |
| `list_objects` | List all object type definitions in the workspace |
| `update_object` | Update an object type definition |
| `delete_object` | Delete an object type and all its records |
| `create_attribute` | Add a field to an object type |
| `update_attribute` | Update a field definition |
| `delete_attribute` | Delete a field from an object type |

### Records (7 tools)

| Tool | Description |
|------|-------------|
| `create_record` | Create a new record for an object type |
| `upsert_record` | Create or update a record with deduplication |
| `get_record` | Retrieve a record by ID |
| `update_record` | Update specific attributes on a record |
| `delete_record` | Permanently delete a record |
| `list_records` | List records with filtering, sorting, and pagination |
| `get_record_timeline` | Get timeline events for a record |

### Relationships (5 tools)

| Tool | Description |
|------|-------------|
| `create_relationship_definition` | Define a relationship type between object types |
| `list_relationship_definitions` | List all relationship type definitions |
| `delete_relationship_definition` | Delete a relationship type and all its instances |
| `create_relationship` | Link two records using a relationship definition |
| `delete_relationship` | Remove a relationship between two records |

### Lists (9 tools)

| Tool | Description |
|------|-------------|
| `list_object_lists` | Get all lists for an object type |
| `create_list` | Create a new list under an object type |
| `get_list` | Get a list definition by ID |
| `delete_list` | Delete a list |
| `add_list_member` | Add a record to a list |
| `upsert_list_member` | Add or update a record in a list |
| `list_list_records` | Get paginated records from a list |
| `update_list_record` | Update list-specific attributes for a record |
| `delete_list_record` | Remove a record from a list |

### Tasks (5 tools)

| Tool | Description |
|------|-------------|
| `create_task` | Create a task, optionally linked to records |
| `list_tasks` | List tasks with filtering and search |
| `get_task` | Get a task by ID |
| `update_task` | Update a task's fields |
| `delete_task` | Archive a task |

### Notes (5 tools)

| Tool | Description |
|------|-------------|
| `create_note` | Create a note, optionally linked to a record |
| `list_notes` | List notes, optionally filtered by record |
| `get_note` | Get a note by ID |
| `update_note` | Update a note's fields |
| `delete_note` | Archive a note |

### Insights (1 tool)

| Tool | Description |
|------|-------------|
| `get_insights` | Query insights by time window (opportunities, risks, relationship changes) |

## Development

```bash
# Run with hot reload
bun run dev

# Build
bun run build

# Run production
bun start
```

### MCP Inspector

Debug tools interactively using the MCP Inspector:

```bash
WUPHF_API_KEY="nex_dev_your_key_here" bun run inspect
```

This opens a browser UI where you can list tools, call them with test inputs, and inspect responses.

## Architecture

```
src/
  index.ts          # Entry point — stdio or HTTP transport
  server.ts         # McpServer creation and tool registration
  client.ts         # WUPHF Developer API HTTP client
  tools/
    context.ts      # Context graph: query, ingest, artifacts, AI lists
    search.ts       # Cross-object search
    schema.ts       # Object types and attributes (CRUD)
    records.ts      # Records (CRUD + timeline)
    relationships.ts # Relationship definitions and instances
    lists.ts        # List management and membership
    tasks.ts        # Task management
    notes.ts        # Note management
    insights.ts     # Insight queries
```

## License

MIT

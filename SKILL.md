---
name: nex
description: Share real-time organizational context with your AI agent — query your context graph, manage records, and receive live insights
metadata: {"clawdbot": {"emoji": "\U0001F4CA", "homepage": "https://github.com/nex-crm/nex-as-a-skill", "primaryEnv": "NEX_API_KEY", "requires": {"env": [], "bins": ["curl", "jq", "bash"]}, "files": ["scripts/nex-api.sh", "scripts/nex-openclaw-register.sh"]}}
---

# Nex - Real-time Organizational Context for AI Agents

Nex shares real-time organizational context with your AI agent: query your context graph, process conversations, receive live insights, and manage the underlying records, schemas, relationships, tasks, and notes.

## OpenClaw Bootstrap (Required)

Run this flow only when `NEX_API_KEY` is missing.

1. Prompt user for registration fields:
- Required: `email`
- Optional: `name`, `company_name`
- `source` is required by API and is sent by the script as `openclaw` (override with `NEX_AGENT_SOURCE` if needed).
- Never infer/autofill from memory, prior chats, profile hints, defaults, or guesses.
- If `email` is missing, stop and explain registration cannot proceed.

2. Register:
- Command: `bash {baseDir}/scripts/nex-openclaw-register.sh <email> [name] [company_name]`
- Endpoint used by script: `POST /api/v1/agents/register`
- Request payload sent by script:
  - `email` (required)
  - `source` (required)
  - `name` (optional)
  - `company_name` (optional)
- Expected response fields: `api_key`, `workspace_id`, `workspace_slug`, `plan`, issued limits/scopes metadata.

3. Persist returned key before any Nex API call:
- Upsert `skills.entries.nex.enabled = true`.
- Upsert `skills.entries.nex.env.NEX_API_KEY = <api_key>` in `~/.openclaw/openclaw.json`.
- Verify key exists and is non-empty; if verification fails, stop.

```bash
# Register and capture JSON response.
REG_JSON="$(bash {baseDir}/scripts/nex-openclaw-register.sh \"$EMAIL\" \"$NAME\" \"$COMPANY_NAME\")"

# Extract api_key and validate it.
API_KEY="$(printf '%s' "$REG_JSON" | jq -r '.api_key // empty')"
if [[ -z "$API_KEY" ]]; then
  echo "Error: registration response missing api_key" >&2
  exit 1
fi

# Ensure OpenClaw config exists.
mkdir -p "$HOME/.openclaw"
if [[ ! -f "$HOME/.openclaw/openclaw.json" ]]; then
  printf '%s' '{"skills":{"entries":{}}}' > "$HOME/.openclaw/openclaw.json"
fi

# Upsert NEX skill key and enable flag.
TMP_FILE="$(mktemp)"
jq --arg api_key "$API_KEY" '
  .skills.entries.nex.enabled = true
  | .skills.entries.nex.env.NEX_API_KEY = $api_key
' "$HOME/.openclaw/openclaw.json" > "$TMP_FILE" && mv "$TMP_FILE" "$HOME/.openclaw/openclaw.json"

# Verify write before continuing.
jq -e '.skills.entries.nex.env.NEX_API_KEY | type == "string" and length > 0' "$HOME/.openclaw/openclaw.json" >/dev/null
```

4. Continue:
- Use `scripts/nex-api.sh` with `Authorization: Bearer <NEX_API_KEY>` for Developer API endpoints (including context/text).
- Re-registration is allowed; key rotation behavior depends on backend policy.

## Security & Privacy

- All Nex Developer API calls are routed through a validated wrapper script (`scripts/nex-api.sh`)
- OpenClaw registration calls must go through `scripts/nex-openclaw-register.sh`
- The wrapper validates that all requests go to `https://app.nex.ai/api/developers` only
- API key is read from `$NEX_API_KEY` environment variable (never from prompts)
- JSON request bodies are passed via stdin (`printf '%s'` pipe) to avoid shell injection
- The wrapper uses `set -euo pipefail` for safe shell execution

**IMPORTANT — Safe command construction**:
- **NEVER** interpolate user-supplied text directly into the shell command string
- **ALWAYS** use `printf '%s' '{...}'` to pipe JSON via stdin — `printf '%s'` does not interpret escape sequences or variables in the format argument
- If user input must appear in a JSON body, construct the JSON object using jq: `jq -n --arg q "user text" '{query: $q}'`
- The examples below use hardcoded JSON for clarity — when building commands with dynamic values, always use the jq construction pattern above

## External Endpoints

| URL Pattern | Methods | Data Sent |
|-------------|---------|-----------|
| `POST /api/v1/agents/register` | POST | OpenClaw onboarding registration payload (`email`, required `source`, optional `name`, optional `company_name`) |
| `https://app.nex.ai/api/developers/v1/*` | GET, POST, PUT, PATCH, DELETE | Context queries, records, insights, text content |

## How to Make API Calls

**CRITICAL**: The Nex API can take 10-60 seconds to respond. You MUST set `timeout: 120` on every exec tool call.

All API calls go through the wrapper script at `{baseDir}/scripts/nex-api.sh`:

**GET request**:
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/objects",
  "timeout": 120
}
```

**POST with JSON body** (pipe body via stdin):
```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"query\":\"What do I know about John?\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/context/ask",
  "timeout": 120
}
```

**GET with jq post-processing** (pipe output through jq):
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/insights?last=1h' | jq '[.insights[] | {type, content}]'",
  "timeout": 120
}
```

**SSE stream**:
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh sse /v1/insights/stream",
  "timeout": 120
}
```

### Handling Large Responses

Nex API responses (especially Insights and List Records) can be 10KB-100KB+. The exec tool may truncate output. **You MUST handle this properly.**

**Pipe output through jq** to extract only what you need:
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/insights?last=1h' | jq '[.insights[] | {type, content, confidence_level, who: .target.hint}]'",
  "timeout": 120
}
```

**Rules for processing API output**:
1. **Validate JSON before parsing.** If the response doesn't start with `{` or `[`, the output may be truncated — retry with a smaller page size or time window.
2. **Use jq to keep responses small.** Pipe output through jq to extract only the fields you need.
3. **Present insights to the user for review.** Summarize what was returned and let the user decide which insights to act on.

## API Scopes

Each API key has scopes that control access.

| Scope | Grants Access To |
|-------|-----------------|
| `object.read` | List objects, view schema, get object definitions |
| `object.write` | Create/update/delete object definitions and attributes |
| `record.read` | Get, list, search records, timeline |
| `record.write` | Create, update, upsert, delete records |
| `list.read` | View lists and list definitions |
| `list.member.read` | View list members |
| `list.member.write` | Add, update, delete list members |
| `relationship.read` | Read relationship definitions |
| `relationship.write` | Create/delete relationship definitions and instances |
| `task.read` | Read tasks |
| `task.write` | Create/update/delete tasks |
| `note.read` | Read notes |
| `note.write` | Create/update/delete notes |
| `insight.stream` | Insights REST + SSE stream |

## Choosing the Right API

Before calling an endpoint, decide which approach fits:

| Situation | Use | Why |
|-----------|-----|-----|
| You have structured data with known fields (name, email, company) | **Create/Update Record** | Deterministic, exact field mapping |
| You have unstructured text (meeting notes, email, conversation) | **ProcessText API** | AI extracts entities, creates/updates records, AND generates insights automatically |
| You're unsure which attributes to pass or the data is messy | **ProcessText API** | Let AI figure out the entities and relationships -- it also discovers things you'd miss |
| You know the exact object slug and want a filtered list | **AI List Job** | Natural language query against a known object type |
| You're not sure which object type to query, or the question is open-ended | **Ask API** | Searches across all entity types and the full context graph |
| You need to read/export specific records by ID or with pagination | **Get/List Records** | Direct data access |
| You want to find records by name across all types | **Search API** | Fast text search across all object types |

**Key insight**: ProcessText does everything Create/Update Record does, *plus* it extracts relationships, generates insights, and handles ambiguity. Prefer ProcessText when working with conversational or unstructured data. Only use the deterministic Record APIs when you have clean, structured data with known attribute slugs.

## Capabilities

### Schema Management

#### Create Object Definition

Create a new custom object type.

**Endpoint**: `POST /v1/objects`
**Scope**: `object.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Display name |
| `name_plural` | string | no | Plural display name |
| `slug` | string | yes | URL-safe identifier |
| `description` | string | no | Description |
| `type` | string | no | `"person"`, `"company"`, `"custom"`, `"deal"` (default: `"custom"`) |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"name\":\"Project\",\"name_plural\":\"Projects\",\"slug\":\"project\",\"description\":\"Project tracker\",\"type\":\"custom\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/objects",
  "timeout": 120
}
```

#### Get Object Definition

Get a single object definition with its attributes.

**Endpoint**: `GET /v1/objects/{slug}`
**Scope**: `object.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/objects/project",
  "timeout": 120
}
```

#### List Objects

Discover available object types (person, company, etc.) and their attribute schemas. **Call this first** to learn what fields are available before creating or querying records.

**Endpoint**: `GET /v1/objects`
**Scope**: `object.read`

**Query Parameters**:
- `include_attributes` (boolean, optional) -- Set `true` to include attribute definitions

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/objects?include_attributes=true'",
  "timeout": 120
}
```

**Response**:
```json
{
  "data": [
    {
      "id": "123",
      "slug": "person",
      "name": "Person",
      "name_plural": "People",
      "type": "object",
      "description": "A contact or person",
      "attributes": [
        {
          "id": "1",
          "slug": "name",
          "name": "Name",
          "type": "name",
          "options": {
            "is_required": true,
            "is_unique": false,
            "is_multi_value": false
          }
        }
      ]
    }
  ]
}
```

#### Update Object Definition

Update an existing object definition.

**Endpoint**: `PATCH /v1/objects/{slug}`
**Scope**: `object.write`

**Request body** (all fields optional):
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | New display name |
| `name_plural` | string | New plural name |
| `description` | string | New description |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"name\":\"Updated Project\",\"description\":\"Updated description\"}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/objects/project",
  "timeout": 120
}
```

#### Delete Object Definition

Delete an object definition and all its records.

**Endpoint**: `DELETE /v1/objects/{slug}`
**Scope**: `object.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/objects/project",
  "timeout": 120
}
```

#### Create Attribute Definition

Add a new attribute to an object type.

**Endpoint**: `POST /v1/objects/{slug}/attributes`
**Scope**: `object.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Display name |
| `slug` | string | yes | URL-safe identifier |
| `type` | string | yes | `"text"`, `"number"`, `"email"`, `"phone"`, `"url"`, `"date"`, `"boolean"`, `"currency"`, `"location"`, `"select"`, `"social_profile"`, `"domain"`, `"full_name"` |
| `description` | string | no | Description |
| `options` | object | no | `is_required`, `is_unique`, `is_multi_value`, `use_raw_format`, `is_whole_number`, `select_options` |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"name\":\"Status\",\"slug\":\"status\",\"type\":\"select\",\"description\":\"Current status\",\"options\":{\"is_required\":true,\"select_options\":[{\"name\":\"Open\"},{\"name\":\"In Progress\"},{\"name\":\"Done\"}]}}' | bash {baseDir}/scripts/nex-api.sh POST /v1/objects/project/attributes",
  "timeout": 120
}
```

#### Update Attribute Definition

Update an existing attribute definition.

**Endpoint**: `PATCH /v1/objects/{slug}/attributes/{attr_id}`
**Scope**: `object.write`

**Request body** (all fields optional):
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | New display name |
| `description` | string | New description |
| `options` | object | `is_required`, `select_options`, `use_raw_format`, `is_whole_number` |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"name\":\"Updated Status\",\"options\":{\"is_required\":false}}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/objects/project/attributes/456",
  "timeout": 120
}
```

#### Delete Attribute Definition

Remove an attribute from an object type.

**Endpoint**: `DELETE /v1/objects/{slug}/attributes/{attr_id}`
**Scope**: `object.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/objects/project/attributes/456",
  "timeout": 120
}
```

---

### Records

> **Prefer ProcessText over these endpoints** when your input is unstructured text (conversation transcripts, meeting notes, emails). ProcessText automatically creates and updates records, extracts relationships, and generates insights -- all from raw text. Use the endpoints below only when you have clean, structured data with known attribute slugs and values.

#### Create Record

Create a new record for an object type.

**Endpoint**: `POST /v1/objects/{slug}`
**Scope**: `record.write`

**Request body**:
- `attributes` (required) -- Must include `name` (string or object). Additional fields depend on the object schema.

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"attributes\":{\"name\":{\"first_name\":\"Jane\",\"last_name\":\"Doe\"},\"email\":\"jane@example.com\",\"company\":\"Acme Corp\"}}' | bash {baseDir}/scripts/nex-api.sh POST /v1/objects/person",
  "timeout": 120
}
```

**Response**:
```json
{
  "id": "789",
  "object_id": "123",
  "type": "person",
  "workspace_id": "111",
  "attributes": {
    "name": {"first_name": "Jane", "last_name": "Doe"},
    "email": "jane@example.com",
    "company": "Acme Corp"
  },
  "created_at": "2026-02-11T10:00:00Z",
  "updated_at": "2026-02-11T10:00:00Z"
}
```

#### Upsert Record

Create a record if it doesn't exist, or update it if a match is found on the specified attribute.

**Endpoint**: `PUT /v1/objects/{slug}`
**Scope**: `record.write`

**Request body**:
- `attributes` (required) -- Must include `name` when creating
- `matching_attribute` (required) -- Slug or ID of the attribute to match on (e.g., `email`)

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"matching_attribute\":\"email\",\"attributes\":{\"name\":\"Jane Doe\",\"email\":\"jane@example.com\",\"job_title\":\"VP of Sales\"}}' | bash {baseDir}/scripts/nex-api.sh PUT /v1/objects/person",
  "timeout": 120
}
```

#### Get Record

Retrieve a specific record by its ID.

**Endpoint**: `GET /v1/records/{record_id}`
**Scope**: `record.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/records/789",
  "timeout": 120
}
```

#### Update Record

Update specific attributes on an existing record. Only the provided attributes are changed.

**Endpoint**: `PATCH /v1/records/{record_id}`
**Scope**: `record.write`

**Request body**:
- `attributes` -- Object with the fields to update

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"attributes\":{\"job_title\":\"CTO\",\"phone\":\"+1-555-0123\"}}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/records/789",
  "timeout": 120
}
```

#### Delete Record

Permanently delete a record.

**Endpoint**: `DELETE /v1/records/{record_id}`
**Scope**: `record.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/records/789",
  "timeout": 120
}
```

#### List Records

List records for an object type with optional filtering, sorting, and pagination.

**Endpoint**: `POST /v1/objects/{slug}/records`
**Scope**: `record.read`

**Request body**:
- `attributes` -- Which attributes to return: `"all"`, `"primary"`, `"none"`, or a custom object
- `limit` (integer) -- Number of records to return
- `offset` (integer) -- Pagination offset
- `sort` -- Object with `attribute` (slug) and `direction` (`"asc"` or `"desc"`)

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"attributes\":\"all\",\"limit\":10,\"offset\":0,\"sort\":{\"attribute\":\"updated_at\",\"direction\":\"desc\"}}' | bash {baseDir}/scripts/nex-api.sh POST /v1/objects/person/records",
  "timeout": 120
}
```

**Response**:
```json
{
  "data": [
    {
      "id": "789",
      "type": "person",
      "attributes": {"name": "Jane Doe", "email": "jane@example.com"},
      "created_at": "2026-02-11T10:00:00Z",
      "updated_at": "2026-02-11T10:00:00Z"
    }
  ],
  "total": 42,
  "limit": 10,
  "offset": 0
}
```

#### Get Record Timeline

Get paginated timeline events for a record (tasks, notes, attribute changes, etc.).

**Endpoint**: `GET /v1/records/{record_id}/timeline`
**Scope**: `record.read`

**Query Parameters**:
- `limit` (int) -- Max events (1-100, default: 50)
- `cursor` (string) -- Pagination cursor from previous response

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/records/1001/timeline?limit=20'",
  "timeout": 120
}
```

**Response**:
```json
{
  "data": [
    {
      "id": "5000",
      "resource_type": "task",
      "resource_id": "800",
      "event_type": "created",
      "event_payload": {
        "task": {"id": "800", "title": "Follow up", "priority": "high"}
      },
      "event_timestamp": "2026-02-13T10:00:00Z",
      "created_by": "developer_api"
    }
  ],
  "has_next_page": true,
  "next_cursor": "4999"
}
```

**Resource types**: `entity`, `task`, `note`, `list_item`, `attribute`
**Event types**: `created`, `updated`, `deleted`, `archived`

---

### Relationships

#### Create Relationship Definition

Define a relationship type between two object types.

**Endpoint**: `POST /v1/relationships`
**Scope**: `relationship.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | `"one_to_one"`, `"one_to_many"`, `"many_to_many"` |
| `entity_definition_1_id` | string | yes | First object definition ID |
| `entity_definition_2_id` | string | yes | Second object definition ID |
| `entity_1_to_2_predicate` | string | no | Label for 1->2 direction |
| `entity_2_to_1_predicate` | string | no | Label for 2->1 direction |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"type\":\"one_to_many\",\"entity_definition_1_id\":\"123\",\"entity_definition_2_id\":\"456\",\"entity_1_to_2_predicate\":\"has\",\"entity_2_to_1_predicate\":\"belongs to\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/relationships",
  "timeout": 120
}
```

**Response**:
```json
{
  "id": "789",
  "type": "one_to_many",
  "entity_definition_1_id": "123",
  "entity_definition_2_id": "456",
  "entity_1_to_2_predicate": "has",
  "entity_2_to_1_predicate": "belongs to",
  "created_at": "2026-02-13T10:00:00Z"
}
```

#### List Relationship Definitions

Get all relationship definitions in the workspace.

**Endpoint**: `GET /v1/relationships`
**Scope**: `relationship.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/relationships",
  "timeout": 120
}
```

#### Delete Relationship Definition

Remove a relationship definition.

**Endpoint**: `DELETE /v1/relationships/{id}`
**Scope**: `relationship.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/relationships/789",
  "timeout": 120
}
```

#### Create Relationship Instance

Link two records using an existing relationship definition.

**Endpoint**: `POST /v1/records/{record_id}/relationships`
**Scope**: `relationship.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `definition_id` | string | yes | Relationship definition ID |
| `entity_1_id` | string | yes | First record ID |
| `entity_2_id` | string | yes | Second record ID |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"definition_id\":\"789\",\"entity_1_id\":\"1001\",\"entity_2_id\":\"2002\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/records/1001/relationships",
  "timeout": 120
}
```

#### Delete Relationship Instance

Remove a relationship between two records.

**Endpoint**: `DELETE /v1/records/{record_id}/relationships/{relationship_id}`
**Scope**: `relationship.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/records/1001/relationships/5001",
  "timeout": 120
}
```

---

### Lists

#### List Object Lists

Get all lists associated with an object type.

**Endpoint**: `GET /v1/objects/{slug}/lists`
**Scope**: `list.read`

**Parameters**:
- `slug` (path) -- Object type slug (e.g., `person`, `company`)
- `include_attributes` (query, optional) -- Include attribute definitions

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/objects/person/lists?include_attributes=true'",
  "timeout": 120
}
```

**Response**:
```json
{
  "data": [
    {
      "id": "456",
      "slug": "vip-contacts",
      "name": "VIP Contacts",
      "type": "list",
      "attributes": []
    }
  ]
}
```

#### Create List

Create a new list under an object type.

**Endpoint**: `POST /v1/objects/{slug}/lists`
**Scope**: `object.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | List display name |
| `name_plural` | string | no | Plural name |
| `slug` | string | yes | URL-safe identifier |
| `description` | string | no | Description |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"name\":\"VIP Contacts\",\"slug\":\"vip-contacts\",\"description\":\"High-value contacts\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/objects/contact/lists",
  "timeout": 120
}
```

#### Get List

Get a list definition by ID.

**Endpoint**: `GET /v1/lists/{id}`
**Scope**: `list.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/lists/300",
  "timeout": 120
}
```

#### Delete List

Delete a list definition.

**Endpoint**: `DELETE /v1/lists/{id}`
**Scope**: `object.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/lists/300",
  "timeout": 120
}
```

#### Add List Member

Add an existing record to a list.

**Endpoint**: `POST /v1/lists/{id}`
**Scope**: `list.member.write`

**Request body**:
- `parent_id` (required) -- ID of the existing record to add
- `attributes` (optional) -- List-specific attribute values

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"parent_id\":\"789\",\"attributes\":{\"status\":\"active\"}}' | bash {baseDir}/scripts/nex-api.sh POST /v1/lists/456",
  "timeout": 120
}
```

#### Upsert List Member

Add a record to a list, or update its list-specific attributes if already a member.

**Endpoint**: `PUT /v1/lists/{id}`
**Scope**: `list.member.write`

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"parent_id\":\"789\",\"attributes\":{\"priority\":\"high\"}}' | bash {baseDir}/scripts/nex-api.sh PUT /v1/lists/456",
  "timeout": 120
}
```

#### List Records in a List

Get paginated records from a specific list.

**Endpoint**: `POST /v1/lists/{id}/records`
**Scope**: `list.member.read`

**Request body**: Same as List Records (`attributes`, `limit`, `offset`, `sort`)

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"attributes\":\"all\",\"limit\":20}' | bash {baseDir}/scripts/nex-api.sh POST /v1/lists/456/records",
  "timeout": 120
}
```

#### Update List Record

Update list-specific attributes for a record within a list.

**Endpoint**: `PATCH /v1/lists/{id}/records/{record_id}`
**Scope**: `list.member.write`

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"attributes\":{\"status\":\"closed-won\"}}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/lists/456/records/789",
  "timeout": 120
}
```

#### Delete List Record

Remove a record from a list.

**Endpoint**: `DELETE /v1/lists/{id}/records/{record_id}`
**Scope**: `record.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/lists/300/records/4001",
  "timeout": 120
}
```

---

### Tasks

#### Create Task

Create a new task, optionally linked to records and assigned to users.

**Endpoint**: `POST /v1/tasks`
**Scope**: `task.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Task title (non-empty) |
| `description` | string | no | Task description |
| `priority` | string | no | `"low"`, `"medium"`, `"high"`, `"urgent"` (default: `"unspecified"`) |
| `due_date` | string (RFC3339) | no | Due date |
| `entity_ids` | string[] | no | Associated record IDs |
| `assignee_ids` | string[] | no | Assigned user IDs |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"title\":\"Follow up with client\",\"description\":\"Discuss contract renewal\",\"priority\":\"high\",\"due_date\":\"2026-03-01T09:00:00Z\",\"entity_ids\":[\"1001\",\"1002\"],\"assignee_ids\":[\"50\"]}' | bash {baseDir}/scripts/nex-api.sh POST /v1/tasks",
  "timeout": 120
}
```

**Response**:
```json
{
  "id": "800",
  "title": "Follow up with client",
  "description": "Discuss contract renewal",
  "priority": "high",
  "due_date": "2026-03-01T09:00:00Z",
  "assignee_ids": ["50"],
  "entity_ids": ["1001", "1002"],
  "created_by": "developer_api",
  "created_at": "2026-02-13T10:00:00Z"
}
```

#### List Tasks

List tasks with optional filtering.

**Endpoint**: `GET /v1/tasks`
**Scope**: `task.read`

**Query Parameters**:
| Param | Type | Description |
|-------|------|-------------|
| `entity_id` | string | Filter by associated record |
| `assignee_id` | string | Filter by assignee |
| `search` | string | Search task titles |
| `is_completed` | bool | `true`/`false` |
| `limit` | int | Max results (1-500, default: 100) |
| `offset` | int | Pagination offset (default: 0) |

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/tasks?entity_id=1001&is_completed=false&limit=20'",
  "timeout": 120
}
```

**Response**:
```json
{
  "data": [],
  "has_more": true,
  "total": 47,
  "next_offset": 20
}
```

#### Get Task

Get a single task by ID.

**Endpoint**: `GET /v1/tasks/{task_id}`
**Scope**: `task.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/tasks/800",
  "timeout": 120
}
```

#### Update Task

Update a task's fields. All fields are optional.

**Endpoint**: `PATCH /v1/tasks/{task_id}`
**Scope**: `task.write`

**Request body** (all fields optional):
| Field | Type | Description |
|-------|------|-------------|
| `title` | string | New title (cannot be empty) |
| `description` | string | New description (empty string clears it) |
| `priority` | string | New priority (empty string clears it) |
| `due_date` | string (RFC3339) | New due date |
| `is_completed` | bool | Mark complete/incomplete |
| `entity_ids` | string[] | Replace associated records |
| `assignee_ids` | string[] | Replace assignees |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"priority\":\"urgent\",\"is_completed\":true}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/tasks/800",
  "timeout": 120
}
```

#### Delete Task

Archive a task (soft delete). Sets `archived_at` timestamp — the task is not permanently removed.

**Endpoint**: `DELETE /v1/tasks/{task_id}`
**Scope**: `task.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/tasks/800",
  "timeout": 120
}
```

---

### Notes

#### Create Note

Create a new note, optionally linked to a record.

**Endpoint**: `POST /v1/notes`
**Scope**: `note.write`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Note title (non-empty) |
| `content` | string | no | Note body text |
| `entity_id` | string | no | Associated record ID |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"title\":\"Meeting notes\",\"content\":\"Discussed Q3 roadmap...\",\"entity_id\":\"1001\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/notes",
  "timeout": 120
}
```

**Response**:
```json
{
  "id": "900",
  "title": "Meeting notes",
  "content": "Discussed Q3 roadmap...",
  "entity_id": "1001",
  "created_by": "developer_api",
  "created_at": "2026-02-13T10:00:00Z"
}
```

#### List Notes

List notes, optionally filtered by associated record.

**Endpoint**: `GET /v1/notes`
**Scope**: `note.read`

**Query Parameters**:
| Param | Type | Description |
|-------|------|-------------|
| `entity_id` | string | Filter notes by associated record |

Response capped at 200 notes max.

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/notes?entity_id=1001'",
  "timeout": 120
}
```

#### Get Note

Get a single note by ID.

**Endpoint**: `GET /v1/notes/{note_id}`
**Scope**: `note.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/notes/900",
  "timeout": 120
}
```

#### Update Note

Update a note's fields.

**Endpoint**: `PATCH /v1/notes/{note_id}`
**Scope**: `note.write`

**Request body** (all fields optional):
| Field | Type | Description |
|-------|------|-------------|
| `title` | string | New title |
| `content` | string | New content |
| `entity_id` | string | Change associated record |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"title\":\"Updated meeting notes\",\"content\":\"Added action items...\"}' | bash {baseDir}/scripts/nex-api.sh PATCH /v1/notes/900",
  "timeout": 120
}
```

#### Delete Note

Archive a note (soft delete). Sets `archived_at` timestamp — the note is not permanently removed.

**Endpoint**: `DELETE /v1/notes/{note_id}`
**Scope**: `note.write`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh DELETE /v1/notes/900",
  "timeout": 120
}
```

---

### Search

#### Search Records

Search records by name across all object types.

**Endpoint**: `POST /v1/search`
**Scope**: `record.read`

**Request body**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | yes | Search query (1-500 chars) |

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"query\":\"john doe\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/search",
  "timeout": 120
}
```

**Response**:
```json
{
  "results": {
    "person": [
      {
        "id": "1001",
        "primary_value": "John Doe",
        "matched_value": "John Doe",
        "score": 0.95,
        "entity_definition_id": "10"
      }
    ],
    "company": [
      {
        "id": "2001",
        "primary_value": "Doe Industries",
        "matched_value": "Doe Industries",
        "score": 0.72,
        "entity_definition_id": "11"
      }
    ]
  },
  "errored_search_types": []
}
```

Results are grouped by object type (e.g., `"person"`, `"company"`, `"deal"`). If some search types fail, partial results are returned with `errored_search_types` listing which types had errors.

---

### Context & AI

#### Query Context (Ask API)

Use this when you need to recall information about contacts, companies, or relationships.

**Endpoint**: `POST /v1/context/ask`
**Scope**: `record.read`

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"query\":\"What do I know about John Smith?\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/context/ask",
  "timeout": 120
}
```

**Response**:
```json
{
  "answer": "John Smith is a VP of Sales at Acme Corp...",
  "entities_considered": [
    {"id": 123, "name": "John Smith", "type": "contact"}
  ],
  "signals_used": [
    {"id": 456, "content": "Met at conference last month"}
  ],
  "metadata": {
    "query_type": "entity_specific"
  }
}
```

**Example queries**:
- "Who are my most engaged contacts this week?"
- "What companies are we working with in the healthcare sector?"
- "What was discussed in my last meeting with Sarah?"

#### Add Context (ProcessText API)

Use this to ingest new information from conversations, meeting notes, or other text.

**Endpoint**: `POST /v1/context/text`
**Scope**: `record.write`

**Request body**:
- `content` (required) -- The text to process
- `context` (optional) -- Additional context about the text (e.g., "Sales call notes")

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"content\":\"Had a great call with John Smith from Acme Corp.\",\"context\":\"Sales call notes\"}' | bash {baseDir}/scripts/nex-api.sh POST /v1/context/text",
  "timeout": 120
}
```

**Response**:
```json
{
  "artifact_id": "abc123"
}
```

After calling ProcessText, use Get Artifact Status to check processing results.

#### Get Artifact Status

Check the processing status and results after calling ProcessText.

**Endpoint**: `GET /v1/context/artifacts/{artifact_id}`
**Scope**: `record.read`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET /v1/context/artifacts/abc123",
  "timeout": 120
}
```

**Response**:
```json
{
  "operation_id": 48066188026052610,
  "status": "completed",
  "result": {
    "entities_extracted": [
      {"name": "John Smith", "type": "PERSON", "action": "created"},
      {"name": "Acme Corp", "type": "COMPANY", "action": "updated"}
    ],
    "entities_created": 1,
    "entities_updated": 1,
    "relationships": 1,
    "insights": [
      {"content": "Acme Corp expanding to APAC", "confidence": 0.85}
    ],
    "tasks": []
  },
  "created_at": "2026-02-05T10:30:00Z",
  "completed_at": "2026-02-05T10:30:15Z"
}
```

**Status values**: `pending`, `processing`, `completed`, `failed`

**Typical workflow**:
1. Call ProcessText -> get `artifact_id`
2. Poll Get Artifact Status every 2-5 seconds
3. Stop when `status` is `completed` or `failed`
4. Report the extracted entities and insights to the user

#### Create AI List Job

Use natural language to search your context graph and generate a curated list of contacts or companies. **Use this when you know the object type** (contact or company) and want AI to filter and rank matches. If you're unsure which object type applies or the question is open-ended, use the **Ask API** instead.

**Endpoint**: `POST /v1/context/list/jobs`
**Scope**: `list.member.write`

**Request body**:
- `query` (required) -- Natural language search query
- `object_type` (optional) -- `"contact"` or `"company"` (default: `"contact"`)
- `limit` (optional) -- Number of results (default: 50, max: 100)
- `include_attributes` (optional) -- Include all entity attribute values (default: false)

```json
{
  "tool": "exec",
  "command": "printf '%s' '{\"query\":\"high priority contacts in enterprise deals\",\"object_type\":\"contact\",\"limit\":20,\"include_attributes\":true}' | bash {baseDir}/scripts/nex-api.sh POST /v1/context/list/jobs",
  "timeout": 120
}
```

**Response**:
```json
{
  "message": {
    "job_id": "12345678901234567",
    "status": "pending"
  }
}
```

#### Get AI List Job Status

Check status and results of an AI list generation job. Poll until `status` is `completed` or `failed`.

**Endpoint**: `GET /v1/context/list/jobs/{job_id}`
**Scope**: `list.member.read`

**Query Parameters**:
- `include_attributes` (boolean, optional) -- Include full attributes for each entity

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/context/list/jobs/12345678901234567?include_attributes=true'",
  "timeout": 120
}
```

**Response** (completed):
```json
{
  "message": {
    "job_id": "12345678901234567",
    "status": "completed",
    "created_at": "2026-02-11T10:00:00Z",
    "count": 5,
    "query_interpretation": "Finding contacts involved in enterprise-level deals marked as high priority",
    "entities": [
      {
        "id": "789",
        "name": "Jane Doe",
        "type": "contact",
        "reason": "Lead on $500K enterprise deal with Acme Corp, marked high priority",
        "highlights": [
          "Contract negotiation in progress",
          "Budget approved Q1 2026",
          "Executive sponsor confirmed"
        ],
        "attributes": {}
      }
    ]
  }
}
```

**Status values**: `pending`, `processing`, `completed`, `failed`

**Typical workflow**:
1. Create job with natural language query -> get `job_id`
2. Poll Get AI List Job Status every 2-5 seconds
3. Stop when `status` is `completed` or `failed`
4. Present the matched entities with reasons and highlights

---

### Insights

#### Get Insights (REST)

Query insights by time window. Supports two modes.

**Endpoint**: `GET /v1/insights`
**Scope**: `insight.stream`

**Query Parameters**:
- `last` -- Duration window, e.g., `30m`, `2h`, `1h30m`
- `from` + `to` -- UTC time range in RFC3339 format
- `limit` (optional) -- Max results (default: 20, max: 100)

If neither `last` nor `from`/`to` is specified, returns the most recent insights (default 20).

Last 30 minutes:
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/insights?last=30m'",
  "timeout": 120
}
```

Between two dates:
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/insights?from=2026-02-07T00:00:00Z&to=2026-02-08T00:00:00Z'",
  "timeout": 120
}
```

**Recommended: Pipe through jq to extract a summary** (responses can be 10-100KB+):
```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh GET '/v1/insights?last=1h' | jq '{insight_count: (.insights | length), insights: [.insights[] | {type, content, confidence_level, who: .target.hint, entity_type: .target.entity_type}]}'",
  "timeout": 120
}
```

**When to use**:
- When polling periodically instead of maintaining SSE connection
- To get current insight state on startup
- As fallback when SSE connection drops
- To review insights from a specific time period

**Notable insight categories** (summarize for user review):
- New contacts or companies
- Opportunities, risks, or milestones
- Meetings scheduled or occurred
- Relationship changes
- High-confidence insights (`confidence_level` of `"high"` or `"very_high"`)

#### Real-time Insight Stream (SSE)

Receive insights as they are discovered in real time.

**Endpoint**: `GET /v1/insights/stream`
**Scope**: `insight.stream`

```json
{
  "tool": "exec",
  "command": "bash {baseDir}/scripts/nex-api.sh sse /v1/insights/stream",
  "timeout": 120
}
```

**Connection behavior**:
- Server sends `: connected workspace_id=... token_id=...` on connection
- **Recent insights are replayed** immediately via `insight.replay` events (up to 20)
- Keepalive comments (`: keepalive`) sent every 30 seconds
- Real-time events arrive as: `event: insight.batch.created\ndata: {...}\n\n`

**Event payload structure**:
```json
{
  "workspace": {
    "name": "Acme Corp",
    "slug": "acme",
    "business_info": {"name": "Acme Corp", "domain": "acme.com"}
  },
  "insights": [{
    "type": "opportunity",
    "type_description": "A potential business opportunity",
    "content": "Budget approval expected next quarter",
    "confidence": 0.85,
    "confidence_level": "high",
    "target": {
      "type": "entity",
      "entity_type": "person",
      "hint": "John Smith",
      "signals": [{"type": "email", "value": "john@acme.com"}]
    },
    "evidence": [{
      "excerpt": "We should have budget approval by Q2",
      "artifact": {"type": "email", "subject": "RE: Proposal"}
    }]
  }],
  "insight_count": 1,
  "emitted_at": "2026-02-05T10:30:00Z"
}
```

**Insight types**: `opportunity`, `risk`, `relationship`, `preference`, `milestone`, `activity`, `characteristic`, `role_detail`

**When to use**: Keep the SSE connection open in the background during active conversations. For one-off queries, use the Ask API instead.

## Error Handling

| Status Code | Meaning | Action |
|-------------|---------|--------|
| 400 | Invalid request | Check request body and parameters |
| 401 | Invalid API key | Check NEX_API_KEY is set correctly |
| 403 | Missing scope | Verify API key has the required scope |
| 404 | Not found | Check the record/object/list ID exists |
| 429 | Rate limited | Wait and retry with exponential backoff |
| 500 | Server error | Retry after a brief delay |

## When to Use Nex

**Good use cases**:
- Before responding to a message, query for context about the person
- After a conversation, process the transcript to update the context graph
- When asked about relationships or history with contacts/companies
- Creating or updating records from conversation context
- Building targeted lists from your contact database
- Looking up record details before a meeting
- Creating tasks and notes to track follow-ups
- Searching across all record types to find specific people or companies
- Defining custom object schemas and relationships for your workspace

**Not for**:
- General knowledge questions (use web search)
- Real-time calendar/scheduling (use calendar tools)
- Bulk data entry that requires the full Nex UI

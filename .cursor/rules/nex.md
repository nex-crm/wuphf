# Nex — Organizational Context & Memory

Nex provides your AI agent with real-time organizational knowledge — contacts, deals, meetings, emails, notes, insights, patterns, and playbooks — via MCP tools. Context is proactively injected into your conversation, so relevant knowledge surfaces automatically even when you don't ask for it.

## MCP Tools Available

### `nex_ask` — AI-powered context query (recommended)
Use for natural language questions about organizational context.
Examples: "what's the latest on the Acme deal?", "when did I last talk to Sarah?"

### `nex_remember` — Store information
Save notes, observations, meeting summaries, or any text to the knowledge base.
Examples: "remember that John prefers email over Slack", store meeting notes

### `nex_search` — Fuzzy record lookup
Search CRM records (people, companies, deals) by name or keyword.
Use when looking up a specific named entity rather than asking a question.

### `nex_list_integrations` — Check connected data sources
Shows which integrations (Gmail, Slack, Calendar, CRM) are connected and active.

### `nex_connect_integration` — Connect a data source
Initiate OAuth connection for Gmail, Google Calendar, Outlook, Slack, Salesforce, HubSpot, or Attio.

### `nex_get_notifications` — View recent notifications
Check for new notifications from Nex. Shows deal changes, meeting updates, relationship insights, and custom alerts.

### `nex_set_notification_preferences` — Update notification settings
Change notification frequency or toggle notification types. Example: set polling to every 5 minutes.

### `nex_create_notification_rule` — Create custom notification rules
Set up AI-powered notification rules in natural language. Example: "notify me when deal value changes by more than 10%".

## Proactive Context

Nex automatically surfaces relevant context from the user's knowledge graph on every prompt — not just questions. When you see a `<nex-context>` block, use it naturally to inform your response:

- **Entity insights** — facts about people, companies, and deals mentioned or relevant to the task
- **Knowledge insights** — patterns, lessons learned, and domain knowledge from past work
- **Playbook rules** — proven approaches and best practices from the user's experience

Leverage this context to provide more informed, personalized responses. If the context mentions a relevant pattern or past decision, incorporate it naturally without explicitly referencing the context block.

## When to Use Nex Tools Directly

Use `nex_ask` proactively when:
- The user mentions a person, company, or project — look up their context
- The task involves a domain the knowledge graph may have insights on
- You need organizational context to make a better recommendation

Use `nex_remember` when:
- The user shares a decision, preference, or important fact worth persisting
- A conversation reveals new knowledge that future sessions should have access to

Always try `nex_ask` first for general queries. Use `nex_search` when you need to find a specific record by name.

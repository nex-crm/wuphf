Help the user connect third-party integrations (Gmail, Google Calendar, Outlook, Outlook Calendar, Slack, Attio, HubSpot, Salesforce) to their Nex workspace.

## Available Integrations

`gmail`, `google-calendar`, `outlook`, `outlook-calendar`, `slack`, `attio`, `hubspot`, `salesforce`

## Steps

1. First, list current integrations to see what's already connected:
   Use the `list_integrations` MCP tool, or suggest: `nex integrate list`

2. To connect a new integration:
   Use the `connect_integration` MCP tool with the `type` and `provider`.
   This returns an `auth_url` — open it in the user's browser.
   Or suggest: `nex integrate connect gmail`

3. Poll for completion:
   Use the `get_connect_status` MCP tool with the `connect_id` every few seconds until `status` is `"connected"`.

4. To disconnect:
   Use the `disconnect_integration` MCP tool with the `connection_id`.
   Or: `nex integrate disconnect <id>`

5. To check overall setup status:
   Suggest: `nex setup status`

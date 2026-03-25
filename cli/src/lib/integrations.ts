/**
 * Integration type definitions and fallback data.
 * Extracted from setup command for reuse across TUI/CLI.
 */

export const INTEGRATIONS_MAP: Record<string, { type: string; provider: string; displayName: string; description: string }> = {
  gmail: { type: "email", provider: "google", displayName: "Gmail", description: "Connect your Gmail account to sync emails" },
  "google-calendar": { type: "calendar", provider: "google", displayName: "Google Calendar (WUPHF Meeting Bot)", description: "Connect Google Calendar for meeting transcripts" },
  outlook: { type: "email", provider: "microsoft", displayName: "Outlook", description: "Connect your Outlook account to sync emails" },
  "outlook-calendar": { type: "calendar", provider: "microsoft", displayName: "Outlook Calendar (WUPHF Meeting Bot)", description: "Connect Outlook Calendar for meeting transcripts" },
  slack: { type: "messaging", provider: "slack", displayName: "Slack", description: "Connect Slack to sync messages" },
  salesforce: { type: "crm", provider: "salesforce", displayName: "Salesforce", description: "Connect Salesforce CRM" },
  hubspot: { type: "crm", provider: "hubspot", displayName: "HubSpot", description: "Connect HubSpot CRM" },
  attio: { type: "crm", provider: "attio", displayName: "Attio", description: "Connect Attio CRM" },
};

export interface IntegrationEntry {
  type: string;
  provider: string;
  display_name: string;
  description: string;
  connections: Array<{ id: string | number; status: string; identifier: string }>;
}

/** Returns a list of all known integrations with empty connections (for use when the API is unreachable). */
export function fallbackIntegrations(): IntegrationEntry[] {
  return Object.values(INTEGRATIONS_MAP).map((entry) => ({
    type: entry.type,
    provider: entry.provider,
    display_name: entry.displayName,
    description: entry.description,
    connections: [],
  }));
}

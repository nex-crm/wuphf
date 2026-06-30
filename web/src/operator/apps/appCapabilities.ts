// Pure helpers that turn an app's deterministic capability map (from
// GET /apps/{id}?source=1) into the read/write rows the Data tab renders. Kept
// framework-free and exported so the mapping is unit-tested without a DOM.

import type { AppCapabilities, AppIntegrationUsage } from "../../api/apps";

// Human labels for the canonical bridge helpers an app can call. Kept in sync
// with bridgeAPINames in custom_app_introspect.go.
export const BRIDGE_READ_LABEL: Record<string, string> = {
  callBroker: "Workspace data",
  getTasks: "Tasks",
  getOfficeMembers: "Team roster",
  getEmails: "Email · Gmail",
  listIntegrations: "Connected integrations",
  callIntegration: "Integration calls",
  ai: "AI · one-shot",
};

// The write markers the broker uses to gate a mutating action (actionLikelyWrites
// in composio_workflows.go). An action id carrying any of these is a side effect
// the broker holds for human approval — never auto-sent.
export const WRITE_MARKERS = [
  "SEND",
  "CREATE",
  "UPDATE",
  "DELETE",
  "PATCH",
  "UPSERT",
  "POST",
  "INSERT",
  "UPLOAD",
  "COMPLETE",
];

export function actionIsWrite(actionId: string): boolean {
  const upper = actionId.toUpperCase();
  return WRITE_MARKERS.some((m) => upper.includes(m));
}

export interface CapabilityRow {
  label: string;
  detail: string;
  gated?: boolean;
}

// Split a platform's actions into read rows and gated write rows.
function integrationRows(it: AppIntegrationUsage): {
  reads: CapabilityRow[];
  writes: CapabilityRow[];
} {
  const reads: CapabilityRow[] = [];
  const writes: CapabilityRow[] = [];
  for (const action of it.actions ?? []) {
    const row: CapabilityRow = { label: it.platform, detail: action };
    if (actionIsWrite(action)) writes.push({ ...row, gated: true });
    else reads.push(row);
  }
  // A platform referenced with no specific action still counts as a touch.
  if ((it.actions ?? []).length === 0) {
    reads.push({ label: it.platform, detail: "referenced" });
  }
  return { reads, writes };
}

// Derive the read/write rows from the deterministic capability map.
export function deriveCapabilityRows(caps: AppCapabilities): {
  reads: CapabilityRow[];
  writes: CapabilityRow[];
} {
  const reads: CapabilityRow[] = [];
  const writes: CapabilityRow[] = [];

  for (const api of caps.bridge_apis ?? []) {
    if (api === "createTask") continue; // surfaced as an office write below
    const label = BRIDGE_READ_LABEL[api];
    if (label) reads.push({ label, detail: `bridge · ${api}()` });
  }
  for (const it of caps.integrations ?? []) {
    const rows = integrationRows(it);
    reads.push(...rows.reads);
    writes.push(...rows.writes);
  }
  for (const w of caps.office_writes ?? []) {
    writes.push({
      label: w === "createTask" ? "Create a task" : w,
      detail: "office write · you confirm before it runs",
      gated: true,
    });
  }
  return { reads, writes };
}

export function hasAnyCapability(caps: AppCapabilities): boolean {
  return Boolean(
    (caps.bridge_apis?.length ?? 0) ||
      (caps.integrations?.length ?? 0) ||
      (caps.office_writes?.length ?? 0) ||
      (caps.data_types?.length ?? 0),
  );
}

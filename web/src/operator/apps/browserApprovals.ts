// Browser-step in-chat approval client (slice 3b). A live workflow run PAUSES
// when a `browser` step needs the operator: first for permission to control the
// browser, then before any external send inside the step. The paused asks are
// polled from GET .../workflow/browser/pending and resumed (or skipped) via
// POST .../workflow/browser/approve. Mirrors the Go handlers in
// internal/team/broker_browser_approval.go.

import { get, post } from "../../api/client";

/** One paused browser-step ask for the app's chat to render. */
export interface BrowserApproval {
  id: string;
  app_id: string;
  /** "control" = drive-the-browser permission; "send" = an external send. */
  kind: "control" | "send";
  goal: string;
}

interface PendingResult {
  pending: BrowserApproval[];
}

/**
 * The app's currently paused browser-step asks (oldest first), or empty.
 *
 * Pass React Query's `signal` so a poll that is superseded (the query refetches
 * or a new run resets the cache) is aborted. Without it a late response from a
 * previous poll could repopulate stale approval cards during the next run.
 */
export async function getBrowserApprovals(
  appId: string,
  signal?: AbortSignal,
): Promise<BrowserApproval[]> {
  const res = await get<PendingResult>(
    `/operator/apps/${encodeURIComponent(appId)}/workflow/browser/pending`,
    undefined,
    { signal },
  );
  return res.pending ?? [];
}

/** Resume ("approve") or skip ("deny") a paused browser-step ask. */
export async function resolveBrowserApproval(
  appId: string,
  approvalId: string,
  decision: "approve" | "deny",
): Promise<void> {
  await post(
    `/operator/apps/${encodeURIComponent(appId)}/workflow/browser/approve`,
    { approval_id: approvalId, decision },
  );
}

/** The conversational prompt shown for a paused ask, by kind. */
export function browserApprovalPrompt(a: BrowserApproval): string {
  if (a.kind === "send") {
    return `This step wants to send: “${a.goal}”. Send it?`;
  }
  return `This step has no integration, so I will do it in your browser: “${a.goal}”. Let me control your browser to run it?`;
}

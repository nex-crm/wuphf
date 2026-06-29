// Classifying the integrations a built workflow references against what the
// operator actually has, so the build chat can offer to connect the missing
// ones. The planner names integrations by display label ("HubSpot", "Slack");
// here we resolve each against WUPHF's real catalog (GET /integrations) into one
// of three states:
//
//   connected   — already authorized; nothing to do.
//   connectable — exists in Composio but not connected; offer an inline Connect.
//   unavailable — not in the catalog at all; offer the browser-setup fallback.
//
// The matching is pure (testable without the network); only the resolver hits
// the API, and it degrades to "unavailable" so a build never blocks on this.

import {
  type IntegrationCatalogItem,
  listIntegrations,
} from "../../api/integrations";
import type { WorkflowStep } from "../mock/data";

export type IntegrationReadiness = "connected" | "connectable" | "unavailable";

export interface ReferencedIntegration {
  /** The display name the plan used, e.g. "HubSpot". */
  name: string;
  readiness: IntegrationReadiness;
  /** Set when connectable/connected — needed to drive the real connect flow. */
  provider?: string;
  platform?: string;
}

/** Distinct, trimmed integration names a plan's steps reference, in first-seen order. */
export function referencedIntegrationNames(steps: WorkflowStep[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const step of steps) {
    const name = step.integration?.trim();
    if (!name) continue;
    const key = name.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(name);
  }
  return out;
}

/**
 * Best catalog match for a referenced name: an exact (case-insensitive) match on
 * the toolkit name or platform slug wins; otherwise the first item (the catalog
 * search already ranked by relevance). Null when there are no candidates.
 */
export function pickMatch(
  name: string,
  items: IntegrationCatalogItem[],
): IntegrationCatalogItem | null {
  if (items.length === 0) return null;
  const needle = name.trim().toLowerCase();
  const exact = items.find(
    (i) =>
      i.name.trim().toLowerCase() === needle ||
      i.platform.trim().toLowerCase() === needle,
  );
  return exact ?? items[0];
}

/** Map a resolved catalog item (or its absence) to a readiness state. */
export function readinessOf(
  match: IntegrationCatalogItem | null,
): IntegrationReadiness {
  if (!match) return "unavailable";
  return match.state === "connected" ? "connected" : "connectable";
}

/**
 * Resolve one referenced name against the live catalog. Searches by name (the
 * planner's label) and classifies the best match. Any failure (offline, no
 * backend in the standalone harness) yields "unavailable", which routes the
 * operator to the browser-setup fallback rather than a dead end.
 */
export async function resolveReferencedIntegration(
  name: string,
): Promise<ReferencedIntegration> {
  try {
    const res = await listIntegrations({ search: name, limit: 5 });
    const match = pickMatch(name, res.items);
    return {
      name,
      readiness: readinessOf(match),
      provider: match?.provider,
      platform: match?.platform,
    };
  } catch {
    return { name, readiness: "unavailable" };
  }
}

/** Classify every integration a plan references, concurrently. */
export async function resolveReferencedIntegrations(
  steps: WorkflowStep[],
): Promise<ReferencedIntegration[]> {
  const names = referencedIntegrationNames(steps);
  return Promise.all(names.map(resolveReferencedIntegration));
}

import { describe, expect, it } from "vitest";

import type { IntegrationCatalogItem } from "../../api/integrations";
import type { WorkflowStep } from "../mock/data";
import {
  pickMatch,
  readinessOf,
  referencedIntegrationNames,
} from "./integrationStatus";

function step(partial: Partial<WorkflowStep>): WorkflowStep {
  return {
    id: partial.id ?? "s",
    kind: partial.kind ?? "action",
    title: partial.title ?? "Step",
    detail: partial.detail ?? "",
    integration: partial.integration,
    gated: partial.gated,
  };
}

function item(
  partial: Partial<IntegrationCatalogItem>,
): IntegrationCatalogItem {
  return {
    provider: partial.provider ?? "composio",
    platform: partial.platform ?? "x",
    name: partial.name ?? "X",
    state: partial.state ?? "disconnected",
    can_connect: partial.can_connect ?? true,
    can_disconnect: partial.can_disconnect ?? false,
  };
}

describe("referencedIntegrationNames", () => {
  it("returns distinct, trimmed names in first-seen order, skipping blanks", () => {
    const steps = [
      step({ integration: "HubSpot" }),
      step({ integration: " Slack " }),
      step({ integration: "hubspot" }), // dupe (case-insensitive)
      step({ integration: undefined }),
      step({ integration: "Gmail" }),
    ];
    expect(referencedIntegrationNames(steps)).toEqual([
      "HubSpot",
      "Slack",
      "Gmail",
    ]);
  });
});

describe("pickMatch", () => {
  it("prefers an exact name/platform match over the first item", () => {
    const items = [
      item({ platform: "hubspot_crm", name: "HubSpot CRM" }),
      item({ platform: "hubspot", name: "HubSpot" }),
    ];
    expect(pickMatch("HubSpot", items)?.platform).toBe("hubspot");
    expect(pickMatch("hubspot", items)?.platform).toBe("hubspot");
  });

  it("falls back to the first (relevance-ranked) item when no exact match", () => {
    const items = [item({ platform: "slack", name: "Slack" })];
    expect(pickMatch("Slack workspace", items)?.platform).toBe("slack");
  });

  it("returns null with no candidates", () => {
    expect(pickMatch("Nope", [])).toBeNull();
  });
});

describe("readinessOf", () => {
  it("maps null to unavailable", () => {
    expect(readinessOf(null)).toBe("unavailable");
  });
  it("maps a connected item to connected", () => {
    expect(readinessOf(item({ state: "connected" }))).toBe("connected");
  });
  it("maps a present-but-disconnected item to connectable", () => {
    expect(readinessOf(item({ state: "disconnected" }))).toBe("connectable");
  });
});

import { describe, expect, it } from "vitest";

import { INTEGRATIONS } from "./registry";
import { INTEGRATION_CATEGORIES } from "./types";

describe("integrations/registry", () => {
  it("uses unique ids", () => {
    const ids = new Set<string>();
    for (const d of INTEGRATIONS) {
      expect(ids.has(d.id), `duplicate id: ${d.id}`).toBe(false);
      ids.add(d.id);
    }
  });

  it("only references declared categories", () => {
    const known = new Set(INTEGRATION_CATEGORIES.map((c) => c.id));
    for (const d of INTEGRATIONS) {
      expect(
        known.has(d.category),
        `${d.id} -> unknown category ${d.category}`,
      ).toBe(true);
    }
  });

  it("populates External Agents and Channels", () => {
    const byCat = new Map<string, number>();
    for (const d of INTEGRATIONS) {
      byCat.set(d.category, (byCat.get(d.category) ?? 0) + 1);
    }
    // The redesign requires at least one integration per declared
    // category — an empty category would render an orphaned heading.
    for (const cat of INTEGRATION_CATEGORIES) {
      expect(
        (byCat.get(cat.id) ?? 0) > 0,
        `category ${cat.id} has no integrations`,
      ).toBe(true);
    }
  });

  it("hides each integration when its build gate is off", () => {
    // Every descriptor must respect a gateway_kinds = [] context. This is
    // the contract the IntegrationsApp relies on to suppress cards on
    // builds where the Go layer can't service them.
    const ctx = {
      cfg: { gateway_kinds: [] as never[] },
      localStatuses: [],
    };
    for (const d of INTEGRATIONS) {
      // The Channels category is always available (no build gate). External
      // Agents must be available iff their gateway is registered.
      if (d.category === "external-agents") {
        // biome-ignore lint/suspicious/noExplicitAny: ctx is a structural sample
        expect(d.isAvailable(ctx as any)).toBe(false);
      }
    }
  });
});

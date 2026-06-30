// Regression: inbound request rows must stay in sync with the run history. A
// lead already "routed" in a run must not still show as "scored" with a
// route-eligible fit score, or InternalToolDetail offers a duplicate handoff.

import { describe, expect, it } from "vitest";

import { getTool, INBOUND_REQUESTS } from "./data";

describe("inbound fixture / run-history consistency", () => {
  it("does not offer a re-route for a lead the run history already routed", () => {
    const tool = getTool("inbound-routing");
    expect(tool).toBeDefined();

    const routedCompanies = new Set(
      (tool?.runs ?? [])
        .filter((r) => r.outcome === "routed")
        .map((r) => r.trigger.split("—")[0].trim()),
    );

    // InternalToolDetail shows a Route button only for scored rows with
    // fitScore >= 70. None of those rows may be a company already routed.
    const reRouteOffered = INBOUND_REQUESTS.filter(
      (req) =>
        req.status === "scored" &&
        req.fitScore !== null &&
        req.fitScore >= 70 &&
        routedCompanies.has(req.company),
    );

    expect(reRouteOffered).toEqual([]);
  });
});

import { beforeEach, describe, expect, it, vi } from "vitest";

// Hoist mock factories before imports so vitest can intercept module resolution.
const getMock = vi.hoisted(() => vi.fn());
const postMock = vi.hoisted(() => vi.fn());
const delMock = vi.hoisted(() => vi.fn());

vi.mock("./client", () => ({
  get: getMock,
  post: postMock,
  del: delMock,
}));

import {
  assignPolicyAgent,
  createPolicy,
  deactivatePolicy,
  getPolicies,
  type Policy,
  policyAppliesToAgent,
  unassignPolicyAgent,
} from "./policies";

describe("policies client", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("getPolicies", () => {
    it("GETs /policies and unwraps the policies array", async () => {
      const policies: Policy[] = [
        {
          id: "p1",
          source: "human_directed",
          rule: "Always respond in English",
          active: true,
          created_at: "2026-06-01T00:00:00Z",
        },
      ];
      getMock.mockResolvedValue({ policies });

      const result = await getPolicies();

      expect(getMock).toHaveBeenCalledWith("/policies");
      expect(result).toEqual(policies);
    });

    it("returns empty array when policies key is absent", async () => {
      getMock.mockResolvedValue({});

      const result = await getPolicies();

      expect(result).toEqual([]);
    });
  });

  describe("createPolicy", () => {
    it("POSTs to /policies with rule, source, and agents", async () => {
      const policy: Policy = {
        id: "p2",
        source: "human_directed",
        rule: "Keep responses concise",
        active: true,
        created_at: "2026-06-02T00:00:00Z",
        agents: ["ceo"],
      };
      postMock.mockResolvedValue(policy);

      const result = await createPolicy({
        rule: "Keep responses concise",
        source: "human_directed",
        agents: ["ceo"],
      });

      expect(postMock).toHaveBeenCalledWith("/policies", {
        source: "human_directed",
        rule: "Keep responses concise",
        agents: ["ceo"],
      });
      expect(result).toEqual(policy);
    });

    it("defaults source to human_directed when not provided", async () => {
      postMock.mockResolvedValue({ id: "p3", rule: "r", active: true });

      await createPolicy({ rule: "r" });

      expect(postMock).toHaveBeenCalledWith("/policies", {
        source: "human_directed",
        rule: "r",
        agents: undefined,
      });
    });
  });

  describe("deactivatePolicy", () => {
    it("DELETEs /policies/:id with URL encoding", async () => {
      delMock.mockResolvedValue(undefined);

      await deactivatePolicy("my/policy id");

      expect(delMock).toHaveBeenCalledWith("/policies/my%2Fpolicy%20id");
    });
  });

  describe("assignPolicyAgent", () => {
    it("POSTs to /policies/:id/assign with the agent", async () => {
      const updated: Policy = {
        id: "p1",
        source: "human_directed",
        rule: "r",
        active: true,
        created_at: "2026-06-01T00:00:00Z",
        agents: ["ceo"],
      };
      postMock.mockResolvedValue(updated);

      const result = await assignPolicyAgent("p1", "ceo");

      expect(postMock).toHaveBeenCalledWith("/policies/p1/assign", {
        agent: "ceo",
      });
      expect(result).toEqual(updated);
    });
  });

  describe("unassignPolicyAgent", () => {
    it("POSTs to /policies/:id/unassign with the agent", async () => {
      const updated: Policy = {
        id: "p1",
        source: "human_directed",
        rule: "r",
        active: true,
        created_at: "2026-06-01T00:00:00Z",
        agents: [],
      };
      postMock.mockResolvedValue(updated);

      const result = await unassignPolicyAgent("p1", "ceo");

      expect(postMock).toHaveBeenCalledWith("/policies/p1/unassign", {
        agent: "ceo",
      });
      expect(result).toEqual(updated);
    });
  });

  describe("policyAppliesToAgent", () => {
    const base: Policy = {
      id: "p",
      source: "human_directed",
      rule: "r",
      active: true,
      created_at: "2026-06-01T00:00:00Z",
    };

    it("returns true for a global policy (no agents field)", () => {
      expect(policyAppliesToAgent(base, "ceo")).toBe(true);
    });

    it("returns true for a global policy with empty agents array", () => {
      expect(policyAppliesToAgent({ ...base, agents: [] }, "ceo")).toBe(true);
    });

    it("returns true when agent is in the agents list", () => {
      expect(
        policyAppliesToAgent({ ...base, agents: ["ceo", "librarian"] }, "ceo"),
      ).toBe(true);
    });

    it("returns false when agent is NOT in the agents list", () => {
      expect(
        policyAppliesToAgent({ ...base, agents: ["librarian"] }, "ceo"),
      ).toBe(false);
    });
  });
});

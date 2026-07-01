import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { agentName, reloadAgentNames, setAgentName } from "./agentNames";

describe("agentNames", () => {
  beforeEach(() => {
    localStorage.clear();
    reloadAgentNames();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("falls back to the given name with no override", () => {
    expect(agentName("app_x", "Pipeline Agent")).toBe("Pipeline Agent");
  });

  it("persists a rename and applies it", () => {
    setAgentName("app_x", "Revenue Radar");
    expect(agentName("app_x", "Pipeline Agent")).toBe("Revenue Radar");
  });

  it("an empty rename clears the override back to the default", () => {
    setAgentName("app_x", "Revenue Radar");
    setAgentName("app_x", "   ");
    expect(agentName("app_x", "Pipeline Agent")).toBe("Pipeline Agent");
  });

  it("keeps the rename in memory when localStorage.setItem throws", () => {
    const setItem = vi.spyOn(localStorage, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    setAgentName("app_x", "Revenue Radar");
    // Persistence was attempted and failed…
    expect(setItem).toHaveBeenCalled();
    expect(localStorage.getItem("wuphf.operator.agentNames")).toBeNull();
    // …but only the cross-reload copy is lost — the in-memory rename applies.
    expect(agentName("app_x", "Pipeline Agent")).toBe("Revenue Radar");
  });

  // Real agents (app_ ids) ALSO persist the rename to the broker,
  // fire-and-forget: PATCH /apps/{id} {"name": …}. Mock ids stay local-only.
  describe("broker persistence", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("fires PATCH /apps/{id} for an app_ id", () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue({ ok: true, json: async () => ({ app: {} }) });
      vi.stubGlobal("fetch", fetchMock);
      setAgentName("app_x", "Revenue Radar");
      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [url, init] = fetchMock.mock.calls[0];
      expect(url).toBe("/api/apps/app_x");
      expect(init.method).toBe("PATCH");
      expect(JSON.parse(String(init.body))).toEqual({ name: "Revenue Radar" });
    });

    it("does not PATCH for a mock agent id", () => {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      setAgentName("inbound-routing", "Revenue Radar");
      expect(fetchMock).not.toHaveBeenCalled();
    });

    it("does not PATCH when the rename is cleared", () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue({ ok: true, json: async () => ({ app: {} }) });
      vi.stubGlobal("fetch", fetchMock);
      setAgentName("app_x", "Revenue Radar");
      setAgentName("app_x", "   ");
      // Only the rename itself hit the broker — clearing stays local.
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it("keeps the local rename when the broker PATCH fails", () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error("broker down"));
      vi.stubGlobal("fetch", fetchMock);
      setAgentName("app_x", "Revenue Radar");
      expect(agentName("app_x", "Pipeline Agent")).toBe("Revenue Radar");
    });
  });
});

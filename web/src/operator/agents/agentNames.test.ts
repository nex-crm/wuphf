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
});

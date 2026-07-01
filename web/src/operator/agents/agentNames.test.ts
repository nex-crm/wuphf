import { beforeEach, describe, expect, it } from "vitest";

import { agentName, setAgentName } from "./agentNames";

describe("agentNames", () => {
  beforeEach(() => localStorage.clear());

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
});

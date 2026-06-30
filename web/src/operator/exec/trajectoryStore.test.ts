import { beforeEach, describe, expect, it } from "vitest";

import type { Trajectory } from "./browserExecClient";
import {
  clearTrajectory,
  loadTrajectory,
  saveTrajectory,
} from "./trajectoryStore";

const TRAJ: Trajectory = {
  goal: "g",
  app: "Google Chrome",
  steps: [{ action: "click", role: "Button", label: "Go" }],
};

describe("trajectoryStore", () => {
  beforeEach(() => localStorage.clear());

  it("saves and loads a trajectory by tool + goal", () => {
    saveTrajectory("Tool", "g", TRAJ);
    expect(loadTrajectory("Tool", "g")).toEqual(TRAJ);
  });

  it("returns null for an unknown key", () => {
    expect(loadTrajectory("Other", "z")).toBeNull();
  });

  it("never persists an empty trajectory", () => {
    saveTrajectory("Tool", "g", { goal: "g", app: "a", steps: [] });
    expect(loadTrajectory("Tool", "g")).toBeNull();
  });

  it("clears a saved trajectory", () => {
    saveTrajectory("Tool", "g", TRAJ);
    clearTrajectory("Tool", "g");
    expect(loadTrajectory("Tool", "g")).toBeNull();
  });
});

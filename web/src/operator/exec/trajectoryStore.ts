// trajectoryStore.ts — persist a recorded run trajectory so the next Run of the
// same workflow replays deterministically (C2) instead of re-driving the model.
// Keyed by the run's identity (tool + goal). localStorage is fine: a trajectory
// is small, per-operator, and self-heals on replay, so staleness is cheap.

import type { Trajectory } from "./browserExecClient";

const PREFIX = "opr.cua.trajectory.";

export function trajectoryKey(toolName: string, goal: string): string {
  return `${PREFIX}${toolName}::${goal}`;
}

export function saveTrajectory(
  toolName: string,
  goal: string,
  trajectory: Trajectory,
): void {
  if (!trajectory.steps || trajectory.steps.length === 0) return;
  try {
    localStorage.setItem(
      trajectoryKey(toolName, goal),
      JSON.stringify(trajectory),
    );
  } catch {
    // Quota or no-localStorage (SSR/tests) — replay is an optimization, skip it.
  }
}

export function loadTrajectory(
  toolName: string,
  goal: string,
): Trajectory | null {
  try {
    const raw = localStorage.getItem(trajectoryKey(toolName, goal));
    if (!raw) return null;
    const traj = JSON.parse(raw) as Trajectory;
    return traj.steps && traj.steps.length > 0 ? traj : null;
  } catch {
    return null;
  }
}

export function clearTrajectory(toolName: string, goal: string): void {
  try {
    localStorage.removeItem(trajectoryKey(toolName, goal));
  } catch {
    // ignore
  }
}

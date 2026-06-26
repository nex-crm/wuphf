import { describe, expect, it } from "vitest";

import type { CustomApp } from "../../api/apps";
import {
  parseAppNameFromTaskTitle,
  resolveAppForTask,
} from "./AppBuildPreview";

function makeApp(overrides: Partial<CustomApp>): CustomApp {
  return {
    id: "app_0000000000000000",
    slug: "app",
    name: "App",
    icon: "🧩",
    entry: "index.html",
    version: 1,
    createdBy: "app-builder",
    createdAt: "2026-06-15T00:00:00Z",
    updatedAt: "2026-06-15T00:00:00Z",
    contentHash: "abc",
    ...overrides,
  };
}

describe("parseAppNameFromTaskTitle", () => {
  it("parses the App Builder task title verbs", () => {
    expect(parseAppNameFromTaskTitle("Build app: Daily Agent Digest")).toBe(
      "Daily Agent Digest",
    );
    expect(parseAppNameFromTaskTitle("Improve app: Lead Scorer")).toBe(
      "Lead Scorer",
    );
    // Case-insensitive verb + trims surrounding whitespace.
    expect(parseAppNameFromTaskTitle("  improve APP:  Lead Scorer  ")).toBe(
      "Lead Scorer",
    );
  });

  it("returns null for non-app task titles", () => {
    expect(parseAppNameFromTaskTitle("Fix the login bug")).toBeNull();
    expect(parseAppNameFromTaskTitle("appify the dashboard")).toBeNull();
    expect(parseAppNameFromTaskTitle("")).toBeNull();
  });
});

describe("resolveAppForTask", () => {
  it("matches the app by name, case-insensitively", () => {
    const apps = [
      makeApp({ id: "app_a", name: "Lead Scorer" }),
      makeApp({ id: "app_b", name: "Daily Agent Digest" }),
    ];
    expect(resolveAppForTask(apps, "daily agent digest")?.id).toBe("app_b");
  });

  it("prefers the most recently updated app when names collide", () => {
    const apps = [
      makeApp({
        id: "app_old",
        name: "Digest",
        updatedAt: "2026-06-15T00:00:00Z",
      }),
      makeApp({
        id: "app_new",
        name: "Digest",
        updatedAt: "2026-06-15T02:00:00Z",
      }),
    ];
    expect(resolveAppForTask(apps, "Digest")?.id).toBe("app_new");
  });

  it("returns undefined when nothing matches or the name is null", () => {
    const apps = [makeApp({ name: "Lead Scorer" })];
    expect(resolveAppForTask(apps, "Daily Agent Digest")).toBeUndefined();
    expect(resolveAppForTask(apps, null)).toBeUndefined();
    expect(resolveAppForTask([], "Anything")).toBeUndefined();
  });
});

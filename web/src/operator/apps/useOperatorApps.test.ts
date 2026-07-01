import { describe, expect, it } from "vitest";

import type { CustomApp } from "../../api/apps";
import {
  APP_ID_PREFIX,
  appBuildState,
  deriveAppName,
  isRealAppId,
  resolveNewAppId,
} from "./useOperatorApps";

function app(over: Partial<CustomApp>): CustomApp {
  return {
    id: "app_0001",
    slug: "x",
    name: "X",
    icon: "🧩",
    entry: "index.html",
    version: 1,
    createdBy: "app-builder",
    createdAt: "2026-06-29T10:00:00Z",
    updatedAt: "2026-06-29T10:00:00Z",
    contentHash: "h",
    ...over,
  };
}

describe("isRealAppId", () => {
  it("accepts app_ ids and rejects mock tool ids", () => {
    expect(isRealAppId(`${APP_ID_PREFIX}abc`)).toBe(true);
    expect(isRealAppId("inbound-routing")).toBe(false);
    expect(isRealAppId(null)).toBe(false);
    expect(isRealAppId(undefined)).toBe(false);
  });
});

describe("resolveNewAppId", () => {
  it("returns null when no app is new", () => {
    const apps = [app({ id: "app_a" }), app({ id: "app_b" })];
    const before = new Set(["app_a", "app_b"]);
    expect(resolveNewAppId(before, apps)).toBeNull();
  });

  it("picks the app whose id was not present before the build", () => {
    const before = new Set(["app_a"]);
    const apps = [app({ id: "app_a" }), app({ id: "app_new" })];
    expect(resolveNewAppId(before, apps)).toBe("app_new");
  });

  it("prefers the newest by updatedAt when several are new", () => {
    const before = new Set<string>();
    const apps = [
      app({ id: "app_old", updatedAt: "2026-06-29T09:00:00Z" }),
      app({ id: "app_new", updatedAt: "2026-06-29T12:00:00Z" }),
    ];
    expect(resolveNewAppId(before, apps)).toBe("app_new");
  });

  it("ignores a renamed existing app (matches by id, not name)", () => {
    // The agent may register under a tweaked display name; id-based correlation
    // must not be fooled into treating the rename as a new app.
    const before = new Set(["app_a"]);
    const apps = [app({ id: "app_a", name: "Open Tasks Dashboard" })];
    expect(resolveNewAppId(before, apps)).toBeNull();
  });
});

describe("appBuildState", () => {
  const now = Date.parse("2026-06-29T12:00:00Z");

  it("reports ready for a published app", () => {
    expect(appBuildState(app({ status: "ready" }), now)).toBe("ready");
    expect(appBuildState(app({ status: undefined }), now)).toBe("ready");
  });

  it("reports building for a recently-started build", () => {
    const a = app({ status: "building", createdAt: "2026-06-29T11:58:00Z" });
    expect(appBuildState(a, now)).toBe("building");
  });

  it("reports failed for a build stalled past the timeout", () => {
    const a = app({ status: "building", createdAt: "2026-06-29T11:40:00Z" });
    expect(appBuildState(a, now)).toBe("failed");
  });
});

describe("deriveAppName", () => {
  it("names an agent for its role when the domain is recognizable", () => {
    expect(deriveAppName("score inbound leads and route hot ones")).toBe(
      "Lead Routing Agent",
    );
    expect(deriveAppName("keep the CRM clean, dedupe contacts")).toBe(
      "CRM Hygiene Agent",
    );
    expect(deriveAppName("a weekly pipeline summary I can glance at")).toBe(
      "Pipeline Agent",
    );
  });

  it("synthesizes <lead words> Agent for an unknown domain", () => {
    expect(deriveAppName("a refund form for vendors")).toBe(
      "Refund Form For Agent",
    );
  });

  it("caps the synthesized lead at three words", () => {
    expect(
      deriveAppName("create one two three four five six seven eight nine"),
    ).toBe("One Two Three Agent");
  });

  it("falls back when empty", () => {
    expect(deriveAppName("   ")).toBe("Untitled Agent");
  });
});

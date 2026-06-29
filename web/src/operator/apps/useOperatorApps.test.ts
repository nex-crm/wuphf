import { describe, expect, it } from "vitest";

import type { CustomApp } from "../../api/apps";
import {
  APP_ID_PREFIX,
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

describe("deriveAppName", () => {
  it("strips lead-ins and title-cases the first clause", () => {
    expect(deriveAppName("build a dashboard of our open tasks")).toBe(
      "Dashboard Of Our Open Tasks",
    );
  });

  it("takes only the first clause", () => {
    expect(
      deriveAppName("a refund form, then post it to Slack when approved"),
    ).toBe("Refund Form");
  });

  it("caps at six words", () => {
    expect(
      deriveAppName("create one two three four five six seven eight nine"),
    ).toBe("One Two Three Four Five Six");
  });

  it("falls back when empty", () => {
    expect(deriveAppName("   ")).toBe("Untitled app");
  });
});

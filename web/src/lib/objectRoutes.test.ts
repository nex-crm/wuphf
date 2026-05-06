import { describe, expect, it } from "vitest";

import { resolveObjectRoute, resolveUnknownObjectRoute } from "./objectRoutes";

describe("resolveObjectRoute", () => {
  it("resolves an agent slug to the DM hash route", () => {
    const route = resolveObjectRoute({ kind: "agent", slug: "alex" });
    expect(route.href).toBe("#/dm/alex");
    expect(route.label).toBe("Agent: alex");
    expect(route.appAction).toEqual({ app: "dm", channel: "alex" });
    expect(route.fallback).toBeUndefined();
  });

  it("resolves a run id to the activity app with a query param", () => {
    const route = resolveObjectRoute({ kind: "run", id: "run-123" });
    expect(route.href).toBe("#/apps/activity?run=run-123");
    expect(route.label).toBe("Run: run-123");
    expect(route.appAction).toEqual({ app: "activity" });
  });

  it("resolves a task id to the tasks hash route", () => {
    const route = resolveObjectRoute({ kind: "task", id: "task-7" });
    expect(route.href).toBe("#/tasks/task-7");
    expect(route.label).toBe("Task: task-7");
    expect(route.appAction).toEqual({ app: "tasks" });
  });

  it("resolves a wiki-page path with encoded separators preserved", () => {
    const route = resolveObjectRoute({
      kind: "wiki-page",
      path: "people/nazz",
    });
    // Matches the live WikiLink contract: encodeURI keeps `/`.
    expect(route.href).toBe("#/wiki/people/nazz");
    expect(route.label).toBe("Wiki: people/nazz");
    expect(route.appAction).toEqual({ app: "wiki" });
  });

  it("resolves a workbench-item to the task route and surfaces its kind", () => {
    const route = resolveObjectRoute({
      kind: "workbench-item",
      id: "wb-9",
      itemKind: "approval",
    });
    expect(route.href).toBe("#/tasks/wb-9");
    expect(route.label).toBe("Workbench approval: wb-9");
    expect(route.appAction).toEqual({ app: "tasks" });
  });

  it("resolves an artifact to the receipts app with a query param", () => {
    const route = resolveObjectRoute({ kind: "artifact", id: "art-1" });
    expect(route.href).toBe("#/apps/receipts?artifact=art-1");
    expect(route.label).toBe("Artifact: art-1");
    expect(route.appAction).toEqual({ app: "receipts" });
  });

  it("resolves a settings-section to the settings app", () => {
    const route = resolveObjectRoute({
      kind: "settings-section",
      section: "providers",
    });
    expect(route.href).toBe("#/apps/settings?section=providers");
    expect(route.label).toBe("Settings: Providers");
    expect(route.appAction).toEqual({ app: "settings" });
  });

  it("returns a missing_id fallback when a required slug is empty", () => {
    const route = resolveObjectRoute({ kind: "agent", slug: "" });
    expect(route.href).toBe("#/");
    expect(route.fallback?.reason).toBe("missing_id");
    expect(route.fallback?.message).toContain("agent");
    expect(route.appAction).toBeUndefined();
  });

  it("returns a missing_id fallback when a required id is empty", () => {
    const route = resolveObjectRoute({ kind: "task", id: "" });
    expect(route.href).toBe("#/");
    expect(route.fallback?.reason).toBe("missing_id");
    expect(route.fallback?.message).toContain("task");
  });
});

describe("resolveUnknownObjectRoute", () => {
  it("returns a graceful unknown_kind fallback for runtime kinds we don't recognise", () => {
    const route = resolveUnknownObjectRoute("mystery");
    expect(route.href).toBe("#/");
    expect(route.label).toBe("Unknown object: mystery");
    expect(route.fallback?.reason).toBe("unknown_kind");
    expect(route.fallback?.message).toContain("mystery");
  });

  it("handles an empty runtime kind without crashing", () => {
    const route = resolveUnknownObjectRoute("");
    expect(route.fallback?.reason).toBe("unknown_kind");
    expect(route.label).toContain("(missing kind)");
  });
});

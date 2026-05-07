/**
 * Tests for deriveBreadcrumbs — each object kind must produce the
 * correct user-facing label and href.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { describe, expect, it } from "vitest";
import { deriveBreadcrumbs } from "./useObjectBreadcrumb";
import type { CurrentRoute } from "../routes/useCurrentRoute";

describe("deriveBreadcrumbs", () => {
  it("returns empty array for channel routes", () => {
    const route: CurrentRoute = { kind: "channel", channelSlug: "general" };
    expect(deriveBreadcrumbs(route)).toEqual([]);
  });

  it("returns empty array for unknown routes", () => {
    const route: CurrentRoute = { kind: "unknown" };
    expect(deriveBreadcrumbs(route)).toEqual([]);
  });

  it("returns [Agents, @agent] for dm routes", () => {
    const route: CurrentRoute = {
      kind: "dm",
      agentSlug: "gaia",
      channelSlug: "gaia__human",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Agents");
    expect(crumbs[1].label).toContain("gaia");
    expect(crumbs[1].href).toContain("gaia");
  });

  it("returns [Tasks] for task-board route", () => {
    const route: CurrentRoute = { kind: "task-board" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Tasks");
    expect(crumbs[0].href).toBe("#/tasks");
  });

  it("returns [Tasks, Task <id>] for task-detail route", () => {
    const route: CurrentRoute = { kind: "task-detail", taskId: "abc-123" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Tasks");
    expect(crumbs[1].label).toContain("abc-123");
    expect(crumbs[1].href).toContain("abc-123");
  });

  it("returns [Wiki] for wiki catalog route", () => {
    const route: CurrentRoute = { kind: "wiki" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Wiki");
  });

  it("returns [Wiki, article path] for wiki-article route", () => {
    const route: CurrentRoute = {
      kind: "wiki-article",
      articlePath: "people/nazz",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Wiki");
    expect(crumbs[1].label).toContain("people/nazz");
    expect(crumbs[1].href).toContain("people");
  });

  it("returns [Wiki] for wiki-lookup route", () => {
    const route: CurrentRoute = { kind: "wiki-lookup", query: "onboarding" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Wiki");
  });

  it("returns [Notebooks] for notebook-catalog route", () => {
    const route: CurrentRoute = { kind: "notebook-catalog" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Notebooks");
  });

  it("returns [Notebooks, agentSlug] for notebook-agent route", () => {
    const route: CurrentRoute = {
      kind: "notebook-agent",
      agentSlug: "researcher",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Notebooks");
    expect(crumbs[1].label).toBe("researcher");
  });

  it("returns [Notebooks, agent, entry] for notebook-entry route", () => {
    const route: CurrentRoute = {
      kind: "notebook-entry",
      agentSlug: "researcher",
      entrySlug: "2026-05-01-insights",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(3);
    expect(crumbs[0].label).toBe("Notebooks");
    expect(crumbs[1].label).toBe("researcher");
    expect(crumbs[2].label).toBe("2026-05-01-insights");
  });

  it("returns [Reviews] for reviews route", () => {
    const route: CurrentRoute = { kind: "reviews" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Reviews");
  });

  it("returns [Settings] for settings app route", () => {
    const route: CurrentRoute = { kind: "app", appId: "settings" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toContain("Settings");
  });

  it("returns [Console] for console app route", () => {
    const route: CurrentRoute = { kind: "app", appId: "console" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Console");
  });
});

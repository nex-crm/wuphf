/**
 * Tests for deriveBreadcrumbs — each object kind must produce the
 * correct user-facing label and href.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { describe, expect, it } from "vitest";

import type { CurrentRoute } from "../routes/useCurrentRoute";
import { deriveBreadcrumbs } from "./useObjectBreadcrumb";

describe("deriveBreadcrumbs", () => {
  it("returns empty array for channel routes", () => {
    const route: CurrentRoute = { kind: "channel", channelSlug: "general" };
    expect(deriveBreadcrumbs(route)).toEqual([]);
  });

  it("returns empty array for unknown routes", () => {
    const route: CurrentRoute = { kind: "unknown" };
    expect(deriveBreadcrumbs(route)).toEqual([]);
  });

  it("returns [Agents, @agent] for agent-detail routes", () => {
    const route: CurrentRoute = {
      kind: "agent-detail",
      agentSlug: "gaia",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Agents");
    expect(crumbs[0].href).toBe("#/agents");
    expect(crumbs[1].label).toBe("Agent: gaia");
    expect(crumbs[1].href).toBe("#/agents/gaia");
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
    expect(crumbs[1].label).toBe("Task: abc-123");
    expect(crumbs[1].href).toBe("#/tasks/abc-123");
  });

  it("returns [Company Brain] for wiki catalog route", () => {
    const route: CurrentRoute = { kind: "wiki" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Company Brain");
  });

  it("returns [Company Brain, article path] for wiki-article route", () => {
    const route: CurrentRoute = {
      kind: "wiki-article",
      articlePath: "people/nazz",
    };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(2);
    expect(crumbs[0].label).toBe("Company Brain");
    expect(crumbs[1].label).toBe("Company Brain: people/nazz");
    expect(crumbs[1].href).toBe("#/wiki/people/nazz");
  });

  it("returns [Company Brain] for wiki-lookup route", () => {
    const route: CurrentRoute = { kind: "wiki-lookup", query: "onboarding" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Company Brain");
  });

  it("returns [Settings] for settings app route", () => {
    const route: CurrentRoute = { kind: "app", appId: "settings" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Settings");
  });

  it("returns [Graph] for graph app route", () => {
    const route: CurrentRoute = { kind: "app", appId: "graph" };
    const crumbs = deriveBreadcrumbs(route);
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0].label).toBe("Graph");
  });
});

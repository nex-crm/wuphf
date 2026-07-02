import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { WireRoutine } from "../agents/agentStateClient";
import { RoutinesTab } from "./RoutinesTab";

describe("RoutinesTab", () => {
  it("lists routines as scheduled prompts with per-routine lifecycle", () => {
    const { getByText, getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    expect(getByText("Monday pipeline recap")).toBeTruthy();
    expect(getAllByText("Every Monday 9:00").length).toBeGreaterThan(0);
    // Disable / Publish belong to EACH routine, not the agent.
    expect(getAllByText("Publish new version").length).toBe(3);
    expect(getByText("paused")).toBeTruthy(); // the seeded disabled routine
  });

  it("disables one routine without touching the others", () => {
    const { getAllByText } = render(<RoutinesTab agentName="Pipeline Agent" />);
    const disables = getAllByText("Disable");
    expect(disables.length).toBe(2); // two enabled seeds
    fireEvent.click(disables[0]);
    expect(getAllByText("Disable").length).toBe(1);
    expect(getAllByText("Enable").length).toBe(2);
  });

  it("editing a prompt marks a draft; Publish freezes it as the next version", () => {
    const { getAllByLabelText, getAllByText, getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    const prompt = getAllByLabelText(/Prompt for/)[0] as HTMLTextAreaElement;
    fireEvent.change(prompt, { target: { value: "New sharper prompt" } });
    expect(getByText(/v3 · draft/)).toBeTruthy();
    const publish = getAllByText("Publish new version")[0];
    fireEvent.click(publish);
    expect(getByText(/v4$/)).toBeTruthy();
  });

  it("adds a new routine from a prompt + schedule", () => {
    const { getByLabelText, getByText, getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" />,
    );
    fireEvent.change(getByLabelText("Routine prompt"), {
      target: { value: "Email me anything stuck in legal" },
    });
    fireEvent.click(getByText("Add routine"));
    // The new routine renders (name span + editable prompt).
    expect(
      getAllByText("Email me anything stuck in legal").length,
    ).toBeGreaterThan(0);
  });

  it("opens the routine's chat session", () => {
    const onOpenSession = vi.fn();
    const { getAllByText } = render(
      <RoutinesTab agentName="Pipeline Agent" onOpenSession={onOpenSession} />,
    );
    fireEvent.click(getAllByText("Open its chat")[0]);
    expect(onOpenSession).toHaveBeenCalledWith(
      "sess_recap",
      "Monday pipeline recap",
    );
  });
});

// With a REAL agent id the tab loads/persists routines through the agent
// service (/agent proxy); when the service is unreachable it keeps the seeds.
describe("RoutinesTab (live agent service)", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function wire(over: Partial<WireRoutine> = {}): WireRoutine {
    return {
      id: "rt_live",
      agent: "app_x",
      name: "Live recap",
      prompt: "Summarize the pipeline",
      schedule: "Every Monday 9:00",
      enabled: true,
      version: 2,
      sessionId: "sess_live",
      ...over,
    };
  }

  function ok(data: unknown) {
    return { ok: true, json: async () => data };
  }

  it("loads persisted routines from the service for a real agent id", async () => {
    const fetchMock = vi.fn().mockResolvedValue(ok({ routines: [wire()] }));
    vi.stubGlobal("fetch", fetchMock);
    const { findByText, queryByText } = render(
      <RoutinesTab agentName="Pipeline Agent" agentId="app_x" />,
    );
    expect(await findByText("Live recap")).toBeTruthy();
    // The seeds were replaced by the service's answer.
    expect(queryByText("Monday pipeline recap")).toBeNull();
    expect(fetchMock.mock.calls[0][0]).toBe("/agent/routines?agent=app_x");
  });

  it("falls back to the seeds when the service is unreachable", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("agent down"));
    vi.stubGlobal("fetch", fetchMock);
    const { getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" agentId="app_x" />,
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    // Seeded state still renders — the offline path never breaks.
    expect(getByText("Monday pipeline recap")).toBeTruthy();
  });

  it("Disable PATCHes the routine and renders the service's answer", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PATCH") {
        return ok({ routine: wire({ enabled: false }) });
      }
      return ok({ routines: [wire()] });
    });
    vi.stubGlobal("fetch", fetchMock);
    const { findByText, getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" agentId="app_x" />,
    );
    await findByText("Live recap"); // hydration replaced the seeds
    fireEvent.click(getByText("Disable"));
    expect(await findByText("paused")).toBeTruthy();
    const patchCall = fetchMock.mock.calls.find(
      ([, init]) => init?.method === "PATCH",
    );
    expect(patchCall?.[0]).toBe("/agent/routines/rt_live");
    expect(JSON.parse(String(patchCall?.[1]?.body))).toEqual({
      schema_version: 1,
      agent: "app_x",
      enabled: false,
    });
  });

  it("Run now POSTs run-now, refreshes the list, and shows ran just now", async () => {
    let lastRun: string | undefined;
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/agent/routines/rt_live/run" && init?.method === "POST") {
        lastRun = "just now";
        return ok({
          routine: wire({ lastRun }),
          session: {
            id: "sess_live",
            agent: "app_x",
            title: "Live recap",
            kind: "routine",
            at: "just now",
          },
        });
      }
      return ok({ routines: [wire({ lastRun })] });
    });
    vi.stubGlobal("fetch", fetchMock);
    const { findByText, getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" agentId="app_x" />,
    );
    await findByText("Live recap"); // hydration replaced the seeds
    fireEvent.click(getByText("Run now"));
    expect(await findByText("ran just now")).toBeTruthy();
    const runCall = fetchMock.mock.calls.find(
      ([url]) => url === "/agent/routines/rt_live/run",
    );
    expect(runCall).toBeTruthy();
    expect(JSON.parse(String(runCall?.[1]?.body))).toEqual({
      schema_version: 1,
      agent: "app_x",
    });
    // Run-now refreshed the list from the service afterwards.
    const listCalls = fetchMock.mock.calls.filter(
      ([url]) => url === "/agent/routines?agent=app_x",
    );
    expect(listCalls.length).toBeGreaterThanOrEqual(2);
  });

  it("Add routine POSTs the new routine and appends the created one", async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/agent/routines" && init?.method === "POST") {
        return ok({
          routine: wire({ id: "rt_new", name: "Chase legal", version: 1 }),
        });
      }
      return ok({ routines: [wire()] });
    });
    vi.stubGlobal("fetch", fetchMock);
    const { findByText, getByLabelText, getByText } = render(
      <RoutinesTab agentName="Pipeline Agent" agentId="app_x" />,
    );
    await findByText("Live recap");
    fireEvent.change(getByLabelText("Routine prompt"), {
      target: { value: "Email me anything stuck in legal" },
    });
    fireEvent.click(getByText("Add routine"));
    expect(await findByText("Chase legal")).toBeTruthy();
    const createCall = fetchMock.mock.calls.find(
      ([url, init]) => url === "/agent/routines" && init?.method === "POST",
    );
    expect(JSON.parse(String(createCall?.[1]?.body))).toEqual({
      schema_version: 1,
      agent: "app_x",
      name: "Email me anything stuck in legal".slice(0, 40),
      prompt: "Email me anything stuck in legal",
      schedule: "Every Monday 9:00",
    });
  });
});

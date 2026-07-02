import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ToolsProvider, useAppTools } from "./toolsContext";

// Surfaces the provider's state so the wire→FE mapping is assertable.
function Probe() {
  const { tools, agentId } = useAppTools();
  return (
    <div>
      <span data-testid="agent-id">{agentId ?? "none"}</span>
      <ul>
        {tools.map((t) => (
          <li key={t.id} data-testid="tool">
            {t.title}
            <code data-testid="script">{t.script}</code>
            <span data-testid="inputs">
              {t.inputs.map((i) => `${i.name}:${i.type}`).join(",")}
            </span>
            <span data-testid="calls">{t.calls.length}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ok(data: unknown) {
  return { ok: true, json: async () => data };
}

describe("ToolsProvider hydration (agent service)", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("hydrates tools from GET /tools?agent=, mapping wire code onto script", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      ok({
        tools: [
          {
            name: "sendRecap",
            title: "Send the recap",
            purpose: "Send the weekly recap to the team.",
            inputs: [{ name: "week" }, { name: "count", type: "number" }],
            code: "return recap(week, count);",
            version: 2,
          },
        ],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const { findByText, getByTestId, getAllByTestId, queryByText } = render(
      <ToolsProvider appName="Pipeline" agentId="app_x">
        <Probe />
      </ToolsProvider>,
    );
    expect(await findByText("Send the recap")).toBeTruthy();
    // The service's toolbox replaced the seeds entirely.
    expect(queryByText("Weekly pipeline summary")).toBeNull();
    expect(getAllByTestId("tool")).toHaveLength(1);
    // Wire `code` → FE `script`; loose wire input types close to the union;
    // run history starts empty.
    expect(getByTestId("script").textContent).toBe(
      "return recap(week, count);",
    );
    expect(getByTestId("inputs").textContent).toBe("week:string,count:number");
    expect(getByTestId("calls").textContent).toBe("0");
    expect(getByTestId("agent-id").textContent).toBe("app_x");
    expect(fetchMock.mock.calls[0][0]).toBe("/agent/tools?agent=app_x");
  });

  it("falls back to the seeds when the service is unreachable", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("agent down"));
    vi.stubGlobal("fetch", fetchMock);
    const { getByText } = render(
      <ToolsProvider appName="Pipeline" agentId="app_x">
        <Probe />
      </ToolsProvider>,
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(getByText("Weekly pipeline summary")).toBeTruthy();
  });

  it("does not touch the service for a mock agent (no real id)", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const { getByText, getByTestId } = render(
      <ToolsProvider appName="Pipeline">
        <Probe />
      </ToolsProvider>,
    );
    expect(getByText("Weekly pipeline summary")).toBeTruthy();
    expect(getByTestId("agent-id").textContent).toBe("none");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

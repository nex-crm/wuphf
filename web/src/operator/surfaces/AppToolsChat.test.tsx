import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ToolsProvider } from "../tools/toolsContext";
import { AppToolsChat } from "./AppToolsChat";
import { AppToolsTab } from "./AppToolsTab";

// Slice 2: teaching a workflow in the app's chat calls create_tool; the tool-call
// renders in the chat AND the new tool appears in the shared Tools tab.
function renderApp() {
  return render(
    <ToolsProvider appName="Pipeline">
      <AppToolsChat appName="Pipeline" />
      <AppToolsTab appName="Pipeline" />
    </ToolsProvider>,
  );
}

describe("AppToolsChat + Tools tab (slice 2)", () => {
  it("teaching a workflow calls create_tool and the tool lands in the tab", async () => {
    const { getByLabelText, getByText, findByText, queryByText } = renderApp();

    // Seeded tools are already listed; the new one is not there yet.
    expect(getByText("Weekly pipeline summary")).toBeTruthy();
    expect(queryByText("Draft a follow-up email")).toBeNull();

    const input = getByLabelText(
      "Describe a task for Nex to build a tool for",
    ) as HTMLInputElement;
    fireEvent.change(input, {
      target: { value: "Draft a follow-up email for a stalled deal" },
    });
    fireEvent.keyDown(input, { key: "Enter" });

    // The chat renders the agent's create_tool call…
    const call = await findByText(/create_tool\(/);
    expect(call.textContent).toContain('name: "draftFollowup"');

    // …and the new tool now appears in the Tools tab (shared context).
    await waitFor(() =>
      expect(getByText("Draft a follow-up email")).toBeTruthy(),
    );
  });

  it("re-teaching the same workflow updates the tool in place (no duplicate)", async () => {
    const { getByLabelText, findAllByText } = renderApp();
    const input = getByLabelText(
      "Describe a task for Nex to build a tool for",
    ) as HTMLInputElement;

    for (let i = 0; i < 2; i++) {
      fireEvent.change(input, {
        target: { value: "score the lead and route it" },
      });
      fireEvent.keyDown(input, { key: "Enter" });
      // eslint-disable-next-line no-await-in-loop
      await waitFor(() => expect(input.disabled).toBe(false));
    }

    // scoreAndRouteLead was seeded AND taught twice, but the tab shows one card
    // (dedup by name). The chat, however, shows a call each time.
    const cards = await findAllByText("Score & route a lead");
    expect(cards).toHaveLength(1);
  });
});

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppWorkflowTab } from "./AppWorkflowTab";

const getAppWorkflow = vi.fn();
const compileAppWorkflow = vi.fn();
const runAppWorkflow = vi.fn();
vi.mock("../apps/workflowClient", () => ({
  getAppWorkflow: (id: string) => getAppWorkflow(id),
  compileAppWorkflow: (id: string) => compileAppWorkflow(id),
  runAppWorkflow: (id: string, dry: boolean, conns?: Record<string, string>) =>
    runAppWorkflow(id, dry, conns),
}));

// Browser-step approvals (only exercised when a workflow has a browser step).
vi.mock("../apps/browserApprovals", () => ({
  getBrowserApprovals: vi.fn().mockResolvedValue([]),
  resolveBrowserApproval: vi.fn().mockResolvedValue(undefined),
  browserApprovalPrompt: vi.fn().mockReturnValue("prompt"),
}));

// Toast has no provider in this unit test; mock it so mutation callbacks are safe.
vi.mock("../../components/ui/Toast", () => ({ showNotice: vi.fn() }));

function wrap(node: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

describe("AppWorkflowTab", () => {
  beforeEach(() => {
    getAppWorkflow.mockReset();
    compileAppWorkflow.mockReset();
  });

  it("auto-compiles (no button) when the app has no frozen workflow yet", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: false,
      workflow_key: "operator-app-abc",
    });
    // Leave compile pending so we observe the auto "laying out" state.
    compileAppWorkflow.mockReturnValue(new Promise(() => {}));
    const { getByText, queryByRole } = wrap(<AppWorkflowTab appId="app_abc" />);
    // It compiles automatically — no button to press.
    await waitFor(() =>
      expect(compileAppWorkflow).toHaveBeenCalledWith("app_abc"),
    );
    expect(getByText(/laying out this app's workflow/i)).toBeTruthy();
    expect(queryByRole("button")).toBeNull();
  });

  it("renders the frozen steps as ONE read-only flow (trigger → steps → deliver)", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      title: "World Weather",
      steps: [
        {
          id: "s1",
          type: "template",
          description: "Read city weather table",
          gated: false,
        },
        {
          id: "s2",
          type: "nex_ask",
          description: "Summarize the five-city forecast",
          gated: false,
        },
      ],
    });
    const { getByText } = wrap(<AppWorkflowTab appId="app_abc" />);
    await waitFor(() => expect(getByText("Deterministic")).toBeTruthy());
    // The app's real steps, framed by the trigger and delivery nodes.
    expect(getByText("Read city weather table")).toBeTruthy();
    expect(getByText("Runs on demand or on a schedule")).toBeTruthy();
    expect(getByText("Deliver to Slack")).toBeTruthy();
  });

  it("has NO run / schedule / recompile action buttons — the flow is read-only", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "template",
          description: "Read city weather table",
          gated: false,
        },
      ],
    });
    const { queryByRole } = wrap(<AppWorkflowTab appId="app_abc" />);
    await waitFor(() =>
      expect(
        queryByRole("button", { name: /run|schedule|recompile/i }),
      ).toBeNull(),
    );
    // And there are no buttons at all on the compiled view.
    expect(queryByRole("button")).toBeNull();
  });

  it("keeps the Slack channel picker native to the delivery node", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "template",
          description: "Read city weather table",
          gated: false,
        },
      ],
    });
    const { getByLabelText } = wrap(<AppWorkflowTab appId="app_abc" />);
    const channel = (await waitFor(() =>
      getByLabelText("Slack channel"),
    )) as HTMLInputElement;
    // The channel input lives ON the delivery node (defaulting to #general),
    // not in a separate delivery block.
    expect(channel.tagName).toBe("INPUT");
    expect(channel.value).toBe("#general");
    expect(channel.closest(".opr-step")).not.toBeNull();
  });

  it("renders a browser step and a Run live action (browser needs interactive execution)", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "browser",
          description: "Book the meeting room",
          gated: true,
        },
      ],
    });
    const { getByText, getByRole } = wrap(<AppWorkflowTab appId="app_abc" />);
    await waitFor(() =>
      expect(getByText("Book the meeting room")).toBeTruthy(),
    );
    // Browser steps read as their own kind with the "runs in your browser" line.
    expect(getByText(/runs in your browser/i)).toBeTruthy();
    // A browser workflow gets the one interactive action it requires.
    const runLive = getByRole("button", { name: /run live/i });
    runLive.click();
    await waitFor(() =>
      expect(runAppWorkflow).toHaveBeenCalledWith("app_abc", false, {}),
    );
  });
});

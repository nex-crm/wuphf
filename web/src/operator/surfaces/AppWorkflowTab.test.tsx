import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppWorkflowTab } from "./AppWorkflowTab";

const getAppWorkflow = vi.fn();
const compileAppWorkflow = vi.fn();
const runAppWorkflow = vi.fn();
const getAppWorkflowConnections = vi.fn();
vi.mock("../apps/workflowClient", () => ({
  getAppWorkflow: (id: string) => getAppWorkflow(id),
  compileAppWorkflow: (id: string) => compileAppWorkflow(id),
  runAppWorkflow: (id: string, dry: boolean, conns?: Record<string, string>) =>
    runAppWorkflow(id, dry, conns),
  getAppWorkflowConnections: (id: string) => getAppWorkflowConnections(id),
}));

const getBrowserApprovals = vi.fn();
const resolveBrowserApproval = vi.fn();
vi.mock("../apps/browserApprovals", () => ({
  getBrowserApprovals: (id: string) => getBrowserApprovals(id),
  resolveBrowserApproval: (id: string, approvalId: string, decision: string) =>
    resolveBrowserApproval(id, approvalId, decision),
  browserApprovalPrompt: (a: { kind: string; goal: string }) =>
    a.kind === "send"
      ? `This step wants to send: “${a.goal}”. Send it?`
      : `Let me control your browser to run it? ${a.goal}`,
}));

// The delivery schedule fetches connections; stub it so this test is scoped to
// the deterministic-workflow section.
vi.mock("./AppDeliverySchedule", () => ({
  AppDeliverySchedule: () => <div data-testid="delivery-schedule" />,
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
    runAppWorkflow.mockReset();
    getAppWorkflowConnections.mockReset();
    getAppWorkflowConnections.mockResolvedValue({ platforms: [] });
    getBrowserApprovals.mockReset();
    getBrowserApprovals.mockResolvedValue([]);
    resolveBrowserApproval.mockReset();
    resolveBrowserApproval.mockResolvedValue(undefined);
  });

  it("auto-compiles (no button) when the app has no frozen workflow yet", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: false,
      workflow_key: "operator-app-abc",
    });
    // Leave compile pending so we observe the auto "designing" state.
    compileAppWorkflow.mockReturnValue(new Promise(() => {}));
    const { getByText, queryByRole } = wrap(
      <AppWorkflowTab appId="app_abc" appName="Digest" />,
    );
    // It compiles automatically — no "compile" button to click.
    await waitFor(() =>
      expect(compileAppWorkflow).toHaveBeenCalledWith("app_abc"),
    );
    expect(getByText(/designing this app's workflow/i)).toBeTruthy();
    expect(queryByRole("button", { name: /compile workflow/i })).toBeNull();
  });

  it("renders the frozen steps and a deterministic badge when compiled", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      title: "Digest",
      steps: [
        {
          id: "s1",
          type: "template",
          description: "Read recent email",
          gated: false,
        },
        {
          id: "s2",
          type: "action",
          description: "Slack: sends a message",
          platform: "slack",
          gated: true,
        },
      ],
    });
    const { getByText, getByRole } = wrap(
      <AppWorkflowTab appId="app_abc" appName="Digest" />,
    );
    await waitFor(() => expect(getByText("Deterministic")).toBeTruthy());
    expect(getByText("Read recent email")).toBeTruthy();
    expect(getByText(/held for your approval/i)).toBeTruthy();
    expect(getByRole("button", { name: /run once \(preview\)/i })).toBeTruthy();
  });

  it("shows an account chooser when a platform has multiple connections", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "action",
          description: "Read recent email",
          platform: "gmail",
          action_id: "GMAIL_FETCH_EMAILS",
          gated: false,
        },
      ],
    });
    getAppWorkflowConnections.mockResolvedValue({
      platforms: [
        {
          platform: "gmail",
          multiple: true,
          connections: [
            { key: "conn_a", name: "work@nex.ai" },
            { key: "conn_b", name: "personal@gmail.com" },
          ],
        },
      ],
    });
    const { getByRole, getByLabelText } = wrap(
      <AppWorkflowTab appId="app_abc" appName="Digest" />,
    );
    const select = (await waitFor(() =>
      getByLabelText("Account for gmail"),
    )) as HTMLSelectElement;
    // Defaults to the first account, no interaction required.
    expect(select.value).toBe("conn_a");
    // Run passes the chosen connection through.
    getByRole("button", { name: /run once \(preview\)/i }).click();
    await waitFor(() =>
      expect(runAppWorkflow).toHaveBeenCalledWith("app_abc", true, {
        gmail: "conn_a",
      }),
    );
  });

  it("renders a browser step with its own 'runs in your browser' affordance (slice 6)", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "browser",
          description: "Email the digest to finance",
          gated: true,
        },
      ],
    });
    const { getByText, queryByText } = wrap(
      <AppWorkflowTab appId="app_abc" appName="Digest" />,
    );
    await waitFor(() => expect(getByText("Deterministic")).toBeTruthy());
    expect(getByText(/runs in your browser/i)).toBeTruthy();
    // A browser step reads as "browser", not the generic gated-action lock line.
    expect(queryByText(/held for your approval/i)).toBeNull();
  });

  it("a live run surfaces a browser-control ask in chat and Allow resolves it (3b)", async () => {
    getAppWorkflow.mockResolvedValue({
      compiled: true,
      workflow_key: "operator-app-abc",
      steps: [
        {
          id: "s1",
          type: "browser",
          description: "Email the digest to finance",
          gated: true,
        },
      ],
    });
    // A live run stays in flight (paused on the browser step) so the approvals
    // poll is active; the broker reports one pending control ask.
    runAppWorkflow.mockReturnValue(new Promise(() => {}));
    getBrowserApprovals.mockResolvedValue([
      {
        id: "ap_1",
        app_id: "app_abc",
        kind: "control",
        goal: "Email the digest to finance",
      },
    ]);
    const { findByRole } = wrap(
      <AppWorkflowTab appId="app_abc" appName="Digest" />,
    );
    (await findByRole("button", { name: /run live/i })).click();
    await waitFor(() =>
      expect(runAppWorkflow).toHaveBeenCalledWith("app_abc", false, {}),
    );
    // The ask surfaces conversationally; Allow resumes the paused step.
    (await findByRole("button", { name: /^allow$/i })).click();
    await waitFor(() =>
      expect(resolveBrowserApproval).toHaveBeenCalledWith(
        "app_abc",
        "ap_1",
        "approve",
      ),
    );
  });
});

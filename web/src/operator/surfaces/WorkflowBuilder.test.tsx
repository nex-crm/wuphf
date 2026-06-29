import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { WorkflowPlan } from "../builder/planWorkflow";
import { WorkflowBuilder } from "./WorkflowBuilder";

// The builder talks to buildPlanSmart (real engine, mock fallback). Pin it to a
// deterministic plan so the test exercises the handoff, not the planner.
vi.mock("../builder/agentClient", () => ({
  buildPlanSmart: vi.fn(),
  runOperatorPlan: vi.fn(),
}));

// Pin integration classification so the connect-card wiring is deterministic
// (the real resolver would hit GET /integrations).
vi.mock("../builder/integrationStatus", () => ({
  resolveReferencedIntegrations: vi.fn(),
}));

// The connect card reuses WUPHF's ConnectIntegrationCard (React Query + network);
// mock it so the builder test stays unit-level and only checks the card is raised.
vi.mock("../../components/messages/ConnectIntegrationCard", () => ({
  ConnectIntegrationCard: ({ request }: { request: { platform?: string } }) => (
    <div data-testid="connect-card">connect:{request.platform}</div>
  ),
}));

import { buildPlanSmart, runOperatorPlan } from "../builder/agentClient";
import { resolveReferencedIntegrations } from "../builder/integrationStatus";

const buildPlanSmartMock = vi.mocked(buildPlanSmart);
const runOperatorPlanMock = vi.mocked(runOperatorPlan);
const resolveRefsMock = vi.mocked(resolveReferencedIntegrations);

// A plan with one clarifying question on the decision step, so we can prove the
// operator's answer (the clarification) is carried in the finish snapshot.
const PLAN: WorkflowPlan = {
  name: "Inbound demo-request routing",
  toolId: "inbound-routing",
  steps: [
    {
      id: "p-trigger",
      kind: "trigger",
      title: "New demo request",
      detail: "When a demo form is submitted.",
    },
    {
      id: "p-decision",
      kind: "decision",
      title: "Does it clear the bar?",
      detail: "Set the cutoff that decides what moves on.",
    },
  ],
  narration: "Here is the workflow I put together.",
  clarify: {
    field: "threshold",
    stepId: "p-decision",
    prompt: "At what fit score should it move on to a person?",
  },
};

// A clarify-free plan whose action step references an integration, so the build
// completes in one turn and the connect-card wiring can be asserted.
const PLAN_WITH_INTEGRATION: WorkflowPlan = {
  name: "HubSpot fit sync",
  toolId: "inbound-routing",
  steps: [
    {
      id: "p-trigger",
      kind: "trigger",
      title: "New demo request",
      detail: "When a demo form is submitted.",
    },
    {
      id: "p-action",
      kind: "action",
      title: "Update the record",
      detail: "Write the fit score back.",
      integration: "HubSpot",
      gated: true,
    },
  ],
  narration: "Here is the workflow I put together.",
  clarify: null,
};

describe("WorkflowBuilder finish handoff", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    buildPlanSmartMock.mockResolvedValue(PLAN);
    resolveRefsMock.mockResolvedValue([]);
  });
  afterEach(() => {
    vi.useRealTimers();
    buildPlanSmartMock.mockReset();
    runOperatorPlanMock.mockReset();
    resolveRefsMock.mockReset();
  });

  it("hands off the built snapshot with clarified steps, and open vs run are distinct", async () => {
    const onFinish = vi.fn();
    const { getByLabelText, getByRole } = render(
      <WorkflowBuilder onClose={() => {}} onFinish={onFinish} />,
    );

    // Describe the workflow; the planner (mocked) returns PLAN, the steps
    // reveal, then the builder asks its one clarifying question.
    fireEvent.change(
      getByLabelText("Describe the workflow you want to build"),
      { target: { value: "route demo requests to an AE" } },
    );
    fireEvent.click(getByRole("button", { name: "Send" }));
    await vi.runAllTimersAsync();

    // Answer the clarification with a concrete cutoff. This must be applied to
    // the decision step in place, then the finish card appears.
    fireEvent.change(
      getByLabelText("Describe the workflow you want to build"),
      { target: { value: "75" } },
    );
    fireEvent.click(getByRole("button", { name: "Send" }));
    await vi.runAllTimersAsync();

    // Open the tool: onFinish gets the full snapshot, not just an id.
    fireEvent.click(getByRole("button", { name: /Open the tool/ }));
    expect(onFinish).toHaveBeenCalledTimes(1);
    const [draft, mode] = onFinish.mock.calls[0];
    expect(mode).toBe("open");
    expect(draft.toolId).toBe("inbound-routing");
    expect(draft.name).toBe("Inbound demo-request routing");
    // The clarified cutoff (75) survived into the snapshot's decision step.
    const decision = draft.steps.find(
      (s: { id: string }) => s.id === "p-decision",
    );
    expect(decision?.title).toContain("75");

    // Run on test data closes the build->run loop through the real executor
    // (dry run), inline — not a second onFinish handoff.
    runOperatorPlanMock.mockResolvedValue({
      ok: true,
      workflow_key: "operator-inbound-routing",
      dry_run: true,
      run_id: "run-1",
      status: "planned",
      steps: {},
    });
    fireEvent.click(getByRole("button", { name: "Run on test data" }));
    await vi.runAllTimersAsync();
    expect(onFinish).toHaveBeenCalledTimes(1); // still just the "open" handoff
    expect(runOperatorPlanMock).toHaveBeenCalledTimes(1);
    const runPlan = runOperatorPlanMock.mock.calls[0][0];
    expect(runPlan.steps).toEqual(draft.steps);
    expect(runPlan.tool_id).toBe("inbound-routing");
  });

  it("surfaces an inline Connect card for an integration the plan needs but isn't connected", async () => {
    buildPlanSmartMock.mockResolvedValue(PLAN_WITH_INTEGRATION);
    resolveRefsMock.mockResolvedValue([
      {
        name: "HubSpot",
        readiness: "connectable",
        provider: "composio",
        platform: "hubspot",
      },
    ]);

    const { getByLabelText, getByRole, getByTestId } = render(
      <WorkflowBuilder onClose={() => {}} onFinish={() => {}} />,
    );
    fireEvent.change(
      getByLabelText("Describe the workflow you want to build"),
      { target: { value: "score inbound and update HubSpot" } },
    );
    fireEvent.click(getByRole("button", { name: "Send" }));
    await vi.runAllTimersAsync();

    // The card classifies the referenced integration and raises WUPHF's built
    // connect card (ConnectIntegrationCard) for it.
    expect(resolveRefsMock).toHaveBeenCalledWith(PLAN_WITH_INTEGRATION.steps);
    expect(getByTestId("connect-card").textContent).toContain("hubspot");
  });
});

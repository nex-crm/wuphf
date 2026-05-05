import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFallbackChannelSlug } from "../../routes/useCurrentRoute";

vi.mock("../../routes/useCurrentRoute", () => ({
  useFallbackChannelSlug: vi.fn(),
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getRequests: vi.fn().mockResolvedValue({ requests: [] }),
    answerRequest: vi.fn().mockResolvedValue({}),
    cancelRequest: vi.fn().mockResolvedValue({}),
  };
});

import * as clientMod from "../../api/client";
import { RequestsApp } from "./RequestsApp";

const mockUseFallbackChannelSlug = vi.mocked(useFallbackChannelSlug);

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockUseFallbackChannelSlug.mockReturnValue("general");
});

const blockingApprovalOption = {
  id: "approve",
  label: "Approve",
  description: "Approve",
};

function makeBlockingRequest(id: string, action: string) {
  return {
    id,
    from: "integration-ops",
    question: `Approve ${action}?`,
    title: `Approve gmail action: ${action}`,
    blocking: true,
    status: "pending",
    options: [blockingApprovalOption],
    kind: "approval",
  };
}

function makeNonBlockingRequest(id: string, title: string) {
  return {
    id,
    from: "integration-ops",
    question: `Activate ${title}?`,
    title: `Approve skill: ${title}`,
    blocking: false,
    status: "pending",
    options: [{ id: "accept", label: "Accept", description: "Accept" }],
    kind: "skill_proposal",
  };
}

describe("<RequestsApp> blocking requests do not stack", () => {
  it("renders only the first blocking request and shows a queued count for the rest", async () => {
    vi.mocked(clientMod.getRequests).mockResolvedValueOnce({
      requests: [
        makeBlockingRequest("request-1", "send-email-1"),
        makeBlockingRequest("request-2", "send-email-2"),
        makeBlockingRequest("request-3", "send-email-3"),
      ],
    });

    render(wrap(<RequestsApp />));

    await waitFor(() => {
      expect(
        screen.getByText(/Approve gmail action: send-email-1/),
      ).toBeInTheDocument();
    });

    expect(
      screen.queryByText(/Approve gmail action: send-email-2/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/Approve gmail action: send-email-3/),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/2 more queued/)).toBeInTheDocument();
  });
});

describe("<RequestsApp> non-blocking dismiss-all", () => {
  it("renders a Dismiss all button that cancels every non-blocking pending request", async () => {
    vi.mocked(clientMod.getRequests).mockResolvedValueOnce({
      requests: [
        makeNonBlockingRequest("request-10", "broker-readiness"),
        makeNonBlockingRequest("request-11", "external-action-handoff"),
        makeNonBlockingRequest("request-12", "blocker-handoff"),
      ],
    });
    const cancelSpy = vi.mocked(clientMod.cancelRequest);
    cancelSpy.mockClear();

    render(wrap(<RequestsApp />));

    const dismissAll = await screen.findByRole("button", {
      name: /Dismiss all/i,
    });
    fireEvent.click(dismissAll);

    await waitFor(() => {
      expect(cancelSpy).toHaveBeenCalledTimes(3);
    });
    const calledIds = cancelSpy.mock.calls.map((c) => c[0]).sort();
    expect(calledIds).toEqual(["request-10", "request-11", "request-12"]);
  });

  it("does not cancel a blocking request when Dismiss all is clicked", async () => {
    vi.mocked(clientMod.getRequests).mockResolvedValueOnce({
      requests: [
        makeBlockingRequest("request-100", "send-email"),
        makeNonBlockingRequest("request-101", "skill-1"),
      ],
    });
    const cancelSpy = vi.mocked(clientMod.cancelRequest);
    cancelSpy.mockClear();

    render(wrap(<RequestsApp />));

    const dismissAll = await screen.findByRole("button", {
      name: /Dismiss all/i,
    });
    fireEvent.click(dismissAll);

    await waitFor(() => {
      expect(cancelSpy).toHaveBeenCalledTimes(1);
    });
    expect(cancelSpy).toHaveBeenCalledWith("request-101");
  });
});

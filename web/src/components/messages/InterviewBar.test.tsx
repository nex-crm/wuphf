import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

// Mocks installed BEFORE InterviewBar import so the component picks them up.
const useRequestsMock = vi.fn();
vi.mock("../../hooks/useRequests", () => ({
  useRequests: () => useRequestsMock(),
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getSkillsList: vi.fn().mockResolvedValue({
      skills: [
        {
          name: "send-digest",
          title: "Send weekly digest",
          description: "Compile and send the weekly customer digest.",
          status: "active",
          content: "## Steps\n1. gather digest items\n2. send",
        },
      ],
    }),
  };
});

import { InterviewBar } from "./InterviewBar";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function setPending(reqs: AgentRequest[]): void {
  useRequestsMock.mockReturnValue({
    all: reqs,
    pending: reqs,
    blockingPending: reqs.find((r) => r.blocking) ?? null,
  });
}

const baseEnhanceRequest: AgentRequest = {
  id: "request-42",
  from: "archivist",
  channel: "general",
  kind: "enhance_skill_proposal",
  status: "pending",
  title: 'Enhance "send-digest" with new content',
  question:
    "@archivist drafted **send-newsletter**, but it looks similar to existing skill **send-digest**.",
  options: [
    {
      id: "enhance",
      label: "Enhance existing",
      description: "Fold this into send-digest. The candidate is dropped.",
    },
    {
      id: "approve_anyway",
      label: "Approve anyway",
      description: "Bypass the similarity gate and create a new skill.",
    },
    {
      id: "reject",
      label: "Reject",
      description: "Drop this draft. The existing skill stays unchanged.",
    },
  ],
  recommended_id: "enhance",
  blocking: false,
  reply_to: "send-newsletter",
  metadata: {
    enhances_slug: "send-digest",
    similar_to_existing: {
      slug: "send-digest",
      score: 0.92,
      method: "embedding-cosine",
    },
  },
  enhance_candidate: {
    name: "send-newsletter",
    description: "Send out the customer newsletter every Friday.",
    content: "## Steps\n1. ...",
  },
  created_at: "2026-04-29T10:00:00Z",
};

const ambiguousRequest: AgentRequest = {
  id: "request-43",
  from: "archivist",
  channel: "general",
  kind: "skill_proposal",
  status: "pending",
  title: "Approve skill: send-newsletter",
  question:
    "@archivist proposed skill **send-newsletter**: Send the customer newsletter.",
  options: [
    { id: "accept", label: "Accept" },
    { id: "reject", label: "Reject" },
  ],
  recommended_id: "accept",
  blocking: false,
  reply_to: "send-newsletter",
  metadata: {
    similar_to_existing: {
      slug: "send-digest",
      score: 0.78,
      method: "embedding-cosine",
    },
  },
  created_at: "2026-04-29T10:00:00Z",
};

describe("<InterviewBar> enhance UX", () => {
  it("renders three-button row for enhance_skill_proposal kind", () => {
    setPending([baseEnhanceRequest]);
    render(wrap(<InterviewBar />));
    expect(
      screen.getByRole("button", { name: /Enhance existing/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Approve anyway/i }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Reject/i }).length).toBe(1);
  });

  it("renders the inline compare preview for enhance kind", () => {
    setPending([baseEnhanceRequest]);
    render(wrap(<InterviewBar />));
    // Both panel headings appear; "Existing" + "Candidate".
    expect(screen.getAllByText(/Existing/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Candidate/).length).toBeGreaterThan(0);
    // Score line is shown.
    expect(screen.getByText(/Similarity score/i)).toBeInTheDocument();
  });

  it("renders the similar banner for ambiguous skill_proposal", () => {
    setPending([ambiguousRequest]);
    render(wrap(<InterviewBar />));
    expect(screen.getByText(/Similar to/)).toBeInTheDocument();
    // Score is rendered to 2 decimals.
    expect(screen.getByText(/0\.78/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Compare/i }),
    ).toBeInTheDocument();
    // Standard accept/reject options remain.
    expect(screen.getByRole("button", { name: /Accept/i })).toBeInTheDocument();
  });

  it("does NOT show the similar banner for clean skill_proposal", () => {
    const clean: AgentRequest = {
      ...ambiguousRequest,
      id: "request-44",
      metadata: undefined,
    };
    setPending([clean]);
    render(wrap(<InterviewBar />));
    expect(screen.queryByText(/Similar to/)).not.toBeInTheDocument();
  });
});

describe("<InterviewBar> approval UX", () => {
  it("renders EXTERNAL ACTION badge and structured details for approval kind", () => {
    const approval: AgentRequest = {
      id: "request-99",
      from: "growthops",
      channel: "general",
      kind: "approval",
      status: "pending",
      title: "Send Email via Gmail",
      question: "@growthops wants to send email via Gmail. Approve?",
      context: [
        "Why: Sending welcome note.",
        "",
        "What this will do:",
        "• To: alex@nex.ai",
        "• Subject: Welcome to Nex",
        "• Body: Hi Alex, welcome aboard!",
        "",
        "Action: GMAIL_SEND_EMAIL via Gmail",
        "Account: live::gmail::default::abc123",
        "Channel: #general",
      ].join("\n"),
      options: [
        { id: "approve", label: "Approve" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "approve",
      blocking: true,
      created_at: "2026-05-06T00:00:00Z",
    };
    setPending([approval]);
    render(wrap(<InterviewBar />));

    expect(screen.getByText("EXTERNAL ACTION")).toBeInTheDocument();
    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    expect(screen.getByText("Sending welcome note.")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(screen.getByText("Welcome to Nex")).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
  });

  it("falls back to plain context for non-approval requests in the bar", () => {
    const interview: AgentRequest = {
      id: "request-100",
      from: "growthops",
      channel: "general",
      kind: "interview",
      status: "pending",
      title: "Need direction",
      question: "Which path should we take?",
      context: "background information from the agent",
      options: [{ id: "yes", label: "Yes" }],
      blocking: false,
      created_at: "2026-05-06T00:00:00Z",
    };
    setPending([interview]);
    render(wrap(<InterviewBar />));

    expect(screen.queryByText("EXTERNAL ACTION")).not.toBeInTheDocument();
    expect(
      screen.getByText("background information from the agent"),
    ).toBeInTheDocument();
  });
});

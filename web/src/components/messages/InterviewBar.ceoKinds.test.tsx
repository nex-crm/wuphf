/**
 * InterviewBar CEO kinds tests (Phase 2 onboarding)
 *
 * Tests cover:
 * 1. Each of the 5 new CEO card kinds renders correctly in "pending" state
 * 2. Pending → submitting → committed lifecycle for form_field + chip_row + checklist
 * 3. XSS attack strings in payload render as text, not HTML (PR #684 regression)
 * 4. ceo_scan_chip renders all three status states
 *
 * The CeoCardSection reads from OnboardingDMContext. We provide the context
 * directly via the exported OnboardingDMContextProvider.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OnboardingDMContextProvider } from "../onboarding/OnboardingDMRoute";
import type {
  CeoChecklistPayload,
  CeoChipRowPayload,
  CeoFormFieldPayload,
  CeoScanChipPayload,
  CeoSuggestion,
} from "../onboarding/types";
import { CeoChecklist } from "./cards/CeoChecklist";
import { CeoChipRow } from "./cards/CeoChipRow";
import { CeoFormField } from "./cards/CeoFormField";
import { CeoScanChip } from "./cards/CeoScanChip";
// We test CeoCardSection, the full InterviewBar mount, and individual cards.
import { CeoCardSection, InterviewBar } from "./InterviewBar";

// ── Mocks ─────────────────────────────────────────────────────────────────

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    get: vi.fn(),
    post: vi.fn(),
    postMessage: vi.fn(),
  };
});

vi.mock("../../hooks/useRequests", () => ({
  useRequests: () => ({ pending: [] }),
}));

import { post, postMessage } from "../../api/client";

const postMock = vi.mocked(post);
const postMessageMock = vi.mocked(postMessage);

// ── Context helper ────────────────────────────────────────────────────────

function makeWrapper(suggestion: CeoSuggestion | null) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    return (
      <QueryClientProvider client={qc}>
        <OnboardingDMContextProvider
          value={{ phase: "identity", pendingSuggestion: suggestion }}
        >
          {children}
        </OnboardingDMContextProvider>
      </QueryClientProvider>
    );
  };
}

beforeEach(() => {
  postMock.mockReset();
  postMock.mockResolvedValue({});
  postMessageMock.mockReset();
  postMessageMock.mockResolvedValue({
    id: "msg-stub",
    from: "human",
    channel: "ceo__human",
    content: "",
    timestamp: new Date().toISOString(),
  });
});

afterEach(() => {
  cleanup();
});

// ── ceo_form_field ─────────────────────────────────────────────────────────

describe("CeoFormField", () => {
  const formPayload: CeoFormFieldPayload = {
    field: "company_name",
    label: "Office name?",
    optional: false,
    placeholder: "e.g. Acme Billing",
  };

  it("renders in pending state with label and input", () => {
    render(
      <CeoFormField payload={formPayload} stage="pending" onSubmit={vi.fn()} />,
    );
    expect(screen.getByTestId("ceo-form-field")).toBeInTheDocument();
    expect(screen.getByRole("textbox")).toBeInTheDocument();
    expect(screen.getByText("Office name?")).toBeInTheDocument();
  });

  it("disables input and shows spinner in submitting state", () => {
    render(
      <CeoFormField
        payload={formPayload}
        stage="submitting"
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(screen.getByText(/Saving/i)).toBeInTheDocument();
  });

  it("renders committed line with check mark in committed state", () => {
    render(
      <CeoFormField
        payload={formPayload}
        stage="committed"
        committedValue="Acme Billing"
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("✓");
    expect(screen.getByRole("status")).toHaveTextContent("Acme Billing");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("calls onSubmit with field and value on Enter", () => {
    const onSubmit = vi.fn();
    render(
      <CeoFormField
        payload={formPayload}
        stage="pending"
        onSubmit={onSubmit}
      />,
    );
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Acme Billing" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("company_name", "Acme Billing");
  });

  it("calls onSkip when Skip chip is clicked (optional field)", () => {
    const onSkip = vi.fn();
    render(
      <CeoFormField
        payload={{ ...formPayload, optional: true }}
        stage="pending"
        onSubmit={vi.fn()}
        onSkip={onSkip}
      />,
    );
    fireEvent.click(screen.getByText("Skip"));
    expect(onSkip).toHaveBeenCalledWith("company_name");
  });

  it("XSS: renders script tag as text, not executed HTML", () => {
    const xssPayload: CeoFormFieldPayload = {
      field: "company_name",
      label: '<script>alert("xss")</script>',
      optional: false,
    };
    render(
      <CeoFormField payload={xssPayload} stage="pending" onSubmit={vi.fn()} />,
    );
    // The script should appear as literal text in the DOM, not as an element.
    expect(document.querySelector("script")).not.toBeInTheDocument();
    expect(
      screen.getByText('<script>alert("xss")</script>'),
    ).toBeInTheDocument();
  });

  it("XSS: img onerror attack string renders as text", () => {
    const xssPayload: CeoFormFieldPayload = {
      field: "company_name",
      label: "<img src=x onerror=alert(1)>",
      optional: false,
    };
    render(
      <CeoFormField payload={xssPayload} stage="pending" onSubmit={vi.fn()} />,
    );
    expect(document.querySelector("img")).not.toBeInTheDocument();
    expect(
      screen.getByText("<img src=x onerror=alert(1)>"),
    ).toBeInTheDocument();
  });
});

// ── ceo_chip_row ──────────────────────────────────────────────────────────

describe("CeoChipRow", () => {
  const chipPayload: CeoChipRowPayload = {
    field: "blueprint_id",
    label: "Pick a starter template:",
    options: [
      { id: "content-ops", label: "Content Ops" },
      { id: "engineering-team", label: "Engineering Team" },
      { id: "scratch", label: "Start from scratch" },
    ],
  };

  it("renders all chips in pending state", () => {
    render(
      <CeoChipRow payload={chipPayload} stage="pending" onSubmit={vi.fn()} />,
    );
    expect(screen.getByTestId("ceo-chip-row")).toBeInTheDocument();
    expect(screen.getByText("Content Ops")).toBeInTheDocument();
    expect(screen.getByText("Engineering Team")).toBeInTheDocument();
    expect(screen.getByText("Start from scratch")).toBeInTheDocument();
  });

  it("calls onSubmit immediately when a chip is clicked", () => {
    const onSubmit = vi.fn();
    render(
      <CeoChipRow payload={chipPayload} stage="pending" onSubmit={onSubmit} />,
    );
    fireEvent.click(screen.getByText("Content Ops"));
    expect(onSubmit).toHaveBeenCalledWith("blueprint_id", "content-ops");
  });

  it("disables all chips in submitting state", () => {
    render(
      <CeoChipRow
        payload={chipPayload}
        stage="submitting"
        onSubmit={vi.fn()}
      />,
    );
    const buttons = screen.getAllByRole("option");
    for (const btn of buttons) {
      expect(btn).toBeDisabled();
    }
  });

  it("renders committed state as one-line with check mark", () => {
    render(
      <CeoChipRow
        payload={chipPayload}
        stage="committed"
        committedValue="Content Ops"
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("✓");
    expect(screen.getByRole("status")).toHaveTextContent("Content Ops");
  });

  it("XSS: chip label renders as text not HTML", () => {
    const xssChipPayload: CeoChipRowPayload = {
      field: "blueprint_id",
      label: "Pick",
      options: [{ id: "x", label: "<img src=x onerror=alert(1)>" }],
    };
    render(
      <CeoChipRow
        payload={xssChipPayload}
        stage="pending"
        onSubmit={vi.fn()}
      />,
    );
    expect(document.querySelector("img")).not.toBeInTheDocument();
  });
});

// ── ceo_checklist ─────────────────────────────────────────────────────────

describe("CeoChecklist", () => {
  const checklistPayload: CeoChecklistPayload = {
    field: "picked_agents",
    label: "Keep or trim your team:",
    items: [
      { id: "writer", label: "Writer", default_checked: true },
      { id: "editor", label: "Editor", default_checked: true },
      { id: "designer", label: "Designer", default_checked: false },
    ],
    submit_label: "Confirm team",
  };

  it("renders all items with default checked state", () => {
    render(
      <CeoChecklist
        payload={checklistPayload}
        stage="pending"
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByTestId("ceo-checklist")).toBeInTheDocument();
    const writerBox = screen.getByLabelText("Writer");
    const designerBox = screen.getByLabelText("Designer");
    expect(writerBox).toBeChecked();
    expect(designerBox).not.toBeChecked();
  });

  it("toggles checkbox on click", () => {
    render(
      <CeoChecklist
        payload={checklistPayload}
        stage="pending"
        onSubmit={vi.fn()}
      />,
    );
    const designerBox = screen.getByLabelText("Designer");
    fireEvent.click(designerBox);
    expect(designerBox).toBeChecked();
  });

  it("calls onSubmit with selected agent IDs", () => {
    const onSubmit = vi.fn();
    render(
      <CeoChecklist
        payload={checklistPayload}
        stage="pending"
        onSubmit={onSubmit}
      />,
    );
    fireEvent.click(screen.getByText("Confirm team"));
    expect(onSubmit).toHaveBeenCalledWith(
      "picked_agents",
      expect.arrayContaining(["writer", "editor"]),
    );
  });

  it("disables all items in submitting state", () => {
    render(
      <CeoChecklist
        payload={checklistPayload}
        stage="submitting"
        onSubmit={vi.fn()}
      />,
    );
    const checkboxes = screen.getAllByRole("checkbox");
    for (const cb of checkboxes) {
      expect(cb).toBeDisabled();
    }
  });

  it("renders committed state as one-line with check mark", () => {
    render(
      <CeoChecklist
        payload={checklistPayload}
        stage="committed"
        committedValue={["Writer", "Editor"]}
        onSubmit={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("✓");
    expect(screen.getByRole("status")).toHaveTextContent("Writer");
    expect(screen.getByRole("status")).toHaveTextContent("Editor");
  });

  it("XSS: checklist item label renders as text not HTML", () => {
    const xssPayload: CeoChecklistPayload = {
      field: "agents",
      label: "Team",
      items: [{ id: "x", label: '<script>alert("xss")</script>' }],
    };
    render(
      <CeoChecklist payload={xssPayload} stage="pending" onSubmit={vi.fn()} />,
    );
    expect(document.querySelector("script")).not.toBeInTheDocument();
    expect(
      screen.getByText('<script>alert("xss")</script>'),
    ).toBeInTheDocument();
  });
});

// ── ceo_scan_chip ─────────────────────────────────────────────────────────

describe("CeoScanChip", () => {
  it("renders scanning state", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "scanning",
    };
    render(<CeoScanChip payload={payload} />);
    const chip = screen.getByTestId("ceo-scan-chip");
    expect(chip).toHaveClass("ceo-scan-chip--scanning");
    expect(chip).toHaveTextContent("Scanning acme.com");
  });

  it("renders done state with success label", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "done",
    };
    render(<CeoScanChip payload={payload} />);
    const chip = screen.getByTestId("ceo-scan-chip");
    expect(chip).toHaveClass("ceo-scan-chip--done");
    expect(chip).toHaveTextContent("Wiki updated");
  });

  it("renders failed state with error label", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "failed",
    };
    render(<CeoScanChip payload={payload} />);
    const chip = screen.getByTestId("ceo-scan-chip");
    expect(chip).toHaveClass("ceo-scan-chip--failed");
    // Use partial match to avoid unicode apostrophe mismatch
    expect(chip.textContent).toContain("read that URL");
  });

  it("surfaces the error reason inline on failure (#934)", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "failed",
      error_reason: "URL fetch failed: 404 Not Found",
    };
    render(<CeoScanChip payload={payload} />);
    expect(screen.getByTestId("ceo-scan-chip-reason")).toHaveTextContent(
      "URL fetch failed: 404 Not Found",
    );
  });

  it("renders Try another URL and Skip CTAs on failure (#934)", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "failed",
    };
    render(<CeoScanChip payload={payload} />);
    expect(screen.getByTestId("ceo-scan-chip-retry")).toBeInTheDocument();
    expect(screen.getByTestId("ceo-scan-chip-skip")).toBeInTheDocument();
  });

  it("does NOT render CTAs on the scanning or done chip", () => {
    const scanning: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "scanning",
    };
    const { unmount } = render(<CeoScanChip payload={scanning} />);
    expect(screen.queryByTestId("ceo-scan-chip-actions")).toBeNull();
    unmount();

    const done: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "done",
    };
    render(<CeoScanChip payload={done} />);
    expect(screen.queryByTestId("ceo-scan-chip-actions")).toBeNull();
  });

  it("clicking Try another URL transitions back to PhaseWebsite (#934)", async () => {
    postMock.mockResolvedValue({ ok: true, phase: "website" });
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "failed",
    };
    render(<CeoScanChip payload={payload} />);
    fireEvent.click(screen.getByTestId("ceo-scan-chip-retry"));
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "website",
      }),
    );
  });

  it("clicking Skip and continue transitions to PhaseBlueprint (#934)", async () => {
    postMock.mockResolvedValue({ ok: true, phase: "blueprint" });
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "acme.com",
      status: "failed",
    };
    render(<CeoScanChip payload={payload} />);
    fireEvent.click(screen.getByTestId("ceo-scan-chip-skip"));
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "blueprint",
      }),
    );
  });

  it("uses custom labels when provided", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: "example.com",
      status: "done",
      done_label: "Wiki updated with example.com facts",
    };
    render(<CeoScanChip payload={payload} />);
    expect(screen.getByTestId("ceo-scan-chip")).toHaveTextContent(
      "Wiki updated with example.com facts",
    );
  });

  it("XSS: URL renders as text not HTML", () => {
    const payload: CeoScanChipPayload = {
      field: "website_url",
      url: '<script>alert("xss")</script>',
      status: "scanning",
    };
    render(<CeoScanChip payload={payload} />);
    expect(document.querySelector("script")).not.toBeInTheDocument();
  });
});

// ── CeoCardSection integration (via context) ─────────────────────────────

describe("CeoCardSection", () => {
  it("renders nothing when there is no pending suggestion", () => {
    render(<CeoCardSection />, { wrapper: makeWrapper(null) });
    expect(screen.queryByTestId("ceo-card-section")).not.toBeInTheDocument();
  });

  it("renders a ceo_form_field card when suggestion is set", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-1",
      phase: "identity",
      kind: "ceo_form_field",
      payload: {
        field: "company_name",
        label: "Office name?",
        optional: false,
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });
    expect(screen.getByTestId("ceo-card-section")).toBeInTheDocument();
    expect(screen.getByTestId("ceo-form-field")).toBeInTheDocument();
  });

  it("transitions to committed after successful POST to /onboarding/answer", async () => {
    postMock.mockResolvedValue({});
    const suggestion: CeoSuggestion = {
      id: "sug-2",
      phase: "identity",
      kind: "ceo_form_field",
      payload: {
        field: "company_name",
        label: "Office name?",
        optional: false,
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "Acme Inc" } });
    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/answer", {
        field: "company_name",
        value: "Acme Inc",
      }),
    );
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "identity",
    });

    // After commit the section disappears (stage="committed" hides it)
    await waitFor(() =>
      expect(screen.queryByTestId("ceo-card-section")).not.toBeInTheDocument(),
    );
  });

  it("advances from blueprint pick to the team trim phase", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-blueprint",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: {
        field: "blueprint_id",
        label: "Pick a template:",
        options: [{ id: "niche-crm", label: "Niche CRM" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Niche CRM"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/answer", {
        field: "blueprint_id",
        value: "niche-crm",
      }),
    );
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "team",
    });
  });

  it("advances the empty blueprint id as the scratch path", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-blueprint-scratch",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: {
        field: "blueprint_id",
        label: "Pick a template:",
        options: [{ id: "", label: "Start from scratch" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Start from scratch"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/answer", {
        field: "blueprint_id",
        value: "",
      }),
    );
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "seed",
    });
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "bridge",
    });
  });

  it("advances legacy scratch blueprint ids as the scratch path", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-blueprint-legacy-scratch",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: {
        field: "blueprint_id",
        label: "Pick a template:",
        options: [{ id: "from-scratch", label: "Start from scratch" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Start from scratch"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "seed",
      }),
    );
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "bridge",
    });
  });

  it("transitions bridge choices without posting them as form answers", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-bridge",
      phase: "bridge",
      kind: "ceo_chip_row",
      payload: {
        field: "bridge_choice",
        label: "All set up. What would you like to do?",
        options: [{ id: "look_around", label: "Look around first" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Look around first"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "complete",
      }),
    );
    expect(postMock).not.toHaveBeenCalledWith("/onboarding/answer", {
      field: "bridge_choice",
      value: "look_around",
    });
  });

  it("starts the issue-draft phase from the start-issue bridge chip", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-bridge-start",
      phase: "bridge",
      kind: "ceo_chip_row",
      payload: {
        field: "bridge_choice",
        label: "All set up. What would you like to do?",
        options: [{ id: "start_issue", label: "Start an issue" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Start an issue"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "draft",
      }),
    );
  });

  it("persists the first issue prompt and advances to approve", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-first-issue",
      phase: "draft",
      kind: "ceo_form_field",
      payload: {
        field: "task_prompt",
        label: "What should the team tackle first?",
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.change(screen.getByLabelText("What should the team tackle first?"), {
      target: { value: "Build Stripe webhooks" },
    });
    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/answer", {
        field: "task_prompt",
        value: "Build Stripe webhooks",
      }),
    );
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "approve",
    });
  });

  it("renders a ceo_chip_row card", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-3",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: {
        field: "blueprint_id",
        label: "Pick a template:",
        options: [
          { id: "scratch", label: "Start from scratch" },
          { id: "content-ops", label: "Content Ops" },
        ],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });
    expect(screen.getByTestId("ceo-chip-row")).toBeInTheDocument();
  });

  it("renders a ceo_checklist card", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-4",
      phase: "team",
      kind: "ceo_checklist",
      payload: {
        field: "picked_agents",
        label: "Keep or trim:",
        items: [{ id: "eng", label: "Engineer", default_checked: true }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });
    expect(screen.getByTestId("ceo-checklist")).toBeInTheDocument();
  });

  it("renders a ceo_team_trim card (alias for checklist)", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-5",
      phase: "team",
      kind: "ceo_team_trim",
      payload: {
        field: "picked_agents",
        label: "Your team:",
        items: [{ id: "pm", label: "PM", default_checked: true }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });
    expect(screen.getByTestId("ceo-checklist")).toBeInTheDocument();
  });

  it("renders a ceo_scan_chip card", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-6",
      phase: "scan",
      kind: "ceo_scan_chip",
      payload: {
        field: "website_url",
        url: "acme.com",
        status: "scanning",
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });
    expect(screen.getByTestId("ceo-scan-chip")).toBeInTheDocument();
  });

  // ── Human-echo (#978) ──────────────────────────────────────────────────
  //
  // After the user commits a form-field / chip / checklist answer, the
  // wizard mirrors that answer back into the CEO DM as a human chat bubble
  // so the transcript reads like a real conversation. The non-conversational
  // bridge_choice action is excluded.

  it("echoes a form-field answer into the CEO DM after submit", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-echo-form",
      phase: "greet",
      kind: "ceo_form_field",
      payload: { field: "company_name", label: "Office name?" },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Acme Test QA" },
    });
    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() =>
      expect(postMessageMock).toHaveBeenCalledWith(
        "Acme Test QA",
        "ceo__human",
      ),
    );
  });

  it("echoes a chip-row choice as its human-readable label", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-echo-chip",
      phase: "blueprint",
      kind: "ceo_chip_row",
      payload: {
        field: "blueprint_id",
        label: "Pick a template",
        options: [{ id: "niche-crm", label: "Niche CRM" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Niche CRM"));

    await waitFor(() =>
      expect(postMessageMock).toHaveBeenCalledWith("Niche CRM", "ceo__human"),
    );
  });

  it("does NOT echo bridge_choice into the CEO DM", async () => {
    const suggestion: CeoSuggestion = {
      id: "sug-echo-bridge",
      phase: "bridge",
      kind: "ceo_chip_row",
      payload: {
        field: "bridge_choice",
        label: "What next?",
        options: [{ id: "look_around", label: "Look around first" }],
      },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.click(screen.getByText("Look around first"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "complete",
      }),
    );
    expect(postMessageMock).not.toHaveBeenCalled();
  });

  it("survives an echo postMessage failure without blocking the wizard", async () => {
    postMessageMock.mockRejectedValueOnce(new Error("network down"));
    const suggestion: CeoSuggestion = {
      id: "sug-echo-fail",
      phase: "greet",
      kind: "ceo_form_field",
      payload: { field: "company_name", label: "Office name?" },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Acme" },
    });
    fireEvent.click(screen.getByText("Submit"));

    // The transition still fires even though the echo POST failed.
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "identity",
      }),
    );
  });

  // Regression guard for CodeRabbit on PR #988: the echo must be detached
  // from the submit critical path. If postMessage hangs indefinitely the
  // wizard must still advance — onboarding state is already committed via
  // /onboarding/answer; the echo is best-effort UI sugar.
  it("does not block wizard advance when echo postMessage hangs", async () => {
    // Never resolves — simulates a hung /messages call.
    postMessageMock.mockImplementationOnce(() => new Promise(() => {}));
    const suggestion: CeoSuggestion = {
      id: "sug-echo-hang",
      phase: "greet",
      kind: "ceo_form_field",
      payload: { field: "company_name", label: "Office name?" },
    };
    render(<CeoCardSection />, { wrapper: makeWrapper(suggestion) });

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Acme" },
    });
    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
        phase: "identity",
      }),
    );
  });
});

// ── InterviewBar integration ──────────────────────────────────────────────

describe("InterviewBar", () => {
  it("renders the CEO card even when the regular request queue is empty", () => {
    const suggestion: CeoSuggestion = {
      id: "sug-interview-empty",
      phase: "greet",
      kind: "ceo_form_field",
      payload: {
        field: "company_name",
        label: "Office name?",
        optional: false,
      },
    };

    render(<InterviewBar />, { wrapper: makeWrapper(suggestion) });

    expect(screen.getByTestId("ceo-card-section")).toBeInTheDocument();
    expect(screen.getByTestId("ceo-form-field")).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Pending agent request" }),
    ).not.toBeInTheDocument();
  });
});

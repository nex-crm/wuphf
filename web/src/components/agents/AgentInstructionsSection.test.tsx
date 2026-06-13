import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const readAgentFileMock = vi.hoisted(() => vi.fn());
const writeAgentFileMock = vi.hoisted(() => vi.fn());
const generateAgentFileMock = vi.hoisted(() => vi.fn());

vi.mock("../../api/agentFiles", async () => {
  const actual = await vi.importActual<typeof import("../../api/agentFiles")>(
    "../../api/agentFiles",
  );
  return {
    ...actual,
    readAgentFile: readAgentFileMock,
    writeAgentFile: writeAgentFileMock,
    generateAgentFile: generateAgentFileMock,
  };
});

// Stub the heavy Tiptap editor: assert the wiring (path + save/cancel) without
// loading the lazy rich-editor chunk in jsdom.
vi.mock("../wiki/WikiEditor", () => ({
  default: ({
    path,
    initialContent,
    onSaved,
    onCancel,
  }: {
    path: string;
    initialContent: string;
    onSaved: (sha: string) => void;
    onCancel: () => void;
  }) => (
    <div data-testid="wiki-editor-stub">
      <span data-testid="editor-path">{path}</span>
      <span data-testid="editor-initial">{initialContent}</span>
      <button type="button" onClick={() => onSaved("newsha")}>
        stub-save
      </button>
      <button type="button" onClick={onCancel}>
        stub-cancel
      </button>
    </div>
  ),
}));

import type { OfficeMember } from "../../api/client";
import { AgentInstructionsSection } from "./AgentInstructionsSection";

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function wrap(ui: ReactNode) {
  return <QueryClientProvider client={makeQC()}>{ui}</QueryClientProvider>;
}

const specialist: OfficeMember = {
  slug: "growth",
  name: "Growth Lead",
  role: "growth lead",
  built_in: false,
};

const lead: OfficeMember = {
  slug: "ceo",
  name: "CEO",
  role: "lead",
  built_in: true,
};

describe("<AgentInstructionsSection>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    readAgentFileMock.mockResolvedValue({
      path: "agents/growth/SOUL.md",
      content: "# SOUL — @growth\nbe relentless",
      sha: "abc123",
      exists: true,
    });
  });

  it("lists the four instruction files and does not fetch until expanded", () => {
    render(wrap(<AgentInstructionsSection agent={specialist} />));
    for (const name of ["SOUL", "IDENTITY", "OPERATIONS", "TOOLS"]) {
      expect(screen.getByText(name)).toBeInTheDocument();
    }
    // Collapsed cards must not fetch.
    expect(readAgentFileMock).not.toHaveBeenCalled();
  });

  it("does not show the office USER file for a non-lead agent", () => {
    render(wrap(<AgentInstructionsSection agent={specialist} />));
    expect(screen.queryByText("USER")).not.toBeInTheDocument();
    expect(screen.queryByText(/office context/i)).not.toBeInTheDocument();
  });

  it("shows the office USER file for the lead agent", () => {
    render(wrap(<AgentInstructionsSection agent={lead} />));
    expect(screen.getByText("USER")).toBeInTheDocument();
    expect(screen.getByText(/office context/i)).toBeInTheDocument();
  });

  it("fetches and renders a file's content when its card is expanded", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));

    await waitFor(() => {
      expect(readAgentFileMock).toHaveBeenCalledWith("agents/growth/SOUL.md");
    });
    expect(await screen.findByText(/be relentless/)).toBeInTheDocument();
    // Edit affordance is present in view mode.
    expect(screen.getByText("Edit")).toBeInTheDocument();
  });

  it("opens the raw markdown editor on Edit (faithful default)", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    await screen.findByText("Edit");
    await user.click(screen.getByText("Edit"));

    // Definition files default to a faithful raw-markdown textarea, not the
    // rich Tiptap editor (which normalizes markdown / drops HTML comments).
    const raw = await screen.findByLabelText(/raw markdown editor for SOUL/i);
    expect(raw).toBeInTheDocument();
    expect((raw as HTMLTextAreaElement).value).toMatch(/be relentless/i);
  });

  it("can switch to the rich editor and save", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    await screen.findByText("Edit");
    await user.click(screen.getByText("Edit"));
    await screen.findByLabelText(/raw markdown editor for SOUL/i);

    // Toggle to Rich → the reused wiki editor mounts seeded with the file.
    await user.click(screen.getByRole("button", { name: "Rich" }));
    const editor = await screen.findByTestId("wiki-editor-stub");
    expect(editor).toBeInTheDocument();
    expect(screen.getByTestId("editor-path")).toHaveTextContent(
      "agents/growth/SOUL.md",
    );

    // Saving exits edit mode (back to the view with the Edit button).
    await user.click(screen.getByText("stub-save"));
    await waitFor(() => {
      expect(screen.queryByTestId("wiki-editor-stub")).not.toBeInTheDocument();
    });
    expect(screen.getByText("Edit")).toBeInTheDocument();
  });

  it("surfaces the seeded badge when a file has not been written yet", async () => {
    readAgentFileMock.mockResolvedValue({
      path: "agents/growth/SOUL.md",
      content: "# SOUL — @growth\nseed",
      sha: "",
      exists: false,
    });
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    expect(await screen.findByText(/seeded/)).toBeInTheDocument();
  });

  it("offers Generate with AI on prose files but not factual ones", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    // SOUL (prose) → Generate offered.
    await user.click(screen.getByText("SOUL"));
    await screen.findByText("Edit");
    expect(screen.getByText("Generate with AI")).toBeInTheDocument();

    // IDENTITY (factual) → no Generate affordance.
    readAgentFileMock.mockResolvedValue({
      path: "agents/growth/IDENTITY.md",
      content: "# IDENTITY — @growth",
      sha: "i1",
      exists: true,
    });
    await user.click(screen.getByText("IDENTITY"));
    await waitFor(() => {
      expect(screen.getAllByText("Edit").length).toBeGreaterThan(0);
    });
    // Only the SOUL card's Generate button exists; IDENTITY adds none.
    expect(screen.getAllByText("Generate with AI")).toHaveLength(1);
  });

  it("generates a draft and opens the editor seeded with it", async () => {
    generateAgentFileMock.mockResolvedValue({
      path: "agents/growth/SOUL.md",
      content: "# SOUL — @growth\nAI-authored, vivid and specific",
    });
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    await user.click(await screen.findByText("Generate with AI"));

    await waitFor(() => {
      expect(generateAgentFileMock).toHaveBeenCalledWith(
        "agents/growth/SOUL.md",
      );
    });
    // Editor opens (raw mode, the faithful default) seeded with the generated
    // draft — not the on-disk content. Regression: generate must seed rawDraft,
    // or the draft would be invisible in the default raw textarea.
    const raw = await screen.findByLabelText(/raw markdown editor for SOUL/i);
    expect((raw as HTMLTextAreaElement).value).toMatch(
      /AI-authored, vivid and specific/,
    );
  });

  it("surfaces a generation error without opening the editor", async () => {
    generateAgentFileMock.mockRejectedValue(new Error("model unavailable"));
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    await user.click(await screen.findByText("Generate with AI"));

    expect(await screen.findByText("model unavailable")).toBeInTheDocument();
    expect(screen.queryByTestId("wiki-editor-stub")).not.toBeInTheDocument();
  });
});

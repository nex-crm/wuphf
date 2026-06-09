import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const readAgentFileMock = vi.hoisted(() => vi.fn());
const writeAgentFileMock = vi.hoisted(() => vi.fn());

vi.mock("../../api/agentFiles", async () => {
  const actual = await vi.importActual<typeof import("../../api/agentFiles")>(
    "../../api/agentFiles",
  );
  return {
    ...actual,
    readAgentFile: readAgentFileMock,
    writeAgentFile: writeAgentFileMock,
  };
});

// Stub the heavy Tiptap editor: assert the wiring (path + save/cancel) without
// loading the lazy rich-editor chunk in jsdom.
vi.mock("../wiki/WikiEditor", () => ({
  default: ({
    path,
    onSaved,
    onCancel,
  }: {
    path: string;
    onSaved: (sha: string) => void;
    onCancel: () => void;
  }) => (
    <div data-testid="wiki-editor-stub">
      <span data-testid="editor-path">{path}</span>
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

  it("opens the editor on Edit and returns to view on save", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentInstructionsSection agent={specialist} />));

    await user.click(screen.getByText("SOUL"));
    await screen.findByText("Edit");
    await user.click(screen.getByText("Edit"));

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
});

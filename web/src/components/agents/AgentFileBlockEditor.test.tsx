import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const writeAgentFileMock = vi.hoisted(() => vi.fn());

vi.mock("../../api/agentFiles", async () => {
  const actual = await vi.importActual<typeof import("../../api/agentFiles")>(
    "../../api/agentFiles",
  );
  return { ...actual, writeAgentFile: writeAgentFileMock };
});

import type { AgentFileResponse } from "../../api/agentFiles";
import { AgentFileBlockEditor } from "./AgentFileBlockEditor";

const SOUL = "# SOUL — @growth\n\n## Who you are\nold identity\n";

function makeData(sha: string): AgentFileResponse {
  return {
    path: "agents/growth/SOUL.md",
    content: SOUL,
    sha,
    exists: true,
  };
}

describe("<AgentFileBlockEditor>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    writeAgentFileMock.mockResolvedValue({
      path: "agents/growth/SOUL.md",
      commit_sha: "committed",
      bytes_written: 10,
    });
  });

  it("saves against the SHA snapshotted at edit-open, not a SHA that advanced mid-edit", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn();
    // Mount with the SHA the user started editing from.
    const { rerender } = render(
      <AgentFileBlockEditor
        path="agents/growth/SOUL.md"
        label="SOUL"
        data={makeData("sha-at-open")}
        onSaved={onSaved}
        onCancel={vi.fn()}
      />,
    );

    const block = await screen.findByRole("textbox", {
      name: /SOUL — Who you are/i,
    });
    fireEvent.change(block, { target: { value: "new identity" } });

    // A background refetch advances the live SHA while the editor stays open
    // (same content, so the parent does NOT remount the editor).
    rerender(
      <AgentFileBlockEditor
        path="agents/growth/SOUL.md"
        label="SOUL"
        data={makeData("sha-advanced")}
        onSaved={onSaved}
        onCancel={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(writeAgentFileMock).toHaveBeenCalled());
    const arg = writeAgentFileMock.mock.calls[0][0] as { expectedSha: string };
    // Must use the open-time SHA so a stale save becomes a conflict instead
    // of silently clobbering newer disk content.
    expect(arg.expectedSha).toBe("sha-at-open");
  });

  it("keeps an emptied schema section's heading but drops emptied custom sections", async () => {
    const user = userEvent.setup();
    const data: AgentFileResponse = {
      path: "agents/growth/SOUL.md",
      content:
        "# SOUL — @growth\n\n## Who you are\nkeep me\n\n## Extra\ndelete me\n",
      sha: "s1",
      exists: true,
    };
    render(
      <AgentFileBlockEditor
        path="agents/growth/SOUL.md"
        label="SOUL"
        data={data}
        onSaved={vi.fn()}
        onCancel={vi.fn()}
      />,
    );

    // Empty the custom "Extra" block (heading not in the SOUL schema).
    const extra = await screen.findByRole("textbox", { name: /SOUL — Extra/i });
    fireEvent.change(extra, { target: { value: "" } });
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(writeAgentFileMock).toHaveBeenCalled());
    const written = writeAgentFileMock.mock.calls[0][0] as { content: string };
    // Schema headings are preserved (empty-to-fill on reopen); the emptied
    // custom section is dropped (clearing it is an intentional delete).
    expect(written.content).toContain("## Who you are");
    expect(written.content).toContain("## Boundaries");
    expect(written.content).not.toContain("## Extra");
  });
});

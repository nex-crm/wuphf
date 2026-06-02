import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { WikiFSTreeNode } from "../../../api/wiki";
import * as wiki from "../../../api/wiki";
import WikiTree from "./WikiTree";

const TREE: WikiFSTreeNode[] = [
  {
    name: "people",
    path: "team/people",
    type: "dir",
    title: "People",
    children: [
      {
        name: "nazz.md",
        path: "team/people/nazz.md",
        type: "page",
        title: "Nazz",
      },
    ],
  },
  {
    name: "playbooks",
    path: "team/playbooks",
    type: "dir",
    title: "Playbooks",
    children: [
      {
        name: "churn.md",
        path: "team/playbooks/churn.md",
        type: "page",
        title: "Churn",
      },
    ],
  },
  {
    name: "assets",
    path: "team/assets",
    type: "dir",
    title: "Assets",
    children: [
      {
        name: "report.pdf",
        path: "team/assets/report.pdf",
        type: "file",
        title: "report.pdf",
        ext: ".pdf",
      },
      {
        name: "dashboard",
        path: "team/assets/dashboard",
        type: "app",
        title: "Dashboard",
      },
    ],
  },
];

describe("<WikiTree>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(wiki, "fetchWikiTree").mockResolvedValue(TREE);
  });

  it("renders top-level folders and expands them on click", async () => {
    render(<WikiTree onNavigate={() => {}} />);

    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    expect(screen.getByText("Playbooks")).toBeInTheDocument();
    // Children are collapsed initially.
    expect(screen.queryByText("Nazz")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    expect(screen.getByText("Nazz")).toBeInTheDocument();
  });

  it("navigates to a page on click", async () => {
    const onNavigate = vi.fn();
    render(<WikiTree onNavigate={onNavigate} />);

    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    fireEvent.click(screen.getByText("Nazz"));
    expect(onNavigate).toHaveBeenCalledWith("team/people/nazz.md");
  });

  it("navigates to a file leaf so the viewer opens", async () => {
    const onNavigate = vi.fn();
    render(<WikiTree onNavigate={onNavigate} />);

    await waitFor(() => expect(screen.getByText("Assets")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand Assets/i }));
    fireEvent.click(screen.getByText("report.pdf"));
    expect(onNavigate).toHaveBeenCalledWith("team/assets/report.pdf");
  });

  it("shows a coming-soon note for an app leaf instead of navigating", async () => {
    const onNavigate = vi.fn();
    render(<WikiTree onNavigate={onNavigate} />);

    await waitFor(() => expect(screen.getByText("Assets")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand Assets/i }));
    fireEvent.click(screen.getByText("Dashboard"));

    expect(onNavigate).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.getAllByText(/opens as an app — coming soon/i).length,
      ).toBeGreaterThan(0),
    );
  });

  it("uploads a file from the Upload dialog into the chosen folder", async () => {
    const uploadSpy = vi.spyOn(wiki, "uploadWikiFile").mockResolvedValue({
      path: "team/assets/notes.txt",
      commit_sha: "abc1234",
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Upload file" }));
    const dialog = screen.getByTestId("wk-tree-upload");

    const fileInput = dialog.querySelector(
      "#wk-tree2-upload-file",
    ) as HTMLInputElement;
    const file = new File(["hello"], "notes.txt", { type: "text/plain" });
    fireEvent.change(fileInput, { target: { files: [file] } });

    fireEvent.click(screen.getByRole("button", { name: "Upload" }));

    await waitFor(() => expect(uploadSpy).toHaveBeenCalledTimes(1));
    // dir defaults to the first folder option (team root); file is forwarded.
    expect(uploadSpy.mock.calls[0][1]).toBe(file);
    await waitFor(() =>
      expect(screen.getAllByText(/Uploaded notes\.txt/).length).toBeGreaterThan(
        0,
      ),
    );
  });

  it("force-expands all folders while searching so deep matches show", async () => {
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText("Search files…"), {
      target: { value: "churn" },
    });
    expect(screen.getByText("Churn")).toBeInTheDocument();
    expect(screen.queryByText("Nazz")).toBeNull();
  });

  it("opens the row context menu", async () => {
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    fireEvent.click(
      screen.getByRole("button", { name: /Actions for People/i }),
    );
    expect(
      screen.getByRole("menuitem", { name: "New sub-page" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Rename" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Delete" }),
    ).toBeInTheDocument();
  });

  it("calls createPage from the New page dialog", async () => {
    const createSpy = vi.spyOn(wiki, "createPage").mockResolvedValue({
      path: "team/playbooks/onboarding.md",
      commit_sha: "abc1234",
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "+ New page" }));
    const dialog = screen.getByTestId("wk-tree-new-page");
    fireEvent.change(within(dialog, "Title"), {
      target: { value: "Onboarding" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create page" }));

    await waitFor(() => expect(createSpy).toHaveBeenCalledTimes(1));
    // TanStack Query passes a trailing mutation-context arg to mutationFn.
    expect(createSpy.mock.calls[0][0]).toEqual(
      expect.objectContaining({
        path: expect.stringContaining("onboarding.md"),
      }),
    );
  });

  it("calls renamePage after an inline rename", async () => {
    const renameSpy = vi.spyOn(wiki, "renamePage").mockResolvedValue({
      to: "team/people/nadia.md",
      commit_sha: "def5678",
      references_rewritten: 3,
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));

    fireEvent.click(screen.getByRole("button", { name: /Actions for Nazz/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }));

    const input = screen.getByLabelText("New name");
    fireEvent.change(input, { target: { value: "nadia" } });
    fireEvent.submit(input);

    await waitFor(() => expect(renameSpy).toHaveBeenCalledTimes(1));
    expect(renameSpy.mock.calls[0][0]).toEqual({
      path: "team/people/nazz.md",
      newName: "nadia",
    });
    // "Rewrote N links" surfaces from references_rewritten. The message renders
    // both in the always-mounted polite live region and the visible note, so
    // assert it appears (at least once) rather than requiring a single match.
    await waitFor(() =>
      expect(screen.getAllByText(/Rewrote 3 links/).length).toBeGreaterThan(0),
    );
  });

  it("confirms before calling deletePage", async () => {
    const deleteSpy = vi.spyOn(wiki, "deletePage").mockResolvedValue({
      path: "team/people/nazz.md",
      commit_sha: "9990000",
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));

    fireEvent.click(screen.getByRole("button", { name: /Actions for Nazz/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    // The confirm dialog appears first — no delete yet.
    expect(screen.getByTestId("wk-tree-delete-confirm")).toBeInTheDocument();
    expect(deleteSpy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteSpy).toHaveBeenCalledTimes(1));
    expect(deleteSpy.mock.calls[0][0]).toBe("team/people/nazz.md");
  });

  it("shows an error note when a mutation fails", async () => {
    vi.spyOn(wiki, "deletePage").mockRejectedValue(new Error("broker down"));
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    fireEvent.click(screen.getByRole("button", { name: /Actions for Nazz/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    // Errors surface in the assertive alert live region and the visible note.
    await waitFor(() =>
      expect(screen.getAllByText("broker down").length).toBeGreaterThan(0),
    );
    // The assertive live region carries the announcement for AT.
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("broker down");
  });
});

// ── Accessibility — WAI-ARIA tree pattern ───────────────────────────────────

describe("<WikiTree> — accessibility", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(wiki, "fetchWikiTree").mockResolvedValue(TREE);
  });

  it("exposes role=tree with treeitem rows and aria-level", async () => {
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    const tree = screen.getByRole("tree", { name: "Wiki files" });
    expect(tree).toBeInTheDocument();

    const items = screen.getAllByRole("treeitem");
    expect(items.length).toBeGreaterThanOrEqual(2);
    // Top-level folders sit at aria-level 1 and advertise expand state.
    const peopleRow = screen
      .getByText("People")
      .closest('[role="treeitem"]') as HTMLElement;
    expect(peopleRow).toHaveAttribute("aria-level", "1");
    expect(peopleRow).toHaveAttribute("aria-expanded", "false");

    // Expanding the folder nests its children under a role=group.
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    const group = screen.getByRole("group");
    expect(group).toBeInTheDocument();
    const nazzRow = screen
      .getByText("Nazz")
      .closest('[role="treeitem"]') as HTMLElement;
    expect(nazzRow).toHaveAttribute("aria-level", "2");
  });

  it("uses a roving tabindex: ArrowDown moves the active treeitem", async () => {
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    const peopleRow = screen
      .getByText("People")
      .closest('[role="treeitem"]') as HTMLElement;
    const playbooksRow = screen
      .getByText("Playbooks")
      .closest('[role="treeitem"]') as HTMLElement;

    // The first visible row owns the single tab stop; the rest are removed.
    await waitFor(() => expect(peopleRow).toHaveAttribute("tabindex", "0"));
    expect(playbooksRow).toHaveAttribute("tabindex", "-1");

    fireEvent.keyDown(peopleRow, { key: "ArrowDown" });

    // Active row (tab stop) moves to the next visible row.
    await waitFor(() => expect(playbooksRow).toHaveAttribute("tabindex", "0"));
    expect(peopleRow).toHaveAttribute("tabindex", "-1");
  });

  it("closes the kebab menu on Escape and returns focus to the kebab", async () => {
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());

    const kebab = screen.getByRole("button", { name: /Actions for People/i });
    fireEvent.click(kebab);

    const menu = screen.getByRole("menu");
    expect(menu).toBeInTheDocument();
    // First menuitem takes focus on open.
    await waitFor(() =>
      expect(
        screen.getByRole("menuitem", { name: "New sub-page" }),
      ).toHaveFocus(),
    );

    fireEvent.keyDown(menu, { key: "Escape" });

    await waitFor(() =>
      expect(screen.queryByRole("menu")).not.toBeInTheDocument(),
    );
    expect(kebab).toHaveFocus();
  });

  it("lands ConfirmDelete initial focus on Cancel, not Delete", async () => {
    vi.spyOn(wiki, "deletePage").mockResolvedValue({
      path: "team/people/nazz.md",
      commit_sha: "9990000",
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    fireEvent.click(screen.getByRole("button", { name: /Actions for Nazz/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    const dialog = screen.getByTestId("wk-tree-delete-confirm");
    expect(dialog).toBeInTheDocument();
    // A stray Enter must not destroy data, so Cancel — not Delete — gets focus.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus(),
    );
    expect(screen.getByRole("button", { name: "Cancel" })).not.toBe(
      document.querySelector(".wk-tree2-danger-btn"),
    );
  });

  it("closes the delete dialog on Escape", async () => {
    vi.spyOn(wiki, "deletePage").mockResolvedValue({
      path: "team/people/nazz.md",
      commit_sha: "9990000",
    });
    render(<WikiTree onNavigate={() => {}} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Expand People/i }));
    fireEvent.click(screen.getByRole("button", { name: /Actions for Nazz/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    const dialog = screen.getByTestId("wk-tree-delete-confirm");
    fireEvent.keyDown(dialog, { key: "Escape" });

    await waitFor(() =>
      expect(
        screen.queryByTestId("wk-tree-delete-confirm"),
      ).not.toBeInTheDocument(),
    );
  });
});

/** Find a labelled input inside a container by its visible <label> text. */
function within(container: HTMLElement, labelText: string): HTMLElement {
  const labels = Array.from(container.querySelectorAll("label"));
  const label = labels.find((l) => l.textContent?.includes(labelText));
  const id = label?.getAttribute("for");
  const input = id ? container.querySelector(`#${id}`) : null;
  if (!(input instanceof HTMLElement)) {
    throw new Error(`No input found for label "${labelText}"`);
  }
  return input;
}

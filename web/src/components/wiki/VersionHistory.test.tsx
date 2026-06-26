import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/wiki";
import VersionHistory from "./VersionHistory";

const PATH = "team/people/customer-x.md";

const COMMITS: api.WikiHistoryCommit[] = [
  {
    sha: "ffffff9999999",
    author_slug: "pm",
    msg: "Tighten pilot scope",
    date: "2026-01-20T00:00:00Z",
  },
  {
    sha: "aaaaaa1111111",
    author_slug: "ceo",
    msg: "Initial brief",
    date: "2026-01-16T00:00:00Z",
  },
];

const SAMPLE_DIFF = [
  "diff --git a/team/people/customer-x.md b/team/people/customer-x.md",
  "index 1111111..2222222 100644",
  "--- a/team/people/customer-x.md",
  "+++ b/team/people/customer-x.md",
  "@@ -1,3 +1,3 @@",
  " # Customer X",
  "-Old pilot scope line.",
  "+New pilot scope line.",
  " End of file.",
].join("\n");

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: COMMITS });
  vi.spyOn(api, "fetchWikiDiff").mockResolvedValue({
    diff: SAMPLE_DIFF,
    sha: COMMITS[0].sha,
    path: PATH,
  });
  vi.spyOn(api, "restoreWikiVersion").mockResolvedValue({
    path: PATH,
    commit_sha: "restored0000000",
  });
});

describe("<VersionHistory>", () => {
  it("renders commits newest-first with author, relative time, and message", async () => {
    render(<VersionHistory path={PATH} />);

    expect(await screen.findByText("Tighten pilot scope")).toBeInTheDocument();
    expect(screen.getByText("Initial brief")).toBeInTheDocument();
    // Author slugs surface as formatted names.
    expect(screen.getByText("PM")).toBeInTheDocument();
    expect(screen.getByText("CEO")).toBeInTheDocument();
    // Short SHA (first 7 chars).
    expect(screen.getByText("ffffff9")).toBeInTheDocument();

    // List order matches the order returned (caller provides newest-first).
    const buttons = screen.getAllByRole("button");
    expect(buttons[0]).toHaveTextContent("Tighten pilot scope");
    expect(buttons[1]).toHaveTextContent("Initial brief");
  });

  it("shows an empty state when there is no history", async () => {
    vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: [] });
    render(<VersionHistory path={PATH} />);
    expect(
      await screen.findByText(/no version history yet/i),
    ).toBeInTheDocument();
  });

  it("shows an error state when the history fetch rejects", async () => {
    vi.spyOn(api, "fetchHistory").mockRejectedValue(new Error("git down"));
    render(<VersionHistory path={PATH} />);
    expect(
      await screen.findByText(/could not load version history/i),
    ).toBeInTheDocument();
  });

  it("fetches and renders a unified diff with coloured add/remove lines when a commit is selected", async () => {
    render(<VersionHistory path={PATH} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));

    await waitFor(() =>
      expect(api.fetchWikiDiff).toHaveBeenCalledWith(PATH, COMMITS[0].sha),
    );

    const added = await screen.findByText("+New pilot scope line.");
    expect(added).toHaveClass("wk-diff-added");
    const removed = screen.getByText("-Old pilot scope line.");
    expect(removed).toHaveClass("wk-diff-removed");
    // Hunk header coloured distinctly, file headers treated as meta.
    expect(screen.getByText("@@ -1,3 +1,3 @@")).toHaveClass("wk-diff-hunk");
    expect(screen.getByText("+++ b/team/people/customer-x.md")).toHaveClass(
      "wk-diff-meta",
    );
  });

  it("surfaces a diff error state when the diff fetch rejects", async () => {
    vi.spyOn(api, "fetchWikiDiff").mockRejectedValue(new Error("no diff"));
    render(<VersionHistory path={PATH} />);

    fireEvent.click(await screen.findByText("Initial brief"));
    expect(
      await screen.findByText(/could not load this version's diff/i),
    ).toBeInTheDocument();
  });

  it("requires confirmation before restoring, then calls restoreWikiVersion and onRestored", async () => {
    const onRestored = vi.fn();
    render(<VersionHistory path={PATH} onRestored={onRestored} />);

    // Select a commit so the diff pane + restore control appear.
    fireEvent.click(await screen.findByText("Tighten pilot scope"));
    const restoreBtn = await screen.findByRole("button", {
      name: /restore version ffffff9/i,
    });

    // First click only asks for confirmation — no API call yet.
    fireEvent.click(restoreBtn);
    expect(api.restoreWikiVersion).not.toHaveBeenCalled();
    expect(
      screen.getByText(/replace the current article with version/i),
    ).toBeInTheDocument();

    // Confirm — now the restore fires and onRestored gets the NEW sha.
    fireEvent.click(
      screen.getByRole("button", {
        name: /confirm restore of version ffffff9/i,
      }),
    );

    await waitFor(() =>
      expect(api.restoreWikiVersion).toHaveBeenCalledWith(PATH, COMMITS[0].sha),
    );
    await waitFor(() =>
      expect(onRestored).toHaveBeenCalledWith("restored0000000"),
    );
  });

  it("renders the confirm cluster as an alertdialog and moves focus to Cancel", async () => {
    render(<VersionHistory path={PATH} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));
    fireEvent.click(
      await screen.findByRole("button", { name: /restore version ffffff9/i }),
    );

    // The confirm cluster announces itself as an alertdialog labelled by the
    // consequence text.
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent(
      /replace the current article with version/i,
    );

    // Focus lands on Cancel (the safe default) when the prompt appears.
    const cancelBtn = screen.getByRole("button", { name: "Cancel" });
    await waitFor(() => expect(cancelBtn).toHaveFocus());
  });

  it("makes the diff region keyboard-focusable with a sha-scoped label", async () => {
    render(<VersionHistory path={PATH} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));

    const diffRegion = await screen.findByRole("region", {
      name: /diff for ffffff9/i,
    });
    expect(diffRegion).toHaveAttribute("tabindex", "0");
  });

  it("cancels the restore confirmation when Escape is pressed", async () => {
    const onRestored = vi.fn();
    render(<VersionHistory path={PATH} onRestored={onRestored} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));
    fireEvent.click(
      await screen.findByRole("button", { name: /restore version ffffff9/i }),
    );

    const dialog = await screen.findByRole("alertdialog");
    fireEvent.keyDown(dialog, { key: "Escape" });

    expect(api.restoreWikiVersion).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: /restore version ffffff9/i }),
    ).toBeInTheDocument();
  });

  it("can cancel the restore confirmation without calling restoreWikiVersion", async () => {
    const onRestored = vi.fn();
    render(<VersionHistory path={PATH} onRestored={onRestored} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));
    fireEvent.click(
      await screen.findByRole("button", { name: /restore version ffffff9/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(api.restoreWikiVersion).not.toHaveBeenCalled();
    expect(onRestored).not.toHaveBeenCalled();
    // Back to the plain "Restore this version" affordance.
    expect(
      screen.getByRole("button", { name: /restore version ffffff9/i }),
    ).toBeInTheDocument();
  });

  it("shows a restore error and does not call onRestored when restore fails", async () => {
    const onRestored = vi.fn();
    vi.spyOn(api, "restoreWikiVersion").mockRejectedValue(
      new Error("restore exploded"),
    );
    render(<VersionHistory path={PATH} onRestored={onRestored} />);

    fireEvent.click(await screen.findByText("Tighten pilot scope"));
    fireEvent.click(
      await screen.findByRole("button", { name: /restore version ffffff9/i }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /confirm restore of version ffffff9/i,
      }),
    );

    expect(await screen.findByText(/restore exploded/i)).toBeInTheDocument();
    expect(onRestored).not.toHaveBeenCalled();
  });
});

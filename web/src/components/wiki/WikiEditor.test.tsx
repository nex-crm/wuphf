import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/wiki";
import WikiEditor from "./WikiEditor";

// The real Tiptap editor lazy-loads ProseMirror + a stack of extensions, which
// is heavy and irrelevant to WikiEditor's wrapper responsibilities (draft /
// conflict / error banners, the commit input, Save/Cancel wiring). Stub it with
// a lightweight controlled textarea that mirrors the props.onChange contract so
// these tests exercise the wrapper without mounting ProseMirror.
vi.mock("./editor/TiptapWikiEditor", () => ({
  default: ({
    content,
    onChange,
  }: {
    content: string;
    onChange: (markdown: string) => void;
  }) => (
    <textarea
      data-testid="wk-tiptap-stub"
      value={content}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

const PATH = "team/people/nazz.md";
const DRAFT_KEY = `wuphf:draft:${PATH}`;
const SERVER_TS = "2026-04-20T10:00:00.000Z";

function setLocalStorageDraft(
  content: string,
  summary: string,
  savedAt: string,
) {
  window.localStorage.setItem(
    DRAFT_KEY,
    JSON.stringify({ content, summary, saved_at: savedAt }),
  );
}

/** The lazy editor resolves on a microtask; await the stub before asserting. */
async function findStub(): Promise<HTMLTextAreaElement> {
  return (await screen.findByTestId("wk-tiptap-stub")) as HTMLTextAreaElement;
}

describe("<WikiEditor>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("mounts the rich editor seeded with the article content and sends the expected SHA on save", async () => {
    const spy = vi.spyOn(api, "writeHumanArticle").mockResolvedValue({
      path: PATH,
      commit_sha: "abc1234",
      bytes_written: 42,
    });

    const onSaved = vi.fn();
    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="deadbee"
        serverLastEditedTs={SERVER_TS}
        onSaved={onSaved}
        onCancel={() => {}}
      />,
    );

    const editor = await findStub();
    expect(editor.value).toContain("Original.");

    fireEvent.change(editor, { target: { value: "# Nazz\n\nEdited." } });
    fireEvent.change(screen.getByTestId("wk-editor-commit"), {
      target: { value: "fix wording" },
    });
    fireEvent.click(screen.getByTestId("wk-editor-save"));

    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(spy).toHaveBeenCalledWith({
      path: PATH,
      content: "# Nazz\n\nEdited.",
      commitMessage: "fix wording",
      expectedSha: "deadbee",
    });
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith("abc1234"));
  });

  it("shows the conflict banner when the server returns 409", async () => {
    vi.spyOn(api, "writeHumanArticle").mockResolvedValue({
      conflict: true,
      error: "wiki: article changed since it was opened",
      current_sha: "newsha9",
      current_content: "# Nazz\n\nFresh text from someone else.",
    });

    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nMine."
        expectedSha="oldsha1"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    await findStub();
    fireEvent.click(screen.getByTestId("wk-editor-save"));

    const banner = await screen.findByText(/Someone else edited this article/);
    expect(banner).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Reload latest & re-apply/ }),
    ).toBeInTheDocument();
  });

  it("blocks save and surfaces an error when the content is emptied", async () => {
    const spy = vi.spyOn(api, "writeHumanArticle");
    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    const editor = await findStub();
    fireEvent.change(editor, { target: { value: "   " } });
    fireEvent.click(screen.getByTestId("wk-editor-save"));
    expect(spy).not.toHaveBeenCalled();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /cannot be empty/i,
    );
  });

  it("cancels via the Cancel button", async () => {
    const onCancel = vi.fn();
    render(
      <WikiEditor
        path={PATH}
        initialContent="x"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={onCancel}
      />,
    );
    await findStub();
    fireEvent.click(screen.getByTestId("wk-editor-cancel"));
    expect(onCancel).toHaveBeenCalled();
  });

  // ── Edit summary + help text ───────────────────────────────────────

  it("renders the edit-summary commit input and the slash/mention help hints", async () => {
    render(
      <WikiEditor
        path={PATH}
        initialContent="body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    await findStub();
    expect(screen.getByTestId("wk-editor-commit")).toBeInTheDocument();
    expect(screen.getByText(/Edit summary/)).toBeInTheDocument();
    const help = screen.getByText(/creates a wikilink/i);
    expect(help.textContent ?? "").toMatch(/Mod-e/);
    // The old plain-markdown / Rich-toggle wording is gone (the surviving
    // "toggles highlight" hint is unrelated to the removed mode toggle).
    expect(help.textContent ?? "").not.toMatch(/Plain markdown/i);
    expect(help.textContent ?? "").not.toMatch(/toggle\s+rich/i);
    // No source/preview/mode toggles or textarea remain.
    expect(screen.queryByTestId("wk-editor-textarea")).toBeNull();
    expect(screen.queryByTestId("wk-editor-mode-toggle")).toBeNull();
    expect(screen.queryByTestId("wk-editor-preview-toggle")).toBeNull();
    expect(screen.queryByTestId("wk-editor-mobile-tabs")).toBeNull();
  });
});

// ── Draft autosave ───────────────────────────────────────────────────
describe("<WikiEditor drafts>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("writes a debounced draft to localStorage after edits", async () => {
    render(
      <WikiEditor
        path={PATH}
        initialContent="Original."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    const editor = await findStub();
    // Switch to fake timers only after the lazy chunk + initial effects have
    // settled, so the debounce window is the only timer in flight.
    vi.useFakeTimers();
    fireEvent.change(editor, { target: { value: "Edited locally." } });
    // Before the debounce fires, nothing is persisted.
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull();
    // Advance past the debounce window.
    await act(async () => {
      vi.advanceTimersByTime(800);
    });
    const raw = window.localStorage.getItem(DRAFT_KEY);
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.content).toBe("Edited locally.");
  });

  it("shows the draft banner when localStorage has a newer draft than the server", async () => {
    const tenMinAfterServer = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("# Nazz\n\nDraft text.", "wip", tenMinAfterServer);

    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(
      await screen.findByTestId("wk-editor-draft-banner"),
    ).toBeInTheDocument();
  });

  it("restore copies the draft into the editor and hides the banner", async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("# Nazz\n\nDraft text.", "wip summary", tenMinAfter);

    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    fireEvent.click(await screen.findByTestId("wk-editor-draft-restore"));
    const editor = await findStub();
    expect(editor.value).toBe("# Nazz\n\nDraft text.");
    const commit = screen.getByTestId("wk-editor-commit") as HTMLInputElement;
    expect(commit.value).toBe("wip summary");
    expect(screen.queryByTestId("wk-editor-draft-banner")).toBeNull();
  });

  it("discard clears the draft from localStorage and hides the banner", async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("draft body", "", tenMinAfter);

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    fireEvent.click(await screen.findByTestId("wk-editor-draft-discard"));
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull();
    expect(screen.queryByTestId("wk-editor-draft-banner")).toBeNull();
  });

  it("hides the banner when the server is newer than the stored draft", async () => {
    // Draft saved before the server timestamp — the server won; draft stale.
    const oldDraft = new Date(
      Date.parse(SERVER_TS) - 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("stale draft", "", oldDraft);

    render(
      <WikiEditor
        path={PATH}
        initialContent="server wins"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    await findStub();
    expect(screen.queryByTestId("wk-editor-draft-banner")).toBeNull();
    // Stale draft should also be cleared.
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull();
  });

  it("successful save clears the draft from localStorage", async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("edited body", "msg", tenMinAfter);
    vi.spyOn(api, "writeHumanArticle").mockResolvedValue({
      path: PATH,
      commit_sha: "abc1234",
      bytes_written: 5,
    });

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    // Restore so content is non-empty + recognized as a real draft.
    fireEvent.click(await screen.findByTestId("wk-editor-draft-restore"));
    fireEvent.click(screen.getByTestId("wk-editor-save"));
    await waitFor(() =>
      expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull(),
    );
  });

  it("409 conflict keeps the draft in localStorage", async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString();
    setLocalStorageDraft("my body", "my summary", tenMinAfter);
    vi.spyOn(api, "writeHumanArticle").mockResolvedValue({
      conflict: true,
      error: "wiki: article changed since it was opened",
      current_sha: "newsha9",
      current_content: "other body",
    });

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    );
    fireEvent.click(await screen.findByTestId("wk-editor-draft-restore"));
    fireEvent.click(screen.getByTestId("wk-editor-save"));
    await screen.findByText(/Someone else edited this article/);
    // The draft must survive so the user's work isn't lost.
    const raw = window.localStorage.getItem(DRAFT_KEY);
    expect(raw).not.toBeNull();
    expect(JSON.parse(raw as string).content).toBe("my body");
  });
});

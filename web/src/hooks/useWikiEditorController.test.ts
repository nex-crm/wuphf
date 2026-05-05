/**
 * Tests for useWikiEditorController — the editor state machine extracted
 * from WikiEditor so the upcoming rich editor can share it.
 *
 * The matching component-level tests in WikiEditor.test.tsx exercise the
 * full UI; these tests pin the underlying state contract directly so the
 * hook can be reused without retrofitting a textarea.
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { WriteHumanResult } from "../api/wiki";
import {
  AUTOSAVE_DEBOUNCE_MS,
  draftKey,
  readDraft,
  useWikiEditorController,
} from "./useWikiEditorController";

const PATH = "team/people/sam.md";
const INITIAL = "# Sam\n\nOriginal body.\n";
const SHA = "abc123";
const SERVER_TS = "2026-05-01T00:00:00Z";

function makeOk(commitSha = "newsha"): WriteHumanResult {
  return { path: PATH, commit_sha: commitSha, bytes_written: 99 };
}

function makeConflict(): WriteHumanResult {
  return {
    conflict: true,
    error: "expected_sha mismatch",
    current_sha: "remotesha",
    current_content: "# Sam\n\nRemote wins.\n",
  };
}

beforeEach(() => {
  window.localStorage.clear();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  window.localStorage.clear();
});

describe("useWikiEditorController — initial state", () => {
  it("seeds content from initialContent and starts with no draft/error/conflict", () => {
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    expect(result.current.content).toBe(INITIAL);
    expect(result.current.commitMessage).toBe("");
    expect(result.current.draft).toBeNull();
    expect(result.current.error).toBeNull();
    expect(result.current.conflict).toBeNull();
    expect(result.current.saving).toBe(false);
  });
});

describe("useWikiEditorController — draft restore/discard", () => {
  it("surfaces a draft banner when localStorage is newer than server and content diverges", () => {
    window.localStorage.setItem(
      draftKey(PATH),
      JSON.stringify({
        content: "# Sam\n\nDraft body.\n",
        summary: "wip",
        saved_at: "2026-05-02T00:00:00Z",
      }),
    );

    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        serverLastEditedTs: SERVER_TS,
        onSaved: vi.fn(),
      }),
    );

    expect(result.current.draft?.content).toBe("# Sam\n\nDraft body.\n");
    expect(result.current.draft?.summary).toBe("wip");
  });

  it("does NOT surface a draft when the server is newer than the cached draft", () => {
    window.localStorage.setItem(
      draftKey(PATH),
      JSON.stringify({
        content: "# Sam\n\nDraft body.\n",
        summary: "wip",
        saved_at: "2026-04-01T00:00:00Z",
      }),
    );

    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        serverLastEditedTs: SERVER_TS,
        onSaved: vi.fn(),
      }),
    );

    expect(result.current.draft).toBeNull();
    // Stale draft is also evicted from storage.
    expect(window.localStorage.getItem(draftKey(PATH))).toBeNull();
  });

  it("does NOT surface a draft when cached content matches server content", () => {
    window.localStorage.setItem(
      draftKey(PATH),
      JSON.stringify({
        content: INITIAL,
        summary: "",
        saved_at: "2026-05-02T00:00:00Z",
      }),
    );

    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        serverLastEditedTs: SERVER_TS,
        onSaved: vi.fn(),
      }),
    );

    expect(result.current.draft).toBeNull();
  });

  it("restores draft content and summary into the editor state", () => {
    window.localStorage.setItem(
      draftKey(PATH),
      JSON.stringify({
        content: "# Sam\n\nDraft body.\n",
        summary: "wip summary",
        saved_at: "2026-05-02T00:00:00Z",
      }),
    );

    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        serverLastEditedTs: SERVER_TS,
        onSaved: vi.fn(),
      }),
    );

    act(() => result.current.handleRestoreDraft());

    expect(result.current.content).toBe("# Sam\n\nDraft body.\n");
    expect(result.current.commitMessage).toBe("wip summary");
    expect(result.current.draft).toBeNull();
  });

  it("clears the cached draft on discard", () => {
    window.localStorage.setItem(
      draftKey(PATH),
      JSON.stringify({
        content: "# Sam\n\nDraft body.\n",
        summary: "wip",
        saved_at: "2026-05-02T00:00:00Z",
      }),
    );

    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        serverLastEditedTs: SERVER_TS,
        onSaved: vi.fn(),
      }),
    );

    act(() => result.current.handleDiscardDraft());

    expect(result.current.draft).toBeNull();
    expect(window.localStorage.getItem(draftKey(PATH))).toBeNull();
  });
});

describe("useWikiEditorController — autosave debounce", () => {
  it("writes to localStorage after AUTOSAVE_DEBOUNCE_MS of quiescence", () => {
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    act(() => result.current.setContent("# Sam\n\nUpdated.\n"));

    // Just before the debounce fires — nothing written yet.
    act(() => {
      vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS - 50);
    });
    expect(readDraft(PATH)).toBeNull();

    act(() => {
      vi.advanceTimersByTime(60);
    });

    const stored = readDraft(PATH);
    expect(stored?.content).toBe("# Sam\n\nUpdated.\n");
  });

  it("does NOT autosave when content equals initial and commit message is empty", () => {
    renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    act(() => {
      vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS * 2);
    });

    expect(readDraft(PATH)).toBeNull();
  });

  it("autosaves a non-empty commit message even when content is unchanged", () => {
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    act(() => result.current.setCommitMessage("typo fix"));
    act(() => {
      vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 10);
    });

    expect(readDraft(PATH)?.summary).toBe("typo fix");
  });
});

describe("useWikiEditorController — save", () => {
  it("posts via writeHumanArticle and calls onSaved with new sha on success", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn().mockResolvedValue(makeOk("freshSha"));
    const onSaved = vi.fn();
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved,
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nNew body.\n"));
    act(() => result.current.setCommitMessage("rewrite intro"));

    await act(async () => {
      await result.current.handleSave();
    });

    expect(writeHumanArticle).toHaveBeenCalledWith({
      path: PATH,
      content: "# Sam\n\nNew body.\n",
      commitMessage: "rewrite intro",
      expectedSha: SHA,
    });
    expect(onSaved).toHaveBeenCalledWith("freshSha");
    expect(result.current.error).toBeNull();
    expect(result.current.conflict).toBeNull();
    // Draft is cleared on successful save.
    expect(window.localStorage.getItem(draftKey(PATH))).toBeNull();
  });

  it("falls back to a default commit message when the user leaves it blank", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn().mockResolvedValue(makeOk());
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nNew body.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(writeHumanArticle).toHaveBeenCalledWith(
      expect.objectContaining({ commitMessage: `human: update ${PATH}` }),
    );
  });

  it("blocks save with an error when content is empty", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn();
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("   \n\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(writeHumanArticle).not.toHaveBeenCalled();
    expect(result.current.error).toMatch(/cannot be empty/i);
  });

  it("captures conflict payload and preserves the user's draft", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn().mockResolvedValue(makeConflict());
    const onSaved = vi.fn();
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved,
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nLocal edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(result.current.conflict).toMatchObject({
      conflict: true,
      current_sha: "remotesha",
    });
    expect(onSaved).not.toHaveBeenCalled();
    // Local content kept so the user can re-apply after reload.
    expect(result.current.content).toBe("# Sam\n\nLocal edit.\n");
  });

  it("surfaces an error when writeHumanArticle throws", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi
      .fn()
      .mockRejectedValue(new Error("network down"));
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nEdited.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(result.current.error).toBe("network down");
    expect(result.current.saving).toBe(false);
  });

  it("ignores re-entrant save calls while a save is in flight", async () => {
    vi.useRealTimers();
    let resolveCall: (value: WriteHumanResult) => void = () => undefined;
    const writeHumanArticle = vi.fn(
      () =>
        new Promise<WriteHumanResult>((resolve) => {
          resolveCall = resolve;
        }),
    );
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nEdited.\n"));

    let firstSave: Promise<void> | undefined;
    act(() => {
      firstSave = result.current.handleSave();
    });
    await waitFor(() => expect(result.current.saving).toBe(true));

    // Second call while in-flight is a no-op.
    await act(async () => {
      await result.current.handleSave();
    });
    expect(writeHumanArticle).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveCall(makeOk());
      await firstSave;
    });
    expect(result.current.saving).toBe(false);
  });
});

describe("useWikiEditorController — conflict reload", () => {
  it("replaces content with server bytes and promotes current_sha via onSaved", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn().mockResolvedValue(makeConflict());
    const onSaved = vi.fn();
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved,
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nLocal edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });
    expect(result.current.conflict).not.toBeNull();

    act(() => result.current.handleReloadConflict());

    expect(result.current.content).toBe("# Sam\n\nRemote wins.\n");
    expect(onSaved).toHaveBeenCalledWith("remotesha");
  });

  it("persists the in-memory edit synchronously when a 409 fires before the autosave debounce", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi.fn().mockResolvedValue(makeConflict());
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    // Edit and save before AUTOSAVE_DEBOUNCE_MS would have fired. Without
    // the synchronous persist on conflict, the user's edit is unrecoverable
    // once `handleReloadConflict` overwrites `content`.
    act(() => result.current.setContent("# Sam\n\nUnsaved local edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    const persisted = readDraft(PATH);
    expect(persisted?.content).toBe("# Sam\n\nUnsaved local edit.\n");

    act(() => result.current.handleReloadConflict());

    // After reload, the draft is still in storage so the user can restore it.
    expect(readDraft(PATH)?.content).toBe("# Sam\n\nUnsaved local edit.\n");
  });

  it("uses the promoted SHA on a follow-up save after reload", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi
      .fn()
      .mockResolvedValueOnce(makeConflict())
      .mockResolvedValueOnce(makeOk("postreloadsha"));
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nLocal edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(writeHumanArticle).toHaveBeenLastCalledWith(
      expect.objectContaining({ expectedSha: SHA }),
    );

    act(() => result.current.handleReloadConflict());

    // Edit again so the second save is non-empty and hits the network mock.
    act(() => result.current.setContent("# Sam\n\nFresh edit on top.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    // The follow-up save must use the SHA promoted by reload, not the stale
    // prop value, otherwise the parent's slow refetch causes a deterministic
    // re-conflict.
    expect(writeHumanArticle).toHaveBeenLastCalledWith(
      expect.objectContaining({ expectedSha: "remotesha" }),
    );
  });

  it("promotes the new SHA locally after a successful save so a follow-up save uses it", async () => {
    vi.useRealTimers();
    const writeHumanArticle = vi
      .fn()
      .mockResolvedValueOnce(makeOk("firstsha"))
      .mockResolvedValueOnce(makeOk("secondsha"));
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
        writeHumanArticle,
      }),
    );

    act(() => result.current.setContent("# Sam\n\nFirst edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    act(() => result.current.setContent("# Sam\n\nSecond edit.\n"));
    await act(async () => {
      await result.current.handleSave();
    });

    expect(writeHumanArticle).toHaveBeenLastCalledWith(
      expect.objectContaining({ expectedSha: "firstsha" }),
    );
  });
});

describe("useWikiEditorController — preview pane visibility", () => {
  it("defaults to source-only", () => {
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    expect(result.current.previewOn).toBe(false);
    expect(result.current.showSource).toBe(true);
    expect(result.current.showPreview).toBe(false);
  });

  it("shows both source and preview on desktop when preview is enabled", () => {
    const { result } = renderHook(() =>
      useWikiEditorController({
        path: PATH,
        initialContent: INITIAL,
        expectedSha: SHA,
        onSaved: vi.fn(),
      }),
    );

    act(() => result.current.setPreviewOn(true));

    // jsdom default viewport is 1024x768 — not mobile.
    expect(result.current.isMobile).toBe(false);
    expect(result.current.showSource).toBe(true);
    expect(result.current.showPreview).toBe(true);
  });
});

describe("useWikiEditorController — path change", () => {
  it("resets editor state when path changes", () => {
    const { result, rerender } = renderHook(
      (props: { path: string; initialContent: string }) =>
        useWikiEditorController({
          path: props.path,
          initialContent: props.initialContent,
          expectedSha: SHA,
          onSaved: vi.fn(),
        }),
      { initialProps: { path: PATH, initialContent: INITIAL } },
    );

    act(() => result.current.setContent("# Sam\n\nEdited.\n"));
    act(() => result.current.setCommitMessage("typo"));

    rerender({
      path: "team/people/other.md",
      initialContent: "# Other\n",
    });

    expect(result.current.content).toBe("# Other\n");
    expect(result.current.commitMessage).toBe("");
    expect(result.current.error).toBeNull();
    expect(result.current.conflict).toBeNull();
  });
});

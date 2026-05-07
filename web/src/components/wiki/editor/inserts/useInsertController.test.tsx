/**
 * Integration tests for the insert controller.
 *
 * Drives the controller through realistic flows (slash trigger -> action ->
 * dialog -> insert) without spinning up the full Milkdown editor. A fake
 * `EditorView` records the transactions the controller would dispatch so
 * we can assert on the final markdown without ProseMirror's overhead.
 */
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useInsertController } from "./useInsertController";

type FakeView = {
  dispatch: ReturnType<typeof vi.fn>;
  state: {
    tr: {
      delete: ReturnType<typeof vi.fn>;
      insertText: ReturnType<typeof vi.fn>;
    };
    selection: { from: number };
  };
  focus: ReturnType<typeof vi.fn>;
};

function makeFakeView(): FakeView {
  // The controller's `replaceRange` / `insertAtSelection` chain through
  // `state.tr.delete().insertText()`. We mirror that fluent shape so the
  // calls don't blow up; we don't model real document state.
  const tr = {
    delete: vi.fn((_from: number, _to: number) => tr),
    insertText: vi.fn((_text: string, _from?: number) => tr),
  };
  return {
    dispatch: vi.fn(),
    state: { tr, selection: { from: 0 } },
    focus: vi.fn(),
  };
}

describe("useInsertController", () => {
  it("opens the citation dialog when the citation slash action is selected", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "Body of article.\n",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "/",
        from: 0,
        to: 4,
        query: "cite",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onSlashSelect("citation");
    });
    expect(result.current.dialog).toBe("citation");
  });

  it("appends a citation definition to the document tail on confirm", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "Body of article.\n",
      }),
    );

    act(() => {
      result.current.onCitationConfirm({
        reference: "[^1]",
        definition: "[^1]: Source - https://example.com\n",
      });
    });
    // The reference should have been inserted at the caret.
    expect(view.state.tr.insertText).toHaveBeenCalledWith("[^1]", 0);
    // And the definition appended via pushContent.
    const next = pushContent.mock.calls[0][0] as string;
    expect(next).toContain("[^1]: Source - https://example.com");
  });

  it("inserts a fact block on confirm without auto-writing", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "x",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "/",
        from: 0,
        to: 5,
        query: "fact",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onSlashSelect("fact");
    });
    expect(result.current.dialog).toBe("fact");
    // No write yet — the dialog is open but nothing is inserted.
    expect(view.state.tr.insertText).not.toHaveBeenCalled();
    expect(pushContent).not.toHaveBeenCalled();
    // Confirm. Block inserts go through pushContent (re-parsed at the
    // document level) so a fenced block round-trips as a real block
    // node — `tr.insertText` would keep it as inline text inside the
    // current paragraph and break serialization.
    act(() => {
      result.current.onFactConfirm(
        "```fact\nsubject: a\npredicate: b\nobject: c\n```\n",
      );
    });
    expect(view.state.tr.insertText).not.toHaveBeenCalled();
    expect(pushContent).toHaveBeenCalled();
    const next = pushContent.mock.calls[0][0] as string;
    expect(next).toContain("```fact");
    expect(next).toContain("subject: a");
  });

  it("inserts a wikilink at the trigger range when the @ picker is used", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "x",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "@",
        from: 5,
        to: 8,
        query: "ale",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onMentionSelect({
        slug: "team/people/alex",
        title: "Alex Chen",
        category: "people",
      });
    });
    // Replace the trigger range with the wikilink.
    expect(view.state.tr.delete).toHaveBeenCalledWith(5, 8);
    expect(view.state.tr.insertText).toHaveBeenCalledWith(
      "[[team/people/alex|Alex Chen]]",
      5,
    );
  });

  it("opens the mention picker dialog for the wiki-link slash action", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "x",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "/",
        from: 0,
        to: 5,
        query: "link",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onSlashSelect("wiki-link");
    });
    expect(result.current.dialog).toBe("mention-picker");
    expect(result.current.mentionPickerState?.categoryFilter).toBeNull();
    expect(result.current.mentionPickerState?.heading).toBe("Link wiki page");
  });

  it("filters the picker to tasks for the task-ref action", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "x",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "/",
        from: 0,
        to: 5,
        query: "task",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onSlashSelect("task-ref");
    });
    expect(result.current.mentionPickerState?.categoryFilter).toBe("tasks");
  });

  it("filters the picker to agents for the agent-mention action", () => {
    const view = makeFakeView();
    const pushContent = vi.fn();
    const { result } = renderHook(() =>
      useInsertController({
        getView: () =>
          view as unknown as ReturnType<
            NonNullable<Parameters<typeof useInsertController>[0]["getView"]>
          >,
        pushContent,
        getCurrentContent: () => "x",
      }),
    );

    act(() => {
      result.current.setTrigger({
        trigger: "/",
        from: 0,
        to: 5,
        query: "agent",
        rect: { top: 0, left: 0, bottom: 0, right: 0 },
      });
    });
    act(() => {
      result.current.onSlashSelect("agent-mention");
    });
    expect(result.current.mentionPickerState?.categoryFilter).toBe("agents");
  });
});

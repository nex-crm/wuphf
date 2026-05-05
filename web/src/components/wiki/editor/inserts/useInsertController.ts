/**
 * Coordinates the WUPHF-specific inserts on top of the rich editor.
 *
 * Responsibilities:
 *   - Track the active trigger (`/` slash menu, `@` mention picker) emitted
 *     by `triggerPlugin`.
 *   - Track which dialog (if any) should be open after the user picks a
 *     slash action.
 *   - On commit, delete the trigger range from ProseMirror and either:
 *       a) insert markdown at the caret, or
 *       b) open the appropriate dialog (which inserts on confirm).
 *   - For citations, also append the footnote definition to the document
 *     tail through the controller's `setContent`. This is the only path
 *     that writes outside the caret position; everything else is local.
 *
 * The controller stays UI-framework-agnostic — it accepts the editor
 * view ref + the controller-level setContent + the catalog and exposes
 * pure handlers consumed by `RichWikiEditor`. No DOM access lives here.
 */

import { useCallback, useState } from "react";
import type { EditorView } from "@milkdown/prose/view";

import {
  appendCitationDefinition,
  type BuiltCitation,
  buildWikilink,
} from "./markdownShapes";
import type { MentionItem } from "./mentionCatalog";
import {
  insertAtSelection,
  replaceRange,
  type TriggerState,
} from "./triggerPlugin";
import type { SlashAction } from "./types";

export type DialogKind =
  | "citation"
  | "fact"
  | "decision"
  | "related"
  | "mention-picker";

export interface MentionPickerState {
  /** Restrict the picker to one category, or null for "any wiki page". */
  categoryFilter: import("./mentionCatalog").MentionCategory | null;
  /** Heading rendered at the top of the picker. */
  heading: string;
}

export interface UseInsertControllerArgs {
  /** Live ProseMirror view from the editor — null until the editor mounts. */
  getView: () => EditorView | null;
  /** Controller-level setter so we can append citation definitions to the
   *  document tail without going through ProseMirror. */
  pushContent: (next: string) => void;
  /** The latest content held by the controller. */
  currentContent: string;
}

export interface InsertController {
  trigger: TriggerState | null;
  setTrigger: (next: TriggerState | null) => void;
  /** Open dialog kind, or null when no dialog is active. */
  dialog: DialogKind | null;
  /** Mention picker state (heading + category filter) when active. */
  mentionPickerState: MentionPickerState | null;
  /** Open a dialog without going through the slash menu — used by the
   *  controller itself once a slash action commits and the trigger range
   *  has been deleted. Exposed for tests. */
  openDialog: (kind: DialogKind, mention?: MentionPickerState) => void;
  closeDialog: () => void;
  /** Slash menu callbacks. */
  onSlashSelect: (action: SlashAction) => void;
  onMentionSelect: (item: MentionItem) => void;
  closeTrigger: () => void;
  /** Dialog confirm callbacks — forwarded by RichWikiEditor to the dialog
   *  components. They remove the dialog and insert the produced markdown. */
  onCitationConfirm: (built: BuiltCitation) => void;
  onFactConfirm: (block: string) => void;
  onDecisionConfirm: (block: string) => void;
  onRelatedConfirm: (block: string) => void;
}

export function useInsertController(
  args: UseInsertControllerArgs,
): InsertController {
  const [trigger, setTriggerState] = useState<TriggerState | null>(null);
  const [dialog, setDialog] = useState<DialogKind | null>(null);
  const [mentionPickerState, setMentionPickerState] =
    useState<MentionPickerState | null>(null);

  const setTrigger = useCallback((next: TriggerState | null) => {
    setTriggerState(next);
  }, []);

  const closeTrigger = useCallback(() => {
    setTriggerState(null);
  }, []);

  const openDialog = useCallback(
    (kind: DialogKind, mention?: MentionPickerState) => {
      setDialog(kind);
      setMentionPickerState(mention ?? null);
    },
    [],
  );

  const closeDialog = useCallback(() => {
    setDialog(null);
    setMentionPickerState(null);
  }, []);

  /**
   * Delete the trigger range (e.g. `/cit`) so the action does not leave
   * stray characters behind, then either insert markdown at the caret or
   * open the appropriate dialog.
   */
  const consumeTrigger = useCallback(
    (active: TriggerState, replacement: string) => {
      const view = args.getView();
      if (!view) return;
      replaceRange(view, active.from, active.to, replacement);
      setTriggerState(null);
    },
    [args],
  );

  const onSlashSelect = useCallback(
    (action: SlashAction) => {
      const active = trigger;
      if (!active) return;
      // For inserts that need a dialog, delete the trigger range first
      // (no replacement) and then open the dialog. The user's caret is
      // left where the trigger used to be, so the dialog's confirm
      // inserts at exactly the right spot.
      const openWithEmptyConsume = (
        kind: DialogKind,
        mention?: MentionPickerState,
      ) => {
        consumeTrigger(active, "");
        openDialog(kind, mention);
      };
      switch (action) {
        case "wiki-link":
          openWithEmptyConsume("mention-picker", {
            categoryFilter: null,
            heading: "Link wiki page",
          });
          break;
        case "task-ref":
          openWithEmptyConsume("mention-picker", {
            categoryFilter: "tasks",
            heading: "Insert task reference",
          });
          break;
        case "agent-mention":
          openWithEmptyConsume("mention-picker", {
            categoryFilter: "agents",
            heading: "Insert agent mention",
          });
          break;
        case "citation":
          openWithEmptyConsume("citation");
          break;
        case "fact":
          openWithEmptyConsume("fact");
          break;
        case "decision":
          openWithEmptyConsume("decision");
          break;
        case "related":
          openWithEmptyConsume("related");
          break;
      }
    },
    [trigger, consumeTrigger, openDialog],
  );

  const onMentionSelect = useCallback(
    (item: MentionItem) => {
      const view = args.getView();
      if (!view) return;
      const link = buildWikilink(item.slug, item.title);
      if (!link) return;
      // Two paths:
      //   1. Triggered from `@` — replace the trigger range with the link.
      //   2. Triggered from a slash action via mention-picker dialog —
      //      the trigger range was already deleted; insert at caret and
      //      close the dialog.
      if (trigger && dialog === null) {
        replaceRange(view, trigger.from, trigger.to, link);
        setTriggerState(null);
        return;
      }
      insertAtSelection(view, link);
      closeDialog();
    },
    [args, trigger, dialog, closeDialog],
  );

  const onCitationConfirm = useCallback(
    (built: BuiltCitation) => {
      const view = args.getView();
      if (!view) {
        closeDialog();
        return;
      }
      // Inline reference goes at the caret (where the trigger lived).
      insertAtSelection(view, built.reference);
      closeDialog();
      // Append the footnote definition through the controller's
      // setContent so it sits at the document tail. We read the
      // most-recent canonical content the editor has emitted so the
      // append doesn't race with an in-flight markdownUpdated tick.
      // ProseMirror's transaction has already settled by the time the
      // callback fires, so `args.currentContent` reflects the
      // post-insert state when the controller is wired correctly.
      const next = appendCitationDefinition(
        args.currentContent,
        built.definition,
      );
      args.pushContent(next);
    },
    [args, closeDialog],
  );

  const insertBlock = useCallback(
    (block: string) => {
      const view = args.getView();
      if (!view) {
        closeDialog();
        return;
      }
      // Block inserts (fact / decision / related) need to start on a new
      // line so the surrounding paragraph doesn't absorb the fence.
      insertAtSelection(view, `\n${block}`);
      closeDialog();
    },
    [args, closeDialog],
  );

  const onFactConfirm = useCallback(
    (block: string) => insertBlock(block),
    [insertBlock],
  );
  const onDecisionConfirm = useCallback(
    (block: string) => insertBlock(block),
    [insertBlock],
  );
  const onRelatedConfirm = useCallback(
    (block: string) => insertBlock(block),
    [insertBlock],
  );

  return {
    trigger,
    setTrigger,
    dialog,
    mentionPickerState,
    openDialog,
    closeDialog,
    onSlashSelect,
    onMentionSelect,
    closeTrigger,
    onCitationConfirm,
    onFactConfirm,
    onDecisionConfirm,
    onRelatedConfirm,
  };
}

/**
 * RestoreToast — 30-second undo toast that appears after a successful
 * non-permanent shred. Click "Undo" to POST /workspaces/restore with the
 * just-emitted trash_id; on success, navigate to the restored workspace
 * URL via a full page reload.
 *
 * The toast itself is rendered inline by the rail's shred handler
 * through the existing global Toast/showUndoToast plumbing, so this
 * module exposes a single helper rather than a JSX component. Keeping
 * the surface a function (not a component) lets the rail call it from
 * inside an async mutation `onSuccess` without any extra state plumbing.
 */
import { useRestoreWorkspace } from "../../api/workspaces";
import { showNotice, showUndoToast } from "../ui/Toast";

const RESTORE_TOAST_MS = 30_000;

interface ShowRestoreToastInput {
  /** The shredded workspace's display name. */
  name: string;
  /** Trash ID returned by /workspaces/shred. */
  trashId: string;
  /** Triggered on Undo click — caller supplies the actual restore call. */
  onUndo: () => void | Promise<void>;
}

/**
 * Imperative entry point — pulled out so the rail can call it from any
 * mutation handler without rendering a fresh React tree just to fire one
 * toast. Mirrors the existing `showNotice` / `showUndoToast` API.
 */
export function showRestoreToast({
  name,
  trashId: _trashId,
  onUndo,
}: ShowRestoreToastInput) {
  showUndoToast(
    `Workspace '${name}' shredded. Undo?`,
    () => {
      void onUndo();
    },
    RESTORE_TOAST_MS,
  );
}

interface UseRestoreToastReturn {
  /** Fires the undo toast and wires the click into the restore mutation. */
  fire: (name: string, trashId: string) => void;
}

/**
 * Hook variant — returns a stable callback that fires the toast and
 * binds Undo to the restore mutation. The component layer (rail) uses
 * this to keep the toast's network behavior co-located with the React
 * Query cache invalidation it triggers.
 */
export function useRestoreToast(): UseRestoreToastReturn {
  const restore = useRestoreWorkspace({
    onSuccess: (data) => {
      // Page reload to the restored workspace's web URL — same protocol
      // as a normal switch. Falls back to a notice if the backend didn't
      // include a URL (e.g., restore returned ok but the broker isn't
      // up yet).
      if (data.url) {
        window.location.assign(data.url);
      } else {
        showNotice(`Workspace '${data.workspace.name}' restored.`, "success");
      }
    },
    onError: (err) => {
      showNotice(
        err instanceof Error
          ? `Restore failed: ${err.message}`
          : "Restore failed.",
        "error",
      );
    },
  });

  return {
    fire: (name, trashId) => {
      showRestoreToast({
        name,
        trashId,
        onUndo: () => restore.mutate({ trash_id: trashId }),
      });
    },
  };
}

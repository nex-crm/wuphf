import { type FormEvent, useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { type AppBuildRequest, requestAppBuild } from "../../api/apps";
import { router } from "../../lib/router";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";

const TITLE_ID = "app-builder-modal-title";

/**
 * CreateAppDialog is the human-initiated entry point for the App Builder, opened
 * by /create-app, /update-app, and the Edit button on an app screen. Submitting
 * kicks off an App Builder task directly — these paths are pre-authorized by the
 * human, so they skip the propose_app approval gate.
 *
 * Mirrors the app's canonical modal frame (HelpModal / VersionModal): a
 * .help-overlay + .help-modal card with a .help-header and Esc affordance,
 * native .input / .btn controls. NOT the Radix/Tailwind dialog, which read as
 * off-system next to every other modal.
 */
export function CreateAppDialog() {
  const dialog = useAppStore((s) => s.appBuilderDialog);
  const close = useAppStore((s) => s.closeAppBuilderDialog);
  const noteAppBuilding = useAppStore((s) => s.noteAppBuilding);

  const isUpdate = dialog?.mode === "update";
  const open = dialog !== null;
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const nameRef = useRef<HTMLInputElement>(null);
  const descRef = useRef<HTMLTextAreaElement>(null);

  // Reset fields whenever a fresh dialog opens (name is prefilled + locked in
  // update mode; the human types only the change).
  useEffect(() => {
    if (dialog) {
      setName(dialog.name ?? "");
      setDescription("");
    }
  }, [dialog]);

  // Esc closes — claim it in the capture phase so we beat the global shortcut
  // handler, same as VersionModal.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent): void {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopImmediatePropagation();
        close();
      }
    }
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [open, close]);

  // Focus the first field on open; restore prior focus on close.
  useEffect(() => {
    if (!open) return;
    const prevFocus = document.activeElement as HTMLElement | null;
    const id = window.requestAnimationFrame(() => {
      (isUpdate ? descRef.current : nameRef.current)?.focus();
    });
    return () => {
      window.cancelAnimationFrame(id);
      if (prevFocus?.isConnected && typeof prevFocus.focus === "function") {
        prevFocus.focus();
      }
    };
  }, [open, isUpdate]);

  const mutation = useMutation({
    mutationFn: (req: AppBuildRequest) => requestAppBuild(req),
    onSuccess: (task, req) => {
      if (!req.appId) {
        noteAppBuilding(req.name);
      }
      close();
      // Open the task chat so the human lands with the App Builder agent and
      // watches the build happen live (the task carries a live preview pane),
      // rather than being left with a fire-and-forget toast.
      if (task?.id) {
        void router.navigate({
          to: "/tasks/$taskId",
          params: { taskId: task.id },
        });
      } else {
        showNotice(
          isUpdate
            ? "App Builder is on it — your change will land shortly."
            : "App Builder is building your app — it'll appear under Apps soon.",
          "success",
        );
      }
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Could not start the App Builder.",
        "error",
      );
    },
  });

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const trimmedDescription = description.trim();
    if (!trimmedDescription) return;
    if (isUpdate) {
      mutation.mutate({
        name: name.trim() || "app",
        description: trimmedDescription,
        appId: dialog?.appId,
      });
      return;
    }
    const trimmedName = name.trim();
    if (!trimmedName) return;
    mutation.mutate({ name: trimmedName, description: trimmedDescription });
  }

  if (!open) return null;

  const canSubmit =
    description.trim().length > 0 && (isUpdate || name.trim().length > 0);

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss matches HelpModal/VersionModal; the keyboard path is the document-level Escape handler above.
    <div className="help-overlay">
      <button
        type="button"
        className="help-backdrop"
        onClick={() => close()}
        tabIndex={-1}
        aria-label="Close create app dialog"
      />
      <div
        className="help-modal app-builder-modal card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={TITLE_ID}
      >
        <header className="help-header">
          <div>
            <h2 id={TITLE_ID} className="help-title">
              {isUpdate ? `Improve ${dialog?.name ?? "app"}` : "Create an app"}
            </h2>
            <p className="help-subtitle">
              {isUpdate
                ? "Describe the change. The App Builder updates this app in place."
                : "Describe the internal tool you want. The App Builder designs, builds, and publishes it under Apps."}
            </p>
          </div>
          <button
            type="button"
            className="help-close"
            onClick={() => close()}
            aria-label="Close"
          >
            Esc
          </button>
        </header>

        <form className="help-body app-builder-modal__body" onSubmit={onSubmit}>
          {!isUpdate && (
            <label className="app-builder-modal__field">
              <span className="app-builder-modal__label">Name</span>
              <input
                ref={nameRef}
                className="input"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Standup Digest"
              />
            </label>
          )}
          <label className="app-builder-modal__field">
            <span className="app-builder-modal__label">
              {isUpdate ? "What should change?" : "What should it do?"}
            </span>
            <textarea
              ref={descRef}
              className="app-builder-modal__textarea"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={5}
              placeholder={
                isUpdate
                  ? "e.g. add a CSV export button to the table"
                  : "e.g. a daily digest listing each agent's open tasks grouped by status"
              }
            />
          </label>

          <div className="app-builder-modal__footer">
            <button
              type="button"
              className="btn btn-ghost"
              onClick={() => close()}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={!canSubmit || mutation.isPending}
            >
              {mutation.isPending
                ? "Starting…"
                : isUpdate
                  ? "Request change"
                  : "Build app"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

/**
 * Focus trap for the insert dialogs (citation / fact / decision / related).
 *
 * Each dialog is `role="dialog" aria-modal="true"` but, without a trap, Tab can
 * walk focus out into the document behind the backdrop while the modal is still
 * up. This hook implements the modal-dialog focus contract:
 *
 *   - on open: remember the element that had focus, then move focus to the
 *     first focusable control inside the dialog (unless focus is already
 *     inside, so a dialog that auto-focuses a specific field keeps it);
 *   - while open: Tab / Shift+Tab cycle within the dialog's focusable set
 *     instead of escaping to the page;
 *   - on close: restore focus to the element that opened the dialog.
 *
 * Attach the returned ref to the dialog's container (the backdrop element that
 * wraps every focusable control). The hook owns its own keydown listener on
 * that node, so it composes with the dialog's existing Escape handler.
 */

import { useEffect, useRef } from "react";

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

function focusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
  ).filter((el) => el.offsetParent !== null || el === document.activeElement);
}

/**
 * Wrap focus to the opposite edge when Tab / Shift+Tab would leave the trap.
 * Returns true when it handled (and the caller should `preventDefault`).
 */
function cycleTab(container: HTMLElement, shiftKey: boolean): boolean {
  const focusables = focusableElements(container);
  if (focusables.length === 0) return true;
  const first = focusables[0];
  const last = focusables[focusables.length - 1];
  const active = document.activeElement;
  const atEdge = shiftKey
    ? active === first || !container.contains(active)
    : active === last || !container.contains(active);
  if (!atEdge) return false;
  (shiftKey ? last : first).focus();
  return true;
}

export function useFocusTrap<
  T extends HTMLElement,
>(): React.RefObject<T | null> {
  const containerRef = useRef<T | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;

    // Only steal focus if it is not already inside — dialogs that auto-focus a
    // chosen field (e.g. the URL input) keep that field focused.
    if (!container.contains(document.activeElement)) {
      const focusables = focusableElements(container);
      focusables[0]?.focus();
    }

    const handleKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Tab") return;
      if (cycleTab(container, event.shiftKey)) {
        event.preventDefault();
      }
    };

    container.addEventListener("keydown", handleKeyDown);
    return () => {
      container.removeEventListener("keydown", handleKeyDown);
      previouslyFocused?.focus();
    };
  }, []);

  return containerRef;
}

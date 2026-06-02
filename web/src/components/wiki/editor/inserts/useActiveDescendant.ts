/**
 * Reflects a listbox's active option id onto the element that actually holds
 * keyboard focus, so assistive tech can follow the slash / mention menus.
 *
 * The slash and mention popups keep focus inside the editor's contenteditable
 * (their keyboard nav rides a global keydown listener, not `document.active
 * element`). The WAI-ARIA combobox pattern wants `aria-activedescendant` on the
 * focused element, pointing at the active option's id — that is the editor
 * surface here, not the floating list. This hook writes the attribute on mount
 * / when the active id changes, and clears it on unmount so a closed menu never
 * leaves a dangling reference behind.
 */

import { useEffect } from "react";

export function useActiveDescendant(
  target: HTMLElement | null | undefined,
  activeOptionId: string | null,
): void {
  useEffect(() => {
    if (!target) return;
    if (activeOptionId) {
      target.setAttribute("aria-activedescendant", activeOptionId);
    } else {
      target.removeAttribute("aria-activedescendant");
    }
    return () => {
      target.removeAttribute("aria-activedescendant");
    };
  }, [target, activeOptionId]);
}

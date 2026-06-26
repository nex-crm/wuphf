import { useSyncExternalStore } from "react";

export type Direction = "ltr" | "rtl";

/**
 * Self-contained locale-direction hook for the refclone editor primitives.
 *
 * The reference app's `useLocale` is wired to its i18next stack and a CJK font
 * loader, neither of which exist in WUPHF. The vendored UI primitives only need
 * the reading direction (`dir`) to mirror direction-sensitive icons, so this
 * adapter derives `dir` from the live `<html dir>` attribute and re-renders on
 * change instead of dragging in the host app's i18n graph.
 */

function readDir(): Direction {
  if (typeof document === "undefined") return "ltr";
  return document.documentElement.dir === "rtl" ? "rtl" : "ltr";
}

function subscribe(callback: () => void) {
  if (
    typeof document === "undefined" ||
    typeof MutationObserver === "undefined"
  ) {
    return () => {};
  }
  const observer = new MutationObserver(callback);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["dir"],
  });
  return () => observer.disconnect();
}

function readServerDir(): Direction {
  return "ltr";
}

/**
 * Humanize an i18n key into a fallback label.
 *
 * The reference editor's `useLocale` returns a `t(key)` translator wired to
 * i18next with bundled `editor:`, `editorExtras:`, `linkPopover:`, etc.
 * namespaces. WUPHF has no i18n graph, so this derives a readable English
 * label from the key's final segment — e.g. `t("editor:toolbar.bold")` →
 * "Bold", `t("editorExtras:editWithAi")` → "Edit With Ai". This keeps every
 * `t(...)` call site in the ported files byte-for-byte verbatim while still
 * rendering sensible aria-labels, titles, and visible strings.
 */
function humanizeKey(key: string): string {
  const last = key.split(":").pop() ?? key;
  const leaf = last.split(".").pop() ?? last;
  // camelCase → words, then capitalize each word.
  const spaced = leaf
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[._-]+/g, " ")
    .trim();
  if (!spaced) return leaf;
  return spaced.replace(/\b\w/g, (c) => c.toUpperCase());
}

export function useLocale(): { dir: Direction; t: (key: string) => string } {
  const dir = useSyncExternalStore(subscribe, readDir, readServerDir);
  return { dir, t: humanizeKey };
}

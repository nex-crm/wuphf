/**
 * useRecentObjects — persists and surfaces the last N objects the user
 * navigated to. Backed by localStorage so the list survives page reloads.
 *
 * An "object" is an ObjectRef from lib/objectRoutes — typed, not free-form
 * strings. The label comes from resolveObjectRoute so breadcrumb and recent
 * list always agree.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { useCallback } from "react";
import { resolveObjectRoute, type ObjectRef } from "../lib/objectRoutes";

export const RECENT_OBJECTS_MAX = 10;
const STORAGE_KEY = "wuphf-recent-objects";

export interface RecentObjectEntry {
  ref: ObjectRef;
  label: string;
  href: string;
  visitedAtMs: number;
}

function safeRead(): RecentObjectEntry[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item): item is RecentObjectEntry =>
        typeof item === "object" &&
        item !== null &&
        typeof (item as { label?: unknown }).label === "string" &&
        typeof (item as { href?: unknown }).href === "string" &&
        typeof (item as { visitedAtMs?: unknown }).visitedAtMs === "number" &&
        Number.isFinite((item as { visitedAtMs: number }).visitedAtMs) &&
        typeof (item as { ref?: { kind?: unknown } }).ref?.kind === "string",
    );
  } catch {
    return [];
  }
}

function safeWrite(entries: RecentObjectEntry[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(entries));
  } catch {
    // Safari private-browsing and sandboxed-iframe contexts throw.
    // Silently swallow — the in-memory state is still correct for this session.
  }
}

/** Derive a stable dedup key from an ObjectRef. */
function objectKey(ref: ObjectRef): string {
  switch (ref.kind) {
    case "agent":
      return `agent:${ref.slug}`;
    case "run":
      return `run:${ref.id}`;
    case "task":
      return `task:${ref.id}`;
    case "wiki-page":
      return `wiki-page:${ref.path}`;
    case "workbench-item":
      return `workbench-item:${ref.id}`;
    case "artifact":
      return `artifact:${ref.id}`;
    case "settings-section":
      return `settings-section:${ref.section}`;
  }
}

/**
 * Read the current recent-objects list directly from localStorage.
 * Safe to call outside React — used by components that need the list
 * without subscribing to re-renders (e.g. the Sidebar which already
 * renders on route changes anyway).
 */
export function readRecentObjects(): RecentObjectEntry[] {
  return safeRead();
}

/**
 * Push a new visit to the front of the list and persist.
 * Deduplicates by object key, capping at RECENT_OBJECTS_MAX entries.
 * Pure function: returns the new list; side-effect is localStorage write.
 */
export function pushRecentObject(ref: ObjectRef): RecentObjectEntry[] {
  const resolution = resolveObjectRoute(ref);
  if (resolution.fallback) {
    // Don't record fallback objects (unknown kind, missing id).
    return readRecentObjects();
  }
  const key = objectKey(ref);
  const entry: RecentObjectEntry = {
    ref,
    label: resolution.label,
    href: resolution.href,
    visitedAtMs: Date.now(),
  };
  const existing = safeRead().filter((e) => objectKey(e.ref) !== key);
  const next = [entry, ...existing].slice(0, RECENT_OBJECTS_MAX);
  safeWrite(next);
  return next;
}

/**
 * React hook: returns a stable `record` callback that pushes a visit
 * to the recent list. The list itself is read from localStorage on
 * demand; callers that render the list should read it via `readRecentObjects`.
 */
export function useRecordRecentObject() {
  return useCallback((ref: ObjectRef) => {
    pushRecentObject(ref);
  }, []);
}

// Agent display names. Operators can RENAME an agent; overrides live
// client-side (localStorage) as the optimistic cache and apply everywhere a
// name renders (detail header, sidebar rail, lists). For a REAL agent (app_…
// id) the rename is ALSO persisted to the broker fire-and-forget
// (PATCH /apps/{id} {"name": …} → {app}); the local override stays regardless
// of the PATCH result, so renames keep working offline.

import { useSyncExternalStore } from "react";

import { patch } from "../../api/client";
import { isRealAppId } from "../apps/useOperatorApps";

const KEY = "wuphf.operator.agentNames";

type NameMap = Record<string, string>;

function read(): NameMap {
  try {
    const raw = localStorage.getItem(KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : {};
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as NameMap;
    }
  } catch {
    // fall through to empty
  }
  return {};
}

// The in-memory map is the source of truth; localStorage is best-effort
// persistence. A failed setItem loses only the cross-reload copy — the rename
// still applies everywhere in this session.
let cache: NameMap = read();
const listeners = new Set<() => void>();

function notify() {
  for (const l of listeners) l();
}

export function setAgentName(id: string, name: string): void {
  const next = { ...cache };
  const trimmed = name.trim();
  if (trimmed) next[id] = trimmed;
  else delete next[id];
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // storage unavailable — the rename holds in memory, just not across reloads
  }
  cache = next;
  notify();
  // Real agents also persist the rename on the broker. Fire-and-forget: the
  // localStorage override above is the optimistic cache either way. A cleared
  // rename (empty name) only drops the local override — the broker keeps its
  // current name, which is what the fallback then shows.
  if (trimmed && isRealAppId(id)) {
    void patch(`/apps/${encodeURIComponent(id)}`, { name: trimmed }).catch(
      () => {},
    );
  }
}

export function agentName(id: string, fallback: string): string {
  return cache[id] ?? fallback;
}

/** Re-hydrate the in-memory map from storage (tests; a storage-event hook). */
export function reloadAgentNames(): void {
  cache = read();
  notify();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Reactive display name for an agent: the operator's rename, else the given. */
export function useAgentName(id: string, fallback: string): string {
  const map = useSyncExternalStore(
    subscribe,
    () => cache,
    () => cache,
  );
  return map[id] ?? fallback;
}

/** The whole reactive override map (for lists that name many agents). */
export function useAgentNames(): Readonly<Record<string, string>> {
  return useSyncExternalStore(
    subscribe,
    () => cache,
    () => cache,
  );
}

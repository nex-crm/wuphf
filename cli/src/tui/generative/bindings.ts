/**
 * RFC 6901 JSON Pointer implementation for A2UI data binding.
 * Provides resolve/set/apply operations over a flat data model.
 */

import type { A2UIDataModel, A2UIUpdate } from './types.js';

/**
 * Parse a JSON Pointer string into path segments.
 * Handles RFC 6901 escaping: ~1 -> /, ~0 -> ~
 */
function parseSegments(pointer: string): string[] {
  if (pointer === '' || pointer === '/') return [];
  // Strip leading /
  const raw = pointer.startsWith('/') ? pointer.slice(1) : pointer;
  return raw.split('/').map((seg) =>
    seg.replace(/~1/g, '/').replace(/~0/g, '~'),
  );
}

/**
 * Resolve a JSON Pointer against a data object.
 *
 * @example
 * resolvePointer({ user: { name: "Alice" } }, "/user/name") // "Alice"
 * resolvePointer({ items: [{ status: "done" }] }, "/items/0/status") // "done"
 */
export function resolvePointer(data: unknown, pointer: string): unknown {
  const segments = parseSegments(pointer);
  let current: unknown = data;

  for (const seg of segments) {
    if (current === null || current === undefined) return undefined;

    if (Array.isArray(current)) {
      const idx = Number(seg);
      if (Number.isNaN(idx)) return undefined;
      current = current[idx];
    } else if (typeof current === 'object') {
      current = (current as Record<string, unknown>)[seg];
    } else {
      return undefined;
    }
  }

  return current;
}

/**
 * Set a value at a JSON Pointer path, creating intermediate objects as needed.
 * Mutates the data object in place.
 */
export function setPointer(
  data: Record<string, unknown>,
  pointer: string,
  value: unknown,
): void {
  const segments = parseSegments(pointer);
  if (segments.length === 0) return;

  let current: Record<string, unknown> = data;

  for (let i = 0; i < segments.length - 1; i++) {
    const seg = segments[i];
    const nextSeg = segments[i + 1];
    const isNextArray = /^\d+$/.test(nextSeg);

    if (!(seg in current) || current[seg] === null || current[seg] === undefined) {
      current[seg] = isNextArray ? [] : {};
    }

    const next = current[seg];
    if (Array.isArray(next)) {
      const idx = Number(segments[i + 1]);
      // Ensure array is long enough
      while (next.length <= idx) next.push(undefined);
      if (i + 1 === segments.length - 1) {
        next[idx] = value;
        return;
      }
      if (typeof next[idx] !== 'object' || next[idx] === null) {
        next[idx] = {};
      }
      current = next[idx] as Record<string, unknown>;
      i++; // skip the array index segment
    } else if (typeof next === 'object') {
      current = next as Record<string, unknown>;
    } else {
      current[seg] = {};
      current = current[seg] as Record<string, unknown>;
    }
  }

  const lastSeg = segments[segments.length - 1];
  if (Array.isArray(current)) {
    const idx = Number(lastSeg);
    if (!Number.isNaN(idx)) {
      while (current.length <= idx) (current as unknown[]).push(undefined);
      (current as unknown[])[idx] = value;
    }
  } else {
    current[lastSeg] = value;
  }
}

/**
 * Check if a string looks like a JSON Pointer (starts with "/").
 */
export function isPointer(value: string): boolean {
  return value.startsWith('/');
}

/**
 * Apply a batch of updates to a data model.
 * Returns a shallow copy of the model with updates applied.
 */
export function applyUpdates(
  model: A2UIDataModel,
  updates: A2UIUpdate[],
): A2UIDataModel {
  const result: A2UIDataModel = { ...model };

  for (const update of updates) {
    switch (update.type) {
      case 'set':
        setPointer(result, update.path, update.value);
        break;

      case 'merge': {
        const existing = resolvePointer(result, update.path);
        if (
          typeof existing === 'object' &&
          existing !== null &&
          typeof update.value === 'object' &&
          update.value !== null &&
          !Array.isArray(existing)
        ) {
          setPointer(result, update.path, {
            ...(existing as Record<string, unknown>),
            ...(update.value as Record<string, unknown>),
          });
        } else {
          // Fallback to set if not mergeable
          setPointer(result, update.path, update.value);
        }
        break;
      }

      case 'delete': {
        const segments = update.path.startsWith('/')
          ? update.path.slice(1).split('/')
          : [];
        if (segments.length === 1) {
          delete result[segments[0]];
        } else if (segments.length > 1) {
          const parentPath = '/' + segments.slice(0, -1).join('/');
          const parent = resolvePointer(result, parentPath);
          if (typeof parent === 'object' && parent !== null && !Array.isArray(parent)) {
            delete (parent as Record<string, unknown>)[segments[segments.length - 1]];
          }
        }
        break;
      }
    }
  }

  return result;
}

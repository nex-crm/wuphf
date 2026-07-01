// Per-run cancellation context (deferred finding [15]): a tool run that has
// SETTLED (timed out / done / errored) must abort its in-flight host-side
// capability calls — a real broker fetch or model completion must not keep
// burning after the worker was killed.
//
// DESIGN CHOICE (documented per the dispatch): AsyncLocalStorage over an
// explicit context param. The capability RPC path is
//   worker postMessage -> host onmessage -> hostCaps.invoke -> capability fn
// and CapabilityFn's shape ((...args) => unknown) is part of the tool-code
// contract — the tool's own args fill the parameter list, so a signal cannot
// ride in as an argument without leaking into agent-authored code. The host
// instead wraps each invoke in `withRunSignal(...)`; capability
// implementations that do real I/O read `currentRunSignal()` and compose it
// with their own timeout (Bun supports node:async_hooks AsyncLocalStorage,
// and the store follows the async chain across the await points inside a
// capability). runTool's public signature is unchanged.

import { AsyncLocalStorage } from "node:async_hooks";

const storage = new AsyncLocalStorage<{ signal: AbortSignal }>();

/** The AbortSignal of the tool run this capability call belongs to, if any.
 * Aborts when the run settles; undefined outside a run (e.g. direct calls). */
export function currentRunSignal(): AbortSignal | undefined {
	return storage.getStore()?.signal;
}

/** Run `fn` with `signal` as the ambient run signal (host side of the RPC). */
export function withRunSignal<T>(signal: AbortSignal, fn: () => T): T {
	return storage.run({ signal }, fn);
}

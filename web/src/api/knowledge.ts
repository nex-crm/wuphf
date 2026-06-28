/**
 * Knowledge API client — semantic-memory (gbrain) embedding options.
 *
 * The onboarding wizard and Settings read GET /knowledge/embedding-options to
 * decide which embedder powers the shared brain. The backend's EnsureBrain
 * auto-selects in priority order (OpenAI key, then local Ollama, then keyword),
 * so this surface is mostly informational: the UI reflects the resulting state
 * and offers the user the recommended upgrade (an OpenAI key).
 *
 * Older brokers do not serve this route. The fetch degrades to a keyword
 * default on a 404 (or any transport failure) so onboarding is never blocked by
 * a missing endpoint.
 */

import { ApiError, get, post } from "./client";

/**
 * The lifecycle of the in-UI gbrain install. The broker drives every
 * transition; the wizard only reflects it.
 *
 * - `idle`        no install has been requested (or none is running).
 * - `installing`  the background installer is running (bootstrap Bun, install
 *                 gbrain, init the brain). `install_progress` carries the last line.
 * - `installed`   the install finished and the brain is ready.
 * - `error`       the install failed. `install_error` carries the reason.
 */
export type InstallState = "idle" | "installing" | "installed" | "error";

const INSTALL_STATES: readonly InstallState[] = [
  "idle",
  "installing",
  "installed",
  "error",
];

/**
 * Coerce an unknown wire value to a known InstallState. An older broker (or a
 * malformed payload) that omits or garbles the field reads as `idle` rather
 * than poisoning the install state machine with an off-contract string.
 */
function normalizeInstallState(value: unknown): InstallState {
  return INSTALL_STATES.includes(value as InstallState)
    ? (value as InstallState)
    : "idle";
}

/**
 * Mirrors the broker's GET /knowledge/embedding-options contract.
 *
 * - `gbrain_installed`     the gbrain semantic index is compiled in / available.
 * - `openai_key_set`       an OpenAI key is on disk, so OpenAI embeddings work.
 * - `ollama_available`     a local Ollama embedding model is reachable.
 * - `ollama_model`         the Ollama embedding model id (e.g. nomic-embed-text).
 * - `active_embedder`      the embedder EnsureBrain currently resolves to.
 * - `embedding_available`  true when any semantic embedder is live (not keyword).
 * - `recommended`          the embedder the broker recommends ("openai" today).
 * - `install_state`        lifecycle of the in-UI gbrain install (see InstallState).
 * - `install_progress`     the last line the installer emitted (empty when idle).
 * - `install_error`        a human-readable failure reason (empty unless errored).
 */
export interface EmbeddingOptions {
  gbrain_installed: boolean;
  openai_key_set: boolean;
  ollama_available: boolean;
  ollama_model: string;
  active_embedder: string;
  embedding_available: boolean;
  recommended: string;
  install_state: InstallState;
  install_progress: string;
  install_error: string;
}

/**
 * Keyword/markdown default. Returned whenever the endpoint is unavailable (older
 * broker 404) or unreachable, so the wizard shows the no-setup keyword path and
 * never crashes. `gbrain_installed: false` is the load-bearing signal: it means
 * "no semantic index here, keyword search only".
 */
export const EMBEDDING_OPTIONS_FALLBACK: EmbeddingOptions = {
  gbrain_installed: false,
  openai_key_set: false,
  ollama_available: false,
  ollama_model: "",
  active_embedder: "keyword",
  embedding_available: false,
  recommended: "keyword",
  install_state: "idle",
  install_progress: "",
  install_error: "",
};

/**
 * Merge a (possibly partial) broker payload over the keyword fallback and
 * normalize the install_state union so the result is always a complete, typed
 * EmbeddingOptions. Shared by fetchEmbeddingOptions and installGbrain.
 */
function mergeEmbeddingOptions(
  raw: Partial<EmbeddingOptions>,
): EmbeddingOptions {
  const merged = { ...EMBEDDING_OPTIONS_FALLBACK, ...raw };
  return {
    ...merged,
    install_state: normalizeInstallState(merged.install_state),
  };
}

/**
 * Fetch the embedding options. Merges the broker payload over the fallback so a
 * partial response (an older broker that omits a field) still yields a complete,
 * typed object. Any failure — a 404 from a broker without the route, a timeout,
 * a network drop — degrades to the keyword default rather than throwing, because
 * this status must never block onboarding.
 */
export async function fetchEmbeddingOptions(): Promise<EmbeddingOptions> {
  try {
    const raw = await get<Partial<EmbeddingOptions>>(
      "/knowledge/embedding-options",
    );
    return mergeEmbeddingOptions(raw);
  } catch (err) {
    // 404 = older broker without the route; any other transport failure is
    // equally non-fatal here. Both degrade to the keyword/markdown default.
    if (err instanceof ApiError && err.status !== 404) {
      // Non-404 ApiError (e.g. 500/503): still degrade, but it is a real server
      // signal rather than a missing-route one. Same safe fallback either way.
      return EMBEDDING_OPTIONS_FALLBACK;
    }
    return EMBEDDING_OPTIONS_FALLBACK;
  }
}

/**
 * Ask the broker to install gbrain (bootstrap Bun, install gbrain globally, and
 * init the brain) in the background. POST /knowledge/install takes an empty body
 * and answers 202 with the current options, where `install_state` has usually
 * flipped to "installing". The caller then polls fetchEmbeddingOptions to follow
 * progress.
 *
 * Like fetchEmbeddingOptions, this never throws: a 404 (older broker without the
 * route) or any transport failure degrades to an `error` state so the wizard can
 * show the keyword fallback and a retry, rather than crashing onboarding.
 */
export async function installGbrain(): Promise<EmbeddingOptions> {
  try {
    const raw = await post<Partial<EmbeddingOptions>>("/knowledge/install", {});
    return mergeEmbeddingOptions(raw);
  } catch {
    // We could not even kick off the install (missing route, network drop, a
    // server error). Surface an error state so the UI offers a retry and the
    // keyword fallback, never a thrown exception that blocks the wizard.
    return { ...EMBEDDING_OPTIONS_FALLBACK, install_state: "error" };
  }
}

/**
 * The semantic-memory backend the current options resolve to, mirroring the
 * broker's EnsureBrain priority (OpenAI key, then Ollama, then keyword). Without
 * a gbrain index there is no semantic memory at all, so the answer is always
 * "keyword" regardless of which keys are present.
 */
export type ResolvedEmbedder = "openai" | "ollama" | "keyword";

export function resolveEmbedder(options: EmbeddingOptions): ResolvedEmbedder {
  if (!options.gbrain_installed) return "keyword";
  if (options.openai_key_set) return "openai";
  if (options.ollama_available) return "ollama";
  return "keyword";
}

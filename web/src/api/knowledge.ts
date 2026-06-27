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

import { ApiError, get } from "./client";

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
 */
export interface EmbeddingOptions {
  gbrain_installed: boolean;
  openai_key_set: boolean;
  ollama_available: boolean;
  ollama_model: string;
  active_embedder: string;
  embedding_available: boolean;
  recommended: string;
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
};

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
    return { ...EMBEDDING_OPTIONS_FALLBACK, ...raw };
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

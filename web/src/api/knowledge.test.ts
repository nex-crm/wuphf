import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import { ApiError } from "./client";
import {
  EMBEDDING_OPTIONS_FALLBACK,
  type EmbeddingOptions,
  fetchEmbeddingOptions,
  resolveEmbedder,
} from "./knowledge";

function apiError(status: number): ApiError {
  return new ApiError({
    status,
    statusText: status === 404 ? "Not Found" : "Server Error",
    bodyText: "",
  });
}

describe("knowledge api client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("fetchEmbeddingOptions returns the broker payload over the fallback", async () => {
    const response: EmbeddingOptions = {
      gbrain_installed: true,
      openai_key_set: true,
      ollama_available: false,
      ollama_model: "nomic-embed-text",
      active_embedder: "openai",
      embedding_available: true,
      recommended: "openai",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(fetchEmbeddingOptions()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/knowledge/embedding-options");
  });

  it("merges a partial payload onto the fallback so the shape stays complete", async () => {
    // An older broker that only knows gbrain + ollama, omitting newer fields.
    vi.spyOn(client, "get").mockResolvedValue({
      gbrain_installed: true,
      ollama_available: true,
      ollama_model: "mxbai-embed-large",
    });

    await expect(fetchEmbeddingOptions()).resolves.toEqual({
      ...EMBEDDING_OPTIONS_FALLBACK,
      gbrain_installed: true,
      ollama_available: true,
      ollama_model: "mxbai-embed-large",
    });
  });

  it("degrades to the keyword fallback on a 404 (older broker, no route)", async () => {
    vi.spyOn(client, "get").mockRejectedValue(apiError(404));

    await expect(fetchEmbeddingOptions()).resolves.toEqual(
      EMBEDDING_OPTIONS_FALLBACK,
    );
  });

  it("degrades to the keyword fallback on a 500 server error", async () => {
    vi.spyOn(client, "get").mockRejectedValue(apiError(500));

    await expect(fetchEmbeddingOptions()).resolves.toEqual(
      EMBEDDING_OPTIONS_FALLBACK,
    );
  });

  it("degrades to the keyword fallback on a network/timeout failure", async () => {
    vi.spyOn(client, "get").mockRejectedValue(
      new Error("Broker not responding — request timed out."),
    );

    await expect(fetchEmbeddingOptions()).resolves.toEqual(
      EMBEDDING_OPTIONS_FALLBACK,
    );
  });

  it("resolveEmbedder mirrors EnsureBrain priority: openai, then ollama, then keyword", () => {
    const base: EmbeddingOptions = {
      ...EMBEDDING_OPTIONS_FALLBACK,
      gbrain_installed: true,
    };

    expect(resolveEmbedder({ ...base, openai_key_set: true })).toBe("openai");
    expect(
      resolveEmbedder({
        ...base,
        openai_key_set: true,
        ollama_available: true,
      }),
    ).toBe("openai");
    expect(resolveEmbedder({ ...base, ollama_available: true })).toBe("ollama");
    expect(resolveEmbedder(base)).toBe("keyword");
  });

  it("resolveEmbedder returns keyword without a gbrain index even when a key is set", () => {
    expect(
      resolveEmbedder({
        ...EMBEDDING_OPTIONS_FALLBACK,
        gbrain_installed: false,
        openai_key_set: true,
        ollama_available: true,
      }),
    ).toBe("keyword");
  });
});

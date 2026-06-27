import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import { ApiError } from "./client";
import {
  EMBEDDING_OPTIONS_FALLBACK,
  type EmbeddingOptions,
  fetchEmbeddingOptions,
  installGbrain,
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
      install_state: "installed",
      install_progress: "Brain ready.",
      install_error: "",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(fetchEmbeddingOptions()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/knowledge/embedding-options");
  });

  it("fetchEmbeddingOptions normalizes an off-contract install_state to idle", async () => {
    vi.spyOn(client, "get").mockResolvedValue({
      gbrain_installed: false,
      install_state: "bootstrapping",
    });

    const result = await fetchEmbeddingOptions();
    expect(result.install_state).toBe("idle");
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

  it("installGbrain posts to /knowledge/install and merges the returned state", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue({
      gbrain_installed: false,
      install_state: "installing",
      install_progress: "Bootstrapping Bun.",
    });

    await expect(installGbrain()).resolves.toEqual({
      ...EMBEDDING_OPTIONS_FALLBACK,
      install_state: "installing",
      install_progress: "Bootstrapping Bun.",
    });
    expect(postSpy).toHaveBeenCalledWith("/knowledge/install", {});
  });

  it("installGbrain degrades to an error state on a 404 (older broker, no route)", async () => {
    vi.spyOn(client, "post").mockRejectedValue(apiError(404));

    await expect(installGbrain()).resolves.toEqual({
      ...EMBEDDING_OPTIONS_FALLBACK,
      install_state: "error",
    });
  });

  it("installGbrain degrades to an error state on a transport failure", async () => {
    vi.spyOn(client, "post").mockRejectedValue(new Error("network down"));

    await expect(installGbrain()).resolves.toEqual({
      ...EMBEDDING_OPTIONS_FALLBACK,
      install_state: "error",
    });
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

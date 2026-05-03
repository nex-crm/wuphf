import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import * as api from "./platform";

describe("platform api client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("getHealth calls the health contract", async () => {
    const response: api.HealthResponse = {
      status: "ok",
      provider: "openai",
      provider_model: "gpt-5.2",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getHealth()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/health");
  });

  it("getVersion calls the version contract", async () => {
    const response: api.VersionInfo = {
      version: "0.84.0",
      build_timestamp: "2026-05-02T12:00:00Z",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getVersion()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/version");
  });

  it("getUsage calls the usage contract", async () => {
    const response: api.UsageData = {
      total: { cost_usd: 1.23, total_tokens: 1200 },
      session: { total_tokens: 1200 },
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getUsage()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/usage");
  });
});

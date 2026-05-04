import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import * as api from "./platform";

describe("platform api client", () => {
  const shareStatus: api.WebShareStatus = {
    running: true,
    bind: "100.64.0.2",
    interface: "tailscale0",
    invite_url: "http://100.64.0.2:7890/join/tok",
    expires_at: "2026-05-05T18:00:00Z",
  };

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("getHealth calls the health contract", async () => {
    const response: api.HealthResponse = {
      status: "ok",
      session_mode: "team",
      one_on_one_agent: "",
      focus_mode: false,
      provider: "openai",
      provider_model: "gpt-5.2",
      memory_backend: "nex",
      memory_backend_active: "nex",
      memory_backend_ready: true,
      nex_connected: true,
      build: {
        version: "0.84.0",
        build_timestamp: "2026-05-02T12:00:00Z",
      },
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
      total: {
        input_tokens: 700,
        output_tokens: 400,
        cache_read_tokens: 100,
        cache_creation_tokens: 0,
        total_tokens: 1200,
        cost_usd: 1.23,
        requests: 2,
      },
      session: {
        input_tokens: 700,
        output_tokens: 400,
        cache_read_tokens: 100,
        cache_creation_tokens: 0,
        total_tokens: 1200,
        cost_usd: 1.23,
        requests: 2,
      },
      since: "2026-05-02T12:00:00Z",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getUsage()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/usage");
  });

  it("getShareStatus calls the share status contract", async () => {
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(shareStatus);

    await expect(api.getShareStatus()).resolves.toEqual(shareStatus);
    expect(getSpy).toHaveBeenCalledWith("/share/status");
  });

  it("startShare calls the share start contract", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue(shareStatus);

    await expect(api.startShare()).resolves.toEqual(shareStatus);
    expect(postSpy).toHaveBeenCalledWith("/share/start", {});
  });

  it("stopShare calls the share stop contract", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue(shareStatus);

    await expect(api.stopShare()).resolves.toEqual(shareStatus);
    expect(postSpy).toHaveBeenCalledWith("/share/stop", {});
  });

  it("revokeHumanSession calls the session revoke contract", async () => {
    const delSpy = vi.spyOn(client, "del").mockResolvedValue({ ok: true });

    await expect(api.revokeHumanSession("session-1")).resolves.toEqual({
      ok: true,
    });
    expect(delSpy).toHaveBeenCalledWith("/humans/sessions", {
      id: "session-1",
    });
  });
});

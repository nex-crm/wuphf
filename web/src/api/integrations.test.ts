import { afterEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import {
  disconnectIntegration,
  getComposioSigninStatus,
  getIntegrationAudit,
  getIntegrationConnectStatus,
  listIntegrations,
  startComposioSignin,
  startIntegrationConnection,
  submitIntegrationCredentials,
} from "./integrations";

describe("integrations api client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lists integrations with stable query params", async () => {
    const response = {
      providers: [],
      items: [],
      next_cursor: "next",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(
      listIntegrations({
        provider: "composio",
        search: "gmail",
        connected: "connected",
        limit: 25,
        cursor: "cur",
      }),
    ).resolves.toEqual(response);

    expect(getSpy).toHaveBeenCalledWith("/integrations", {
      provider: "composio",
      search: "gmail",
      connected: "connected",
      limit: 25,
      cursor: "cur",
    });
  });

  it("starts, polls, disconnects, and audits integration connections", async () => {
    const postSpy = vi.spyOn(client, "post");
    const getSpy = vi.spyOn(client, "get");
    postSpy
      .mockResolvedValueOnce({
        provider: "composio",
        platform: "gmail",
        status: "pending",
        auth_url: "https://connect.example",
        connect_id: "ca_123",
      })
      .mockResolvedValueOnce({
        ok: true,
        provider: "composio",
        platform: "gmail",
        connection_key: "ca_123",
        status: "disconnected",
      });
    getSpy
      .mockResolvedValueOnce({
        provider: "composio",
        platform: "gmail",
        status: "connected",
        connection_key: "ca_123",
      })
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({
        events: [
          {
            id: "action-1",
            event_type: "external_action_executed",
            created_at: "2026-06-04T12:00:00Z",
          },
        ],
      });

    await startIntegrationConnection("composio", "gmail");
    await getIntegrationConnectStatus({
      provider: "composio",
      platform: "gmail",
      connect_id: "ca_123",
    });
    await disconnectIntegration("composio", "ca_123", "gmail");
    await expect(
      getIntegrationAudit({
        provider: "composio",
        platform: "gmail",
        connection_key: "ca_123",
      }),
    ).resolves.toHaveLength(0);
    await expect(
      getIntegrationAudit({
        provider: "composio",
        platform: "gmail",
        connection_key: "ca_123",
      }),
    ).resolves.toHaveLength(1);

    expect(postSpy).toHaveBeenNthCalledWith(1, "/integrations/connect", {
      provider: "composio",
      platform: "gmail",
    });
    expect(getSpy).toHaveBeenNthCalledWith(1, "/integrations/connect-status", {
      provider: "composio",
      platform: "gmail",
      connect_id: "ca_123",
    });
    expect(postSpy).toHaveBeenNthCalledWith(2, "/integrations/disconnect", {
      provider: "composio",
      platform: "gmail",
      connection_key: "ca_123",
    });
    expect(getSpy).toHaveBeenNthCalledWith(2, "/integrations/audit", {
      provider: "composio",
      platform: "gmail",
      connection_key: "ca_123",
    });
  });

  it("submits API-key credentials for a non-OAuth toolkit", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue({
      provider: "composio",
      platform: "instantly",
      status: "connected",
      connection_key: "ca_instantly",
    });

    await expect(
      submitIntegrationCredentials("composio", "instantly", {
        generic_api_key: "ik_secret",
      }),
    ).resolves.toMatchObject({ status: "connected" });

    expect(postSpy).toHaveBeenCalledWith("/integrations/connect-credentials", {
      provider: "composio",
      platform: "instantly",
      fields: { generic_api_key: "ik_secret" },
    });
  });

  it("starts and polls the Sign in with Composio flow on stable routes", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue({
      status: "awaiting_login",
      auth_url: "https://platform.composio.dev/?cliKey=sess_1",
    });
    const getSpy = vi.spyOn(client, "get").mockResolvedValue({
      status: "done",
    });

    await expect(startComposioSignin()).resolves.toEqual({
      status: "awaiting_login",
      auth_url: "https://platform.composio.dev/?cliKey=sess_1",
    });
    await expect(getComposioSigninStatus()).resolves.toEqual({
      status: "done",
    });

    expect(postSpy).toHaveBeenCalledWith(
      "/integrations/composio/signin/start",
      {},
    );
    expect(getSpy).toHaveBeenCalledWith("/integrations/composio/signin/status");
  });
});

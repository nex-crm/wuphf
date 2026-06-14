// Typed client for the guided Slack-onboarding wizard. Wraps the four broker
// endpoints the wizard drives (manifest, tokens, connect, status). /slack/connect
// hot-starts the Socket Mode transport in-process, so the wizard no longer
// restarts the broker — it just polls /slack/status until the transport reports
// a live connection.

import { get, post } from "./client";

export interface SlackAppManifest {
  manifest_json: string;
  create_url: string;
  guide: string[];
}

export interface SlackTokenResult {
  ok: boolean;
  bot_user_id: string;
  bot_name: string;
  workspace: string;
}

export interface SlackOnboardingStatus {
  bot_token_set: boolean;
  app_token_set: boolean;
  channel_connected: boolean;
  channel_slug: string;
  /** The in-process Socket Mode transport is connected and healthy. */
  transport_connected: boolean;
  /** tokens + channel + a live transport — i.e. the office is live in Slack. */
  ready: boolean;
}

export interface SlackConnectResult {
  channel_slug: string;
  name: string;
}

/** The ready-to-paste office app manifest + numbered setup guide. */
export function getSlackAppManifest(): Promise<SlackAppManifest> {
  return get<SlackAppManifest>("/slack/app-manifest");
}

/** Validate the bot token against Slack (auth.test) and persist both tokens. */
export function saveSlackTokens(
  botToken: string,
  appToken: string,
): Promise<SlackTokenResult> {
  return post<SlackTokenResult>("/slack/tokens", {
    bot_token: botToken,
    app_token: appToken,
  });
}

/** Bind a Slack channel (by id) to an office channel. */
export function connectSlackChannel(
  channelId: string,
  name?: string,
): Promise<SlackConnectResult> {
  return post<SlackConnectResult>("/slack/connect", {
    channel_id: channelId,
    name,
  });
}

/** Tokens + channel + live-transport state — polled until the office is live. */
export function getSlackOnboardingStatus(): Promise<SlackOnboardingStatus> {
  return get<SlackOnboardingStatus>("/slack/status");
}

/** A bot already present in a bridged channel — an "other AI agent" to connect. */
export interface DiscoveredSlackBot {
  user_id: string;
  name: string;
  real_name?: string;
  already_registered: boolean;
  registered_slug?: string;
}

/** Discover the bots (other AI agents) already in a bridged Slack channel. */
export function discoverSlackBots(
  channelId: string,
): Promise<{ channel_id: string; bots: DiscoveredSlackBot[] }> {
  return get<{ channel_id: string; bots: DiscoveredSlackBot[] }>(
    `/slack/discover?channel_id=${encodeURIComponent(channelId)}`,
  );
}

/** Register a discovered bot as a foreign agent so the CEO can coordinate it. */
export function connectSlackAgent(
  userId: string,
  name: string,
): Promise<{ slug: string; name: string; user_id: string; created: boolean }> {
  return post<{
    slug: string;
    name: string;
    user_id: string;
    created: boolean;
  }>("/slack/agents", { user_id: userId, name });
}

// Typed client for the guided Slack-onboarding wizard. Wraps the four
// broker endpoints the wizard drives (manifest, tokens, connect, status) plus
// the shared restartBroker used to activate the Socket Mode transport.

import { get, post, restartBroker } from "./client";

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

/** Tokens + channel + readiness state — polled to bring the office back. */
export function getSlackOnboardingStatus(): Promise<SlackOnboardingStatus> {
  return get<SlackOnboardingStatus>("/slack/status");
}

export { restartBroker };

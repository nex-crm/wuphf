import { useMemo } from "react";
import type { ApiToken, BrokerUrl } from "../bootstrap/types.ts";
import { useReadyBrokerBootstrap } from "../bootstrap/useBrokerBootstrap.ts";

export type JsonCodec<T> = (value: unknown) => T;
export type FetchLike = typeof fetch;

export interface BrokerApiSession {
  readonly brokerUrl: BrokerUrl;
  readonly bearer: ApiToken;
}

export interface BrokerApiClient {
  getJson<T>(path: string, codec: JsonCodec<T>): Promise<T>;
  postJson<T>(path: string, body: unknown, codec: JsonCodec<T>): Promise<T>;
}

export class BrokerHttpError extends Error {
  readonly status: number;

  constructor(status: number) {
    super(`Broker request failed with HTTP ${String(status)}`);
    this.name = "BrokerHttpError";
    this.status = status;
  }
}

export function useBrokerApiClient(): BrokerApiClient {
  const bootstrap = useReadyBrokerBootstrap();
  return useMemo(
    () =>
      createBrokerApiClient({
        brokerUrl: bootstrap.brokerUrl,
        bearer: bootstrap.bearer,
      }),
    [bootstrap.bearer, bootstrap.brokerUrl],
  );
}

export function createBrokerApiClient(
  session: BrokerApiSession,
  fetchImpl: FetchLike = fetch,
): BrokerApiClient {
  return {
    getJson: (path, codec) => fetchBrokerJson(session, fetchImpl, path, codec),
    postJson: (path, body, codec) =>
      fetchBrokerJson(session, fetchImpl, path, codec, {
        method: "POST",
        body: JSON.stringify(body),
      }),
  };
}

export async function fetchBrokerJson<T>(
  session: BrokerApiSession,
  fetchImpl: FetchLike,
  path: string,
  codec: JsonCodec<T>,
  init: RequestInit = {},
): Promise<T> {
  const response = await fetchImpl(brokerApiUrl(session.brokerUrl, path), {
    ...init,
    headers: authorizedHeaders(session.bearer, init),
  });
  if (!response.ok) {
    throw new BrokerHttpError(response.status);
  }
  return codec(await response.json());
}

function authorizedHeaders(bearer: ApiToken, init: RequestInit): Headers {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${bearer}`);
  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  return headers;
}

function brokerApiUrl(brokerUrl: BrokerUrl, path: string): string {
  if (!path.startsWith("/")) {
    throw new Error(`Broker API path must be absolute: ${path}`);
  }
  const url = new URL(path, `${brokerUrl}/`);
  if (url.origin !== brokerUrl) {
    throw new Error(`Broker API path escaped broker origin: ${path}`);
  }
  return url.toString();
}

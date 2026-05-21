import { type RouteError, routeErrorFromJson } from "@wuphf/protocol/browser";
import { useMemo } from "react";

import type { ApiToken, BrokerUrl } from "../bootstrap/types.ts";
import { useReadyBrokerBootstrap } from "../bootstrap/useBrokerBootstrap.ts";

export type JsonCodec<T> = (value: unknown) => T;
export type JsonEncoder<T> = (value: T) => unknown;
export type FetchLike = typeof fetch;

export interface BrokerApiSession {
  readonly brokerUrl: BrokerUrl;
  readonly bearer: ApiToken;
}

export interface BrokerApiClient {
  getJson<T>(path: string, codec: JsonCodec<T>): Promise<T>;
  postJson<TReq, TRes>(
    path: string,
    request: TReq,
    requestToJsonValue: JsonEncoder<TReq>,
    responseFromJson: JsonCodec<TRes>,
  ): Promise<TRes>;
}

export class BrokerHttpError extends Error {
  readonly status: number;
  readonly routeError: RouteError | null;

  constructor(status: number, routeError: RouteError | null = null) {
    super(`Broker request failed with HTTP ${String(status)}`);
    this.name = "BrokerHttpError";
    this.status = status;
    this.routeError = routeError;
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
    postJson: (path, request, requestToJsonValue, responseFromJson) =>
      fetchBrokerJson(session, fetchImpl, path, responseFromJson, {
        method: "POST",
        body: JSON.stringify(requestToJsonValue(request)),
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
    throw new BrokerHttpError(response.status, await decodeRouteError(response));
  }
  return codec(await response.json());
}

async function decodeRouteError(response: Response): Promise<RouteError | null> {
  try {
    return routeErrorFromJson(await response.json());
  } catch {
    return null;
  }
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

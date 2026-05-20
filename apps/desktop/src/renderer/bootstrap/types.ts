import type { GetBrokerStatusResponse } from "../../shared/api-contract.ts";

declare const apiTokenBrand: unique symbol;
declare const brokerUrlBrand: unique symbol;

export type ApiToken = string & { readonly [apiTokenBrand]: "ApiToken" };
export type BrokerUrl = string & { readonly [brokerUrlBrand]: "BrokerUrl" };

export interface BrokerBootstrapLoading {
  readonly status: "loading";
  readonly brokerStatus: GetBrokerStatusResponse | null;
  readonly bearer: null;
  readonly brokerUrl: null;
  readonly error: null;
  readonly retry: () => void;
}

export interface BrokerBootstrapReady {
  readonly status: "ready";
  readonly brokerStatus: GetBrokerStatusResponse;
  readonly bearer: ApiToken;
  readonly brokerUrl: BrokerUrl;
  readonly error: null;
  readonly retry: () => void;
}

export interface BrokerBootstrapError {
  readonly status: "error";
  readonly brokerStatus: GetBrokerStatusResponse | null;
  readonly bearer: null;
  readonly brokerUrl: null;
  readonly error: string;
  readonly retry: () => void;
}

export type BrokerBootstrapState =
  | BrokerBootstrapLoading
  | BrokerBootstrapReady
  | BrokerBootstrapError;

export function apiTokenFromBootstrap(value: string): ApiToken {
  return value as ApiToken;
}

export function brokerUrlFromBootstrap(value: string): BrokerUrl {
  return value as BrokerUrl;
}

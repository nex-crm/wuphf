import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import { vi } from "vitest";

import {
  BrokerBootstrapContext,
} from "../../src/renderer/bootstrap/useBrokerBootstrap.ts";
import { parseBootstrap } from "../../src/renderer/bootstrap.ts";
import {
  type BrokerBootstrapReady,
  type BrokerBootstrapState,
} from "../../src/renderer/bootstrap/types.ts";
import { BrokerStreamStateProvider } from "../../src/renderer/sse/useBrokerEvents.ts";
import type { WuphfDesktopApi } from "../../src/shared/api-contract.ts";

export const VALID_TOKEN = "A".repeat(16);
export const VALID_BROKER_URL = "http://127.0.0.1:54321";
export const VALID_BOOTSTRAP = parseBootstrap({
  token: VALID_TOKEN,
  broker_url: VALID_BROKER_URL,
});

export function createDesktopApi(
  overrides: Partial<WuphfDesktopApi> = {},
): WuphfDesktopApi {
  return {
    openExternal: vi.fn<WuphfDesktopApi["openExternal"]>(() => Promise.resolve({ ok: true })),
    showItemInFolder: vi.fn<WuphfDesktopApi["showItemInFolder"]>(() =>
      Promise.resolve({ ok: true }),
    ),
    getAppVersion: vi.fn<WuphfDesktopApi["getAppVersion"]>(() =>
      Promise.resolve({ version: "0.0.0-test" }),
    ),
    getPlatform: vi.fn<WuphfDesktopApi["getPlatform"]>(() =>
      Promise.resolve({ platform: "linux", arch: "x64" }),
    ),
    getBrokerStatus: vi.fn<WuphfDesktopApi["getBrokerStatus"]>(() =>
      Promise.resolve({
        status: "alive",
        pid: 1234,
        restartCount: 0,
        brokerUrl: VALID_BROKER_URL,
      }),
    ),
    ...overrides,
  };
}

export function readyBootstrapState(
  overrides: Partial<BrokerBootstrapReady> = {},
): BrokerBootstrapReady {
  return {
    status: "ready",
    brokerStatus: {
      status: "alive",
      pid: 1234,
      restartCount: 0,
      brokerUrl: VALID_BROKER_URL,
    },
    bearer: VALID_BOOTSTRAP.token,
    brokerUrl: VALID_BOOTSTRAP.brokerUrl,
    error: null,
    retry: vi.fn<() => void>(),
    ...overrides,
  };
}

export function renderWithProviders(
  ui: ReactNode,
  bootstrap: BrokerBootstrapState = readyBootstrapState(),
): RenderResult & { readonly queryClient: QueryClient } {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <BrokerStreamStateProvider>
        <BrokerBootstrapContext.Provider value={bootstrap}>{ui}</BrokerBootstrapContext.Provider>
      </BrokerStreamStateProvider>
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

export function jsonResponse(value: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(value),
  } as Response;
}

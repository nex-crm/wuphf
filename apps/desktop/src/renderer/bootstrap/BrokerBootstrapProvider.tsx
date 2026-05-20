import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { parseBootstrap } from "../bootstrap.ts";
import type { WuphfDesktopApi } from "../../shared/api-contract.ts";
import { BrokerBootstrapContext } from "./useBrokerBootstrap.ts";
import {
  apiTokenFromBootstrap,
  brokerUrlFromBootstrap,
  type BrokerBootstrapReady,
  type BrokerBootstrapState,
} from "./types.ts";

type BootstrapDesktopApi = Pick<WuphfDesktopApi, "getBrokerStatus">;
type FetchLike = typeof fetch;

export interface BrokerBootstrapProviderProps {
  readonly children: ReactNode;
  readonly desktopApi?: BootstrapDesktopApi;
  readonly fetchImpl?: FetchLike;
}

interface LoadedBrokerBootstrap {
  readonly brokerStatus: BrokerBootstrapReady["brokerStatus"];
  readonly brokerUrl: BrokerBootstrapReady["brokerUrl"];
  readonly bearer: BrokerBootstrapReady["bearer"];
}

export function BrokerBootstrapProvider({
  children,
  desktopApi = window.wuphf,
  fetchImpl = fetch,
}: BrokerBootstrapProviderProps) {
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => {
    setAttempt((value) => value + 1);
  }, []);
  const [state, setState] = useState<BrokerBootstrapState>({
    status: "loading",
    brokerStatus: null,
    bearer: null,
    brokerUrl: null,
    error: null,
    retry,
  });

  useEffect(() => {
    let active = true;
    setState((previous) => ({
      status: "loading",
      brokerStatus: previous.brokerStatus,
      bearer: null,
      brokerUrl: null,
      error: null,
      retry,
    }));
    void loadBrokerBootstrap(desktopApi, fetchImpl)
      .then((loaded) => {
        if (!active) return;
        setState({
          status: "ready",
          brokerStatus: loaded.brokerStatus,
          bearer: loaded.bearer,
          brokerUrl: loaded.brokerUrl,
          error: null,
          retry,
        });
      })
      .catch((error: unknown) => {
        if (!active) return;
        setState((previous) => ({
          status: "error",
          brokerStatus: previous.brokerStatus,
          bearer: null,
          brokerUrl: null,
          error: error instanceof Error ? error.message : "Broker bootstrap failed",
          retry,
        }));
      });
    return () => {
      active = false;
    };
  }, [attempt, desktopApi, fetchImpl, retry]);

  const value = useMemo(() => state, [state]);
  return <BrokerBootstrapContext.Provider value={value}>{children}</BrokerBootstrapContext.Provider>;
}

export async function loadBrokerBootstrap(
  desktopApi: BootstrapDesktopApi,
  fetchImpl: FetchLike,
): Promise<LoadedBrokerBootstrap> {
  const brokerStatus = await desktopApi.getBrokerStatus();
  if (brokerStatus.brokerUrl === null || brokerStatus.brokerUrl.length === 0) {
    throw new Error("broker not ready");
  }

  const tokenResponse = await fetchImpl(`${brokerStatus.brokerUrl}/api-token`);
  if (!tokenResponse.ok) {
    throw new Error(`api-token ${String(tokenResponse.status)}`);
  }
  const parsed = parseBootstrap(await tokenResponse.json());
  const bearer = apiTokenFromBootstrap(parsed.token);
  const brokerUrl = brokerUrlFromBootstrap(parsed.brokerUrl);

  const healthResponse = await fetchImpl(`${brokerUrl}/api/health`, {
    headers: { Authorization: `Bearer ${bearer}` },
  });
  if (!healthResponse.ok) {
    throw new Error(`broker health ${String(healthResponse.status)}`);
  }

  return { brokerStatus, brokerUrl, bearer };
}

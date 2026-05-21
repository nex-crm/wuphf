import { createContext, useContext } from "react";

import type { BrokerBootstrapReady, BrokerBootstrapState } from "./types.ts";

export const BrokerBootstrapContext = createContext<BrokerBootstrapState | null>(null);

export function useBrokerBootstrap(): BrokerBootstrapState {
  const state = useContext(BrokerBootstrapContext);
  if (state === null) {
    throw new Error("useBrokerBootstrap must be used inside BrokerBootstrapProvider");
  }
  return state;
}

export function useReadyBrokerBootstrap(): BrokerBootstrapReady {
  const state = useBrokerBootstrap();
  if (state.status !== "ready") {
    throw new Error("Broker bootstrap is not ready");
  }
  return state;
}

// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  BrokerBootstrapContext,
  useBrokerBootstrap,
  useReadyBrokerBootstrap,
} from "../../src/renderer/bootstrap/useBrokerBootstrap.ts";
import { readyBootstrapState } from "./test-utils.tsx";

describe("useBrokerBootstrap", () => {
  it("reads the provider state", () => {
    render(
      <BrokerBootstrapContext.Provider value={readyBootstrapState()}>
        <ReadyProbe />
      </BrokerBootstrapContext.Provider>,
    );

    expect(screen.getByText("ready")).toBeInTheDocument();
  });

  it("throws outside the provider", () => {
    expect(() => render(<Probe />)).toThrow(
      "useBrokerBootstrap must be used inside BrokerBootstrapProvider",
    );
  });

  it("throws when a ready-only consumer runs before bootstrap", () => {
    expect(() =>
      render(
        <BrokerBootstrapContext.Provider
          value={{
            status: "loading",
            brokerStatus: null,
            bearer: null,
            brokerUrl: null,
            error: null,
            retry: () => undefined,
          }}
        >
          <ReadyProbe />
        </BrokerBootstrapContext.Provider>,
      ),
    ).toThrow("Broker bootstrap is not ready");
  });
});

function Probe() {
  const bootstrap = useBrokerBootstrap();
  return <p>{bootstrap.status}</p>;
}

function ReadyProbe() {
  const bootstrap = useReadyBrokerBootstrap();
  return <p>{bootstrap.status}</p>;
}

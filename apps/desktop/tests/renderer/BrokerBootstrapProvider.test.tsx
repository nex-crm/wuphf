// @vitest-environment happy-dom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  BrokerBootstrapProvider,
  loadBrokerBootstrap,
} from "../../src/renderer/bootstrap/BrokerBootstrapProvider.tsx";
import { useBrokerBootstrap } from "../../src/renderer/bootstrap/useBrokerBootstrap.ts";
import type { WuphfDesktopApi } from "../../src/shared/api-contract.ts";
import {
  createDesktopApi,
  jsonResponse,
  VALID_BROKER_URL,
  VALID_TOKEN,
} from "./test-utils.tsx";

describe("BrokerBootstrapProvider", () => {
  it("transitions from loading to ready after status, api-token, and health succeed", async () => {
    const desktopApi = createDesktopApi();
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      const url = String(input);
      if (url.endsWith("/api-token")) {
        return Promise.resolve(
          jsonResponse({ token: VALID_TOKEN, broker_url: VALID_BROKER_URL }),
        );
      }
      if (url.endsWith("/api/health")) {
        expect(init?.headers).toEqual({ Authorization: `Bearer ${VALID_TOKEN}` });
        return Promise.resolve(jsonResponse({ ok: true }));
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`));
    });

    render(
      <BrokerBootstrapProvider desktopApi={desktopApi} fetchImpl={fetchMock}>
        <BootstrapProbe />
      </BrokerBootstrapProvider>,
    );

    expect(screen.getByText("loading")).toBeInTheDocument();
    expect(await screen.findByText("ready")).toBeInTheDocument();
    expect(screen.getByText(VALID_BROKER_URL)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(`${VALID_BROKER_URL}/api-token`);
    expect(fetchMock).toHaveBeenCalledWith(`${VALID_BROKER_URL}/api/health`, {
      headers: { Authorization: `Bearer ${VALID_TOKEN}` },
    });
  });

  it("exposes the error state and retries bootstrap on demand", async () => {
    let brokerUrl: string | null = null;
    const getBrokerStatus = vi.fn<WuphfDesktopApi["getBrokerStatus"]>(() =>
      Promise.resolve({
        status: brokerUrl === null ? "dead" : "alive",
        pid: brokerUrl === null ? null : 1234,
        restartCount: brokerUrl === null ? 1 : 2,
        brokerUrl,
      }),
    );
    const desktopApi = createDesktopApi({ getBrokerStatus });
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input);
      if (url.endsWith("/api-token")) {
        return Promise.resolve(
          jsonResponse({ token: VALID_TOKEN, broker_url: VALID_BROKER_URL }),
        );
      }
      if (url.endsWith("/api/health")) {
        return Promise.resolve(jsonResponse({ ok: true }));
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`));
    });

    render(
      <BrokerBootstrapProvider desktopApi={desktopApi} fetchImpl={fetchMock}>
        <BootstrapProbe />
      </BrokerBootstrapProvider>,
    );

    expect(await screen.findByText("broker not ready")).toBeInTheDocument();
    brokerUrl = VALID_BROKER_URL;
    fireEvent.click(screen.getByRole("button", { name: "retry" }));

    expect(await screen.findByText("ready")).toBeInTheDocument();
    expect(getBrokerStatus).toHaveBeenCalledTimes(2);
  });

  it("surfaces a bootstrap failure without leaking thrown non-errors", async () => {
    const desktopApi = createDesktopApi();
    const fetchMock = vi.fn<typeof fetch>(() => Promise.reject("network refused"));

    render(
      <BrokerBootstrapProvider desktopApi={desktopApi} fetchImpl={fetchMock}>
        <BootstrapProbe />
      </BrokerBootstrapProvider>,
    );

    expect(await screen.findByText("Broker bootstrap failed")).toBeInTheDocument();
  });

  it("rejects non-ok token and health responses", async () => {
    const desktopApi = createDesktopApi();

    await expect(
      loadBrokerBootstrap(desktopApi, () => Promise.resolve(jsonResponse({ error: "no" }, 403))),
    ).rejects.toThrow("api-token 403");

    await expect(
      loadBrokerBootstrap(desktopApi, (input) => {
        const url = String(input);
        if (url.endsWith("/api-token")) {
          return Promise.resolve(
            jsonResponse({ token: VALID_TOKEN, broker_url: VALID_BROKER_URL }),
          );
        }
        return Promise.resolve(jsonResponse({ error: "down" }, 503));
      }),
    ).rejects.toThrow("broker health 503");
  });
});

function BootstrapProbe() {
  const bootstrap = useBrokerBootstrap();
  return (
    <div>
      <p>{bootstrap.status}</p>
      {bootstrap.status === "ready" && <p>{bootstrap.brokerUrl}</p>}
      {bootstrap.status === "error" && <p>{bootstrap.error}</p>}
      <button onClick={bootstrap.retry} type="button">
        retry
      </button>
    </div>
  );
}

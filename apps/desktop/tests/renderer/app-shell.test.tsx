// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { createMemoryHistory } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App, createAppRouter } from "../../src/renderer/app/App.tsx";
import { AppShell } from "../../src/renderer/app/AppShell.tsx";
import type { WuphfDesktopApi } from "../../src/shared/api-contract.ts";
import {
  createDesktopApi,
  jsonResponse,
  renderWithProviders,
  VALID_BROKER_URL,
  VALID_TOKEN,
} from "./test-utils.tsx";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("AppShell", () => {
  it("renders navigation, broker status, and child content", () => {
    renderWithProviders(
      <AppShell>
        <p>Route content</p>
      </AppShell>,
    );

    expect(screen.getByRole("navigation", { name: "Primary" })).toBeInTheDocument();
    expect(screen.getByText("Broker ready").closest('[role="status"]')).toBeInTheDocument();
    expect(screen.getByText("Route content")).toBeInTheDocument();
  });

  it("renders loading and error broker tones", () => {
    const { unmount } = renderWithProviders(
      <AppShell>
        <p>Route content</p>
      </AppShell>,
      {
        status: "loading",
        brokerStatus: null,
        bearer: null,
        brokerUrl: null,
        error: null,
        retry: vi.fn<() => void>(),
      },
    );

    expect(screen.getByText("Broker loading")).toHaveAttribute("data-tone", "pending");
    expect(screen.getByText("Broker loading")).toHaveAttribute("aria-busy", "true");

    unmount();

    renderWithProviders(
      <AppShell>
        <p>Error route content</p>
      </AppShell>,
      {
        status: "error",
        brokerStatus: null,
        bearer: null,
        brokerUrl: null,
        error: "down",
        retry: vi.fn<() => void>(),
      },
    );

    expect(screen.getByText("Broker error")).toHaveAttribute("data-tone", "error");
  });
});

describe("App", () => {
  it("mounts providers and renders the index route", async () => {
    const desktopApi = createDesktopApi();
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
      if (url.endsWith("/api/events")) {
        return Promise.resolve({ ok: false, status: 401, body: null } as Response);
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`));
    });
    installWindowApi(desktopApi);
    vi.stubGlobal("fetch", fetchMock);
    const router = createAppRouter(createMemoryHistory({ initialEntries: ["/"] }));

    render(<App routerInstance={router} />);

    expect(await screen.findByText("Desktop status")).toBeInTheDocument();
    expect(await screen.findByText("Ready")).toBeInTheDocument();
  });
});

function installWindowApi(api: WuphfDesktopApi): void {
  Object.defineProperty(window, "wuphf", {
    configurable: true,
    value: api,
  });
}

// @vitest-environment happy-dom

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IndexRoute } from "../../src/renderer/app/routes/index.tsx";
import type { WuphfDesktopApi } from "../../src/shared/api-contract.ts";
import { createDesktopApi, readyBootstrapState, renderWithProviders } from "./test-utils.tsx";

describe("IndexRoute", () => {
  beforeEach(() => {
    installWindowApi(createDesktopApi());
  });

  it("shows the ready broker status and version chip", async () => {
    renderWithProviders(<IndexRoute />);

    expect(await screen.findByText("v0.0.0-test")).toBeInTheDocument();
    expect(screen.getByText("Ready")).toBeInTheDocument();
  });

  it("shows loading state", () => {
    renderWithProviders(<IndexRoute />, {
      status: "loading",
      brokerStatus: null,
      bearer: null,
      brokerUrl: null,
      error: null,
      retry: vi.fn<() => void>(),
    });

    expect(screen.getByText("Starting").closest('[role="status"]')).toHaveAttribute(
      "aria-busy",
      "true",
    );
  });

  it("calls retry from the error state", () => {
    const retry = vi.fn<() => void>();
    renderWithProviders(<IndexRoute />, {
      status: "error",
      brokerStatus: null,
      bearer: null,
      brokerUrl: null,
      error: "broker not ready",
      retry,
    });

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("does not render a version chip before the version resolves", () => {
    const api = createDesktopApi({
      getAppVersion: vi.fn<WuphfDesktopApi["getAppVersion"]>(() => new Promise(() => undefined)),
    });
    installWindowApi(api);

    renderWithProviders(<IndexRoute />, readyBootstrapState());

    expect(screen.queryByText(/^v/)).not.toBeInTheDocument();
  });
});

function installWindowApi(api: WuphfDesktopApi): void {
  Object.defineProperty(window, "wuphf", {
    configurable: true,
    value: api,
  });
}

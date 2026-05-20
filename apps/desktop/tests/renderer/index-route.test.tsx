// @vitest-environment happy-dom

import { act, fireEvent, render, screen } from "@testing-library/react";
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

  it("does not update state after unmount when the version resolves", async () => {
    const version = deferred<Awaited<ReturnType<WuphfDesktopApi["getAppVersion"]>>>();
    const api = createDesktopApi({
      getAppVersion: vi.fn<WuphfDesktopApi["getAppVersion"]>(() => version.promise),
    });
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    installWindowApi(api);

    const { unmount } = renderWithProviders(<IndexRoute />, readyBootstrapState());
    unmount();

    await act(async () => {
      version.resolve({ version: "9.9.9-test" });
      await version.promise;
    });

    expect(consoleError).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });
});

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve: ((value: T) => void) | null = null;
  const promise = new Promise<T>((innerResolve) => {
    resolve = innerResolve;
  });
  if (resolve === null) {
    throw new Error("deferred resolver was not initialized");
  }
  return { promise, resolve };
}

function installWindowApi(api: WuphfDesktopApi): void {
  Object.defineProperty(window, "wuphf", {
    configurable: true,
    value: api,
  });
}

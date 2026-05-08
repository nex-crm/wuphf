import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as clientApi from "../../api/client";
import * as upgradeApi from "../../api/upgrade";
import { VersionModal } from "./VersionModal";

function makeWrapper() {
  // A fresh QueryClient per test guarantees no /upgrade-check cache
  // bleed between cases — the modal queries on `["upgrade-check"]` which
  // would otherwise carry over.
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false },
    },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  while (document.body.firstChild) {
    document.body.removeChild(document.body.firstChild);
  }
});

beforeEach(() => {
  (document.activeElement as HTMLElement | null)?.blur?.();
});

describe("<VersionModal>", () => {
  it("renders nothing when open=false", () => {
    const Wrapper = makeWrapper();
    const { container } = render(
      <Wrapper>
        <VersionModal open={false} onClose={vi.fn()} />
      </Wrapper>,
    );
    expect(container.querySelector(".version-modal")).toBeNull();
  });

  it("renders the dev sentinel without a v-prefix", async () => {
    vi.spyOn(upgradeApi, "getUpgradeCheck").mockResolvedValue({
      current: "dev",
      latest: "0.83.10",
      upgrade_available: false,
      is_dev_build: true,
      upgrade_command: "npm install -g wuphf@latest",
    });
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <VersionModal open={true} onClose={vi.fn()} />
      </Wrapper>,
    );
    // The current-version <code> should read `dev`, not `vdev`.
    await waitFor(() => {
      expect(screen.getByText("dev build")).toBeInTheDocument();
    });
    const codes = screen.getAllByText("dev");
    expect(codes.length).toBeGreaterThan(0);
    expect(screen.queryByText("vdev")).toBeNull();
  });

  it("does not surface a stale Install-complete after close-during-run", async () => {
    // Hold runUpgrade open so we control when it resolves. Exercises the
    // stale-outcome guard: the user closes the modal before the install
    // finishes; once the promise eventually resolves, its result MUST NOT
    // be stored as run.phase=done — otherwise the next open shows an
    // Install-complete + Restart-now prompt the user never asked for.
    let resolveRun: (r: upgradeApi.UpgradeRunResult) => void = () => {};
    vi.spyOn(upgradeApi, "getUpgradeCheck").mockResolvedValue({
      current: "0.83.0",
      latest: "0.84.0",
      upgrade_available: true,
      is_dev_build: false,
      upgrade_command: "npm install -g wuphf@latest",
    });
    vi.spyOn(upgradeApi, "runUpgrade").mockImplementation(
      () =>
        new Promise<upgradeApi.UpgradeRunResult>((resolve) => {
          resolveRun = resolve;
        }),
    );

    const Wrapper = makeWrapper();
    const onClose = vi.fn();
    const { rerender } = render(
      <Wrapper>
        <VersionModal open={true} onClose={onClose} />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Force update")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText("Force update"));
    await waitFor(() => {
      expect(screen.getByText("Installing…")).toBeInTheDocument();
    });

    // Close mid-run.
    rerender(
      <Wrapper>
        <VersionModal open={false} onClose={onClose} />
      </Wrapper>,
    );

    // Now the in-flight promise resolves AFTER the close — should be a no-op.
    resolveRun({ ok: true, output: "added 1 package" });
    // Give microtasks a chance to flush.
    await new Promise((r) => setTimeout(r, 0));

    // Reopen — there must be no Install-complete section, and the primary
    // CTA must read "Force update" again, not "Restart now".
    rerender(
      <Wrapper>
        <VersionModal open={true} onClose={onClose} />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Force update")).toBeInTheDocument();
    });
    expect(screen.queryByText("Install complete")).toBeNull();
    expect(screen.queryByText("Restart now")).toBeNull();
  });

  it("calls restartBroker and closes on restart success", async () => {
    vi.spyOn(upgradeApi, "getUpgradeCheck").mockResolvedValue({
      current: "0.83.0",
      latest: "0.83.0",
      upgrade_available: false,
      is_dev_build: false,
      upgrade_command: "npm install -g wuphf@latest",
    });
    const restartSpy = vi
      .spyOn(clientApi, "restartBroker")
      .mockResolvedValue({ ok: true });

    const Wrapper = makeWrapper();
    const onClose = vi.fn();
    render(
      <Wrapper>
        <VersionModal open={true} onClose={onClose} />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Restart broker")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText("Restart broker"));

    await waitFor(() => {
      expect(restartSpy).toHaveBeenCalledTimes(1);
      expect(onClose).toHaveBeenCalled();
    });
  });

  it("surfaces an inline error and stays open when restartBroker fails", async () => {
    vi.spyOn(upgradeApi, "getUpgradeCheck").mockResolvedValue({
      current: "0.83.0",
      latest: "0.83.0",
      upgrade_available: false,
      is_dev_build: false,
      upgrade_command: "npm install -g wuphf@latest",
    });
    vi.spyOn(clientApi, "restartBroker").mockRejectedValue(
      new Error("broker socket closed"),
    );

    const Wrapper = makeWrapper();
    const onClose = vi.fn();
    render(
      <Wrapper>
        <VersionModal open={true} onClose={onClose} />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Restart broker")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText("Restart broker"));

    await waitFor(() => {
      expect(screen.getByText(/broker socket closed/i)).toBeInTheDocument();
    });
    expect(onClose).not.toHaveBeenCalled();
    // Button is re-enabled after the failure.
    expect(
      screen.getByRole("button", { name: "Restart broker" }),
    ).toBeEnabled();
  });
});

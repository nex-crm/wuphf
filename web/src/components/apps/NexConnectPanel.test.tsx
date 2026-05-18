import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const postMock = vi.fn();
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, post: (...args: unknown[]) => postMock(...args) };
});

const showNoticeMock = vi.fn();
vi.mock("../ui/Toast", () => ({
  showNotice: (...args: unknown[]) => showNoticeMock(...args),
}));

import { NexConnectPanel } from "./NexConnectPanel";

// Drives the panel through one registration attempt with the given email.
async function submitEmail(email: string) {
  render(<NexConnectPanel />);
  fireEvent.change(
    screen.getByLabelText("Email address for Nex registration"),
    { target: { value: email } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Connect Nex" }));
}

describe("<NexConnectPanel>", () => {
  beforeEach(() => {
    postMock.mockReset();
    showNoticeMock.mockReset();
  });

  it("confirms success and tells the user to check their inbox", async () => {
    postMock.mockResolvedValue({ status: "ok" });
    await submitEmail("founder@example.com");

    expect(
      await screen.findByText(/check your inbox at founder@example.com/i),
    ).toBeInTheDocument();
    expect(showNoticeMock).toHaveBeenCalledWith(
      expect.stringContaining("Nex API key"),
      "success",
    );
  });

  // The reported bug: the @nex-ai/nex npm shim is on PATH but the real
  // binary is not, so `nex-cli setup` exits with "nex-cli binary not found".
  // The broker forwards that as a 502 JSON body. The panel must degrade to
  // the register-externally fallback — never render the raw JSON.
  it("flips to the fallback when only the npm shim is installed", async () => {
    postMock.mockRejectedValue(
      new Error(
        JSON.stringify({
          status: "error",
          error:
            "nex-cli setup hi@mustafa.li: nex-cli binary not found. Install it with: curl ...",
        }),
      ),
    );
    await submitEmail("hi@mustafa.li");

    expect(
      await screen.findByRole("link", { name: /nex\.ai\/register/i }),
    ).toBeInTheDocument();
    // The raw JSON / stderr blob must not leak into the UI.
    expect(screen.queryByText(/"status":"error"/)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("flips to the fallback when nex-cli is not installed at all", async () => {
    postMock.mockRejectedValue(
      new Error(
        JSON.stringify({ status: "error", error: "nex-cli not installed" }),
      ),
    );
    await submitEmail("founder@example.com");

    expect(
      await screen.findByRole("link", { name: /nex\.ai\/register/i }),
    ).toBeInTheDocument();
  });

  it("shows the parsed message (not raw JSON) for a genuine failure", async () => {
    postMock.mockRejectedValue(
      new Error(
        JSON.stringify({
          status: "error",
          error: "setup failed: rate limited",
        }),
      ),
    );
    await submitEmail("founder@example.com");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("setup failed: rate limited");
    expect(alert).not.toHaveTextContent("{");
  });
});

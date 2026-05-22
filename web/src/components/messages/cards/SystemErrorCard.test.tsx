import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  parseSystemAuthErrorPayload,
  SystemErrorCard,
} from "./SystemErrorCard";

describe("parseSystemAuthErrorPayload", () => {
  it("narrows a valid payload object to typed string fields", () => {
    const out = parseSystemAuthErrorPayload({
      provider: "claude-code",
      sign_in_command: "claude auth login",
      detail: "Not logged in",
      reporter: "ceo",
    });
    expect(out).toEqual({
      provider: "claude-code",
      sign_in_command: "claude auth login",
      detail: "Not logged in",
      reporter: "ceo",
    });
  });

  it("returns an empty object for non-object payloads (defense in depth)", () => {
    expect(parseSystemAuthErrorPayload(null)).toEqual({});
    expect(parseSystemAuthErrorPayload("malicious")).toEqual({});
    expect(parseSystemAuthErrorPayload(42)).toEqual({});
    expect(parseSystemAuthErrorPayload(["a", "b"])).toEqual({});
  });

  it("drops non-string fields silently", () => {
    const out = parseSystemAuthErrorPayload({
      provider: 123,
      sign_in_command: { nested: "obj" },
      detail: "valid string",
    });
    expect(out).toEqual({ detail: "valid string" });
  });
});

describe("SystemErrorCard", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      writable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it("renders the provider name, detail, and sign-in command", () => {
    render(
      <SystemErrorCard
        payload={{
          provider: "claude-code",
          sign_in_command: "claude auth login",
          detail: "Claude CLI requires login.",
        }}
      />,
    );
    const card = screen.getByTestId("system-error-card");
    expect(card.getAttribute("data-provider")).toBe("claude-code");
    expect(card.textContent).toMatch(/Sign in required/);
    expect(card.textContent).toMatch(/claude-code/);
    expect(card.textContent).toMatch(/Claude CLI requires login/);
    expect(screen.getByTestId("system-error-card-command")).toHaveTextContent(
      "claude auth login",
    );
  });

  it("copies the sign-in command on click and flips to 'Copied' briefly", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      writable: true,
      value: { writeText },
    });
    render(
      <SystemErrorCard
        payload={{
          provider: "codex",
          sign_in_command: "codex login",
          detail: "Codex CLI requires login.",
        }}
      />,
    );

    const copyBtn = screen.getByTestId("system-error-card-copy");
    fireEvent.click(copyBtn);

    expect(writeText).toHaveBeenCalledWith("codex login");
    await waitFor(() => expect(copyBtn.textContent).toBe("Copied"));
  });

  it("uses role=alert so screen readers announce the banner", () => {
    render(
      <SystemErrorCard
        payload={{
          provider: "opencode",
          sign_in_command: "opencode auth login",
        }}
      />,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it("renders a Retry button when onRetry is supplied", () => {
    const onRetry = vi.fn();
    render(
      <SystemErrorCard
        payload={{ provider: "claude-code" }}
        onRetry={onRetry}
      />,
    );
    const retry = screen.getByTestId("system-error-card-retry");
    fireEvent.click(retry);
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("renders gracefully when payload fields are missing", () => {
    render(<SystemErrorCard payload={{}} />);
    const card = screen.getByTestId("system-error-card");
    // Falls back to a generic provider label without crashing
    expect(card.textContent).toMatch(/Sign in to provider/);
    // No command means no copy button
    expect(screen.queryByTestId("system-error-card-copy")).toBeNull();
  });
});

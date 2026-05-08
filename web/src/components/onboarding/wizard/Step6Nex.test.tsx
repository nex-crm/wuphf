import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { NexStep } from "./Step6Nex";
import type { NexSignupStatus } from "./types";

interface Overrides {
  email?: string;
  status?: NexSignupStatus;
  error?: string;
}

function renderNex(
  overrides: Overrides = {},
  callbacks: Partial<{
    onChangeEmail: (v: string) => void;
    onSubmit: () => void;
    onNext: () => void;
    onBack: () => void;
  }> = {},
) {
  const props = {
    email: "",
    status: "open" as NexSignupStatus,
    error: "",
    onChangeEmail: callbacks.onChangeEmail ?? (() => {}),
    onSubmit: callbacks.onSubmit ?? (() => {}),
    onNext: callbacks.onNext ?? (() => {}),
    onBack: callbacks.onBack ?? (() => {}),
    ...overrides,
  };
  return render(<NexStep {...props} />);
}

describe("NexStep", () => {
  it("disables the primary CTA when email is empty in 'open' status", () => {
    renderNex({ status: "open", email: "" });
    expect(
      screen.getByRole("button", { name: /Register and continue/i }),
    ).toBeDisabled();
  });

  it("enables the primary CTA once a non-whitespace email is typed", () => {
    renderNex({ status: "open", email: "me@example.com" });
    expect(
      screen.getByRole("button", { name: /Register and continue/i }),
    ).toBeEnabled();
  });

  it("disables both CTAs while submitting", () => {
    renderNex({ status: "submitting", email: "me@example.com" });
    expect(screen.getByRole("button", { name: /Registering/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /^Skip$/ })).toBeDisabled();
  });

  it("'ok' state shows the inbox confirmation and a Continue CTA (no Skip)", () => {
    renderNex({ status: "ok", email: "me@example.com" });
    // Inbox confirmation message references the email.
    expect(
      screen.getByText(/Check your inbox at me@example.com/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Continue$/ })).toBeEnabled();
    expect(screen.queryByRole("button", { name: /^Skip$/ })).toBeNull();
  });

  it("'fallback' state surfaces the external register link and a Continue CTA", () => {
    renderNex({ status: "fallback", email: "" });
    const link = screen.getByRole("link", { name: /Open nex.ai\/register/i });
    expect(link).toHaveAttribute("href", "https://nex.ai/register");
    // The primary CTA in fallback advances; Register-and-continue is gone
    // because nex-cli isn't available to consume the email server-side.
    expect(screen.getByRole("button", { name: /^Continue$/ })).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: /Register and continue/i }),
    ).toBeNull();
  });

  it("Back button always invokes onBack", () => {
    const onBack = vi.fn();
    renderNex({ status: "open" }, { onBack });
    fireEvent.click(screen.getByRole("button", { name: /^Back$/ }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});

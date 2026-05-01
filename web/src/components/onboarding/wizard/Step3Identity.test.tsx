import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IdentityStep } from "./Step3Identity";

interface Overrides {
  company?: string;
  description?: string;
  priority?: string;
  nexEmail?: string;
  nexSignupStatus?: "hidden" | "open" | "submitting" | "ok" | "fallback";
  nexSignupError?: string;
}

function renderIdentity(
  overrides: Overrides = {},
  callbacks: Partial<{
    onChangeCompany: (v: string) => void;
    onChangeDescription: (v: string) => void;
    onChangePriority: (v: string) => void;
    onChangeNexEmail: (v: string) => void;
    onSubmitNexSignup: () => void;
    onOpenNexSignup: () => void;
    onNext: () => void;
    onBack: () => void;
  }> = {},
) {
  const props = {
    company: "",
    description: "",
    priority: "",
    nexEmail: "",
    nexSignupStatus: "hidden" as const,
    nexSignupError: "",
    onChangeCompany: callbacks.onChangeCompany ?? (() => {}),
    onChangeDescription: callbacks.onChangeDescription ?? (() => {}),
    onChangePriority: callbacks.onChangePriority ?? (() => {}),
    onChangeNexEmail: callbacks.onChangeNexEmail ?? (() => {}),
    onSubmitNexSignup: callbacks.onSubmitNexSignup ?? (() => {}),
    onOpenNexSignup: callbacks.onOpenNexSignup ?? (() => {}),
    onNext: callbacks.onNext ?? (() => {}),
    onBack: callbacks.onBack ?? (() => {}),
    ...overrides,
  };
  return render(<IdentityStep {...props} />);
}

describe("IdentityStep", () => {
  it("disables Continue when company or description are empty", () => {
    renderIdentity({ company: "", description: "" });
    const cta = screen.getByRole("button", { name: /Choose a blueprint/i });
    expect(cta).toBeDisabled();

    renderIdentity({ company: "Acme", description: "" });
    expect(
      screen.getAllByRole("button", { name: /Choose a blueprint/i })[1],
    ).toBeDisabled();
  });

  it("enables Continue once both company and description are non-empty", () => {
    renderIdentity({ company: "Acme", description: "We sell things" });
    expect(
      screen.getByRole("button", { name: /Choose a blueprint/i }),
    ).toBeEnabled();
  });

  it("treats whitespace-only inputs as empty (trim gate)", () => {
    renderIdentity({ company: "   ", description: "  " });
    expect(
      screen.getByRole("button", { name: /Choose a blueprint/i }),
    ).toBeDisabled();
  });

  it("typing into company / description fires the change handlers", () => {
    const onChangeCompany = vi.fn();
    const onChangeDescription = vi.fn();
    renderIdentity({}, { onChangeCompany, onChangeDescription });

    fireEvent.change(screen.getByLabelText(/Company or project name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/One-liner description/i), {
      target: { value: "We sell things" },
    });
    expect(onChangeCompany).toHaveBeenCalledWith("Acme");
    expect(onChangeDescription).toHaveBeenCalledWith("We sell things");
  });

  it("hides the NexSignupPanel by default and surfaces the trigger link", () => {
    const onOpenNexSignup = vi.fn();
    renderIdentity({ nexSignupStatus: "hidden" }, { onOpenNexSignup });
    const trigger = screen.getByRole("button", {
      name: /Don.+t have a Nex account/i,
    });
    fireEvent.click(trigger);
    expect(onOpenNexSignup).toHaveBeenCalledTimes(1);
    // Email input is mounted only when the panel is visible.
    expect(screen.queryByLabelText(/^Email$/i)).not.toBeInTheDocument();
  });

  it("renders the NexSignupPanel once status leaves 'hidden'", () => {
    renderIdentity({ nexSignupStatus: "open" });
    expect(screen.getByLabelText(/^Email$/i)).toBeInTheDocument();
  });
});

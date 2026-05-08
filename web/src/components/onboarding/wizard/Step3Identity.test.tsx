import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IdentityStep } from "./Step3Identity";

interface Overrides {
  company?: string;
  description?: string;
  priority?: string;
  website?: string;
  ownerName?: string;
  ownerRole?: string;
}

function renderIdentity(
  overrides: Overrides = {},
  callbacks: Partial<{
    onChangeCompany: (v: string) => void;
    onChangeDescription: (v: string) => void;
    onChangePriority: (v: string) => void;
    onChangeWebsite: (v: string) => void;
    onChangeOwnerName: (v: string) => void;
    onChangeOwnerRole: (v: string) => void;
    onNext: () => void;
    onBack: () => void;
  }> = {},
) {
  const props = {
    company: "",
    description: "",
    priority: "",
    website: "",
    ownerName: "",
    ownerRole: "",
    onChangeCompany: callbacks.onChangeCompany ?? (() => {}),
    onChangeDescription: callbacks.onChangeDescription ?? (() => {}),
    onChangePriority: callbacks.onChangePriority ?? (() => {}),
    onChangeWebsite: callbacks.onChangeWebsite ?? (() => {}),
    onChangeOwnerName: callbacks.onChangeOwnerName ?? (() => {}),
    onChangeOwnerRole: callbacks.onChangeOwnerRole ?? (() => {}),
    onNext: callbacks.onNext ?? (() => {}),
    onBack: callbacks.onBack ?? (() => {}),
    ...overrides,
  };
  const result = render(<IdentityStep {...props} />);
  return {
    ...result,
    rerenderWith: (newOverrides: Overrides) =>
      result.rerender(<IdentityStep {...props} {...newOverrides} />),
  };
}

describe("IdentityStep", () => {
  it("disables Continue when company or description are empty", () => {
    const { rerenderWith } = renderIdentity({ company: "", description: "" });
    const cta = screen.getByRole("button", { name: /Continue/i });
    expect(cta).toBeDisabled();

    rerenderWith({ company: "Acme", description: "" });
    expect(screen.getByRole("button", { name: /Continue/i })).toBeDisabled();
  });

  it("enables Continue once both company and description are non-empty", () => {
    renderIdentity({ company: "Acme", description: "We sell things" });
    expect(screen.getByRole("button", { name: /Continue/i })).toBeEnabled();
  });

  it("treats whitespace-only inputs as empty (trim gate)", () => {
    renderIdentity({ company: "   ", description: "  " });
    expect(screen.getByRole("button", { name: /Continue/i })).toBeDisabled();
  });

  it("typing into the identity fields fires the change handlers", () => {
    const onChangeCompany = vi.fn();
    const onChangeDescription = vi.fn();
    const onChangePriority = vi.fn();
    renderIdentity(
      {},
      { onChangeCompany, onChangeDescription, onChangePriority },
    );

    fireEvent.change(screen.getByLabelText(/Office name/i), {
      target: { value: "Acme" },
    });
    fireEvent.change(screen.getByLabelText(/Short description/i), {
      target: { value: "We sell things" },
    });
    fireEvent.change(screen.getByLabelText(/Top priority/i), {
      target: { value: "Win first customer" },
    });
    expect(onChangeCompany).toHaveBeenCalledWith("Acme");
    expect(onChangeDescription).toHaveBeenCalledWith("We sell things");
    expect(onChangePriority).toHaveBeenCalledWith("Win first customer");
  });

  it("typing into website and owner fields fires the change handlers", () => {
    const onChangeWebsite = vi.fn();
    const onChangeOwnerName = vi.fn();
    const onChangeOwnerRole = vi.fn();
    renderIdentity(
      { company: "Acme", description: "We sell things" },
      { onChangeWebsite, onChangeOwnerName, onChangeOwnerRole },
    );

    fireEvent.change(screen.getByLabelText(/Company website/i), {
      target: { value: "https://acme.com" },
    });
    fireEvent.change(screen.getByLabelText(/Your name/i), {
      target: { value: "Nazz Mohammad" },
    });
    fireEvent.change(screen.getByLabelText(/Your role/i), {
      target: { value: "Founder" },
    });
    expect(onChangeWebsite).toHaveBeenCalledWith("https://acme.com");
    expect(onChangeOwnerName).toHaveBeenCalledWith("Nazz Mohammad");
    expect(onChangeOwnerRole).toHaveBeenCalledWith("Founder");
  });
});

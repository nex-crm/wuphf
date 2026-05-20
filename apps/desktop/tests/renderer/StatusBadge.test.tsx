// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge } from "../../src/renderer/ui/StatusBadge.tsx";

describe("StatusBadge", () => {
  it("announces busy status and renders the tone", () => {
    render(<StatusBadge busy label="Starting" tone="pending" />);

    const badge = screen.getByRole("status");
    expect(badge).toHaveAttribute("aria-busy", "true");
    expect(badge).toHaveAttribute("data-tone", "pending");
    expect(badge).toHaveTextContent("Starting");
  });
});

// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Button } from "../../src/renderer/ui/Button.tsx";

describe("Button", () => {
  it("renders an accessible button with variant classes", () => {
    render(<Button variant="secondary">Retry</Button>);

    const button = screen.getByRole("button", { name: "Retry" });
    expect(button).toHaveAttribute("type", "button");
    expect(button.className).toContain("border-border");
  });
});

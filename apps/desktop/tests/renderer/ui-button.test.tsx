// @vitest-environment happy-dom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Button } from "../../src/renderer/ui/Button.tsx";

describe("Button", () => {
  it("renders an accessible button with variant classes", () => {
    render(<Button variant="secondary">Retry</Button>);

    const button = screen.getByRole("button", { name: "Retry" });
    expect(button).toHaveAttribute("type", "button");
    expect(button.className).toContain("border-border");
  });

  it("fires clicks when enabled and suppresses them when disabled", () => {
    const onClick = vi.fn<() => void>();
    const { rerender } = render(<Button onClick={onClick}>Run</Button>);

    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    expect(onClick).toHaveBeenCalledTimes(1);

    rerender(
      <Button disabled onClick={onClick}>
        Run
      </Button>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("renders non-default size and variant classes", () => {
    render(
      <Button size="sm" variant="ghost">
        More
      </Button>,
    );

    const button = screen.getByRole("button", { name: "More" });
    expect(button.className).toContain("h-8");
    expect(button.className).toContain("bg-transparent");
  });
});

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import CiteThisPagePanel from "./CiteThisPagePanel";

describe("<CiteThisPagePanel>", () => {
  it("renders the wikilink code block", () => {
    render(<CiteThisPagePanel slug="people/customer-x" />);
    expect(screen.getByText("[[people/customer-x]]")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /copy wikilink/i }),
    ).toBeInTheDocument();
  });

  it("writes the wikilink to the clipboard on copy", async () => {
    // Arrange — happy-dom's navigator.clipboard is read-only; swap the whole
    // navigator so we can observe the call.
    const writeText = vi.fn().mockResolvedValue(undefined);
    const originalNav = Object.getOwnPropertyDescriptor(
      globalThis,
      "navigator",
    );
    Object.defineProperty(globalThis, "navigator", {
      value: { clipboard: { writeText } },
      writable: true,
      configurable: true,
    });
    try {
      render(<CiteThisPagePanel slug="people/nazz" />);
      // Act — use fireEvent.click to avoid userEvent's clipboard interception.
      fireEvent.click(screen.getByRole("button", { name: /copy wikilink/i }));
      // Assert
      await waitFor(() =>
        expect(writeText).toHaveBeenCalledWith("[[people/nazz]]"),
      );
      expect(await screen.findByText("copied")).toBeInTheDocument();
    } finally {
      if (originalNav)
        Object.defineProperty(globalThis, "navigator", originalNav);
    }
  });
});

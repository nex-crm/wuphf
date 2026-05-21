// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Card } from "../../src/renderer/ui/Card.tsx";

describe("Card", () => {
  it("renders a labelled region with title and body", () => {
    render(
      <Card aria-label="Broker status" title="Broker">
        Ready
      </Card>,
    );

    expect(screen.getByRole("region", { name: "Broker status" })).toHaveTextContent(
      "Broker",
    );
    expect(screen.getByText("Ready")).toBeInTheDocument();
  });

  it("renders optional description without requiring a title", () => {
    render(
      <Card aria-label="Details" description="Loopback broker details">
        <span>Body</span>
      </Card>,
    );

    expect(screen.getByRole("region", { name: "Details" })).toHaveTextContent(
      "Loopback broker details",
    );
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });
});

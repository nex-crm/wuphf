import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import CategoriesFooter from "./CategoriesFooter";

describe("<CategoriesFooter>", () => {
  it("renders a labeled category link per slug pointing at the category page", () => {
    render(<CategoriesFooter tags={["companies", "people", "playbooks"]} />);
    expect(screen.getByText("Categories:")).toBeInTheDocument();
    const companies = screen.getByRole("link", { name: "Companies" });
    expect(companies).toBeInTheDocument();
    expect(companies).toHaveAttribute("href", "#/wiki/_category/companies");
    expect(screen.getByRole("link", { name: "People" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Playbooks" })).toBeInTheDocument();
  });

  it("fires onSelect with the slug instead of navigating", async () => {
    // Arrange
    const onSelect = vi.fn();
    const user = userEvent.setup();
    render(<CategoriesFooter tags={["companies"]} onSelect={onSelect} />);
    // Act
    await user.click(screen.getByRole("link", { name: "Companies" }));
    // Assert
    expect(onSelect).toHaveBeenCalledWith("companies");
  });

  it("renders nothing when empty", () => {
    const { container } = render(<CategoriesFooter tags={[]} />);
    expect(container.firstChild).toBeNull();
  });
});

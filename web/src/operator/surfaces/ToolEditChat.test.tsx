import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { getTool } from "../mock/data";
import { ToolEditChat } from "./ToolEditChat";

// Regression for the dropped-edit bug: apply() used to bump only local chat
// state, so the parent tool model never learned about the published version.
// The fix lifts the applied version up through onApply.
describe("ToolEditChat", () => {
  it("lifts the applied edit to the parent via onApply with the bumped version", () => {
    const tool = getTool("inbound-routing");
    if (!tool) throw new Error("fixture tool 'inbound-routing' missing");
    const onApply = vi.fn();

    const { getByLabelText, getByRole } = render(
      <ToolEditChat tool={tool} onClose={() => {}} onApply={onApply} />,
    );

    fireEvent.change(getByLabelText("Describe a change to this tool"), {
      target: { value: "lower the threshold to 65" },
    });
    fireEvent.click(getByRole("button", { name: "Send" }));

    // The AI proposes a new version; applying it must notify the parent.
    fireEvent.click(getByRole("button", { name: "Apply & publish" }));

    expect(onApply).toHaveBeenCalledTimes(1);
    const [[applied]] = onApply.mock.calls;
    expect(applied.version).toBe(tool.version + 1);
    expect(applied.author).toBe("you");
    // The published version carries the concrete change, not a stock label.
    expect(applied.label).toContain("65");
    expect(applied.note.length).toBeGreaterThan(0);
  });
});

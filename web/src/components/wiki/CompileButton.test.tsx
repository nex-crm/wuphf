import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CompileResult } from "../../api/sources";
import CompileButton from "./CompileButton";

const RESULT: CompileResult = {
  pages_written: 4,
  concepts: 6,
  sources_read: 11,
};

describe("<CompileButton>", () => {
  it("runs compile and surfaces the tally", async () => {
    const compile = vi.fn(async () => RESULT);
    const onCompiled = vi.fn();
    render(<CompileButton compile={compile} onCompiled={onCompiled} />);
    fireEvent.click(screen.getByRole("button", { name: "Compile wiki" }));
    expect(await screen.findByText(/4 pages written/)).toBeInTheDocument();
    expect(screen.getByText(/6 concepts/)).toBeInTheDocument();
    expect(onCompiled).toHaveBeenCalledWith(RESULT);
  });

  it("renders warnings when the run reports them", async () => {
    render(
      <CompileButton
        compile={async () => ({
          ...RESULT,
          errors: ["source note-1: empty"],
        })}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(await screen.findByText("1 warning")).toBeInTheDocument();
  });

  it("surfaces an error when compile fails", async () => {
    render(
      <CompileButton
        compile={async () => {
          throw new Error("wiki backend is not active");
        }}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(
      await screen.findByText("wiki backend is not active"),
    ).toBeInTheDocument();
  });
});

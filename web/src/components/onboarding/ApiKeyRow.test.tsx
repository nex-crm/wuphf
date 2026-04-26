import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiKeyRow } from "./Wizard";

const FIELD = {
  key: "ANTHROPIC_API_KEY",
  label: "Anthropic",
  hint: "Powers Claude-based agents",
  cliLoginCmd: "claude login",
} as const;

describe("<ApiKeyRow>", () => {
  // The wizard's API-keys panel must default to "Use CLI login" so a
  // user with `claude login` already in their session doesn't see a
  // wall of empty password fields. Reveal-on-demand keeps the
  // password input out of the DOM until the user explicitly clicks
  // "Use API key".
  it("defaults to CLI login and hides the password input", () => {
    render(<ApiKeyRow field={FIELD} value="" onChange={vi.fn()} />);
    expect(screen.getByTestId("api-key-cli-ANTHROPIC_API_KEY")).toHaveClass(
      /selected/,
    );
    expect(screen.getByText(/claude login/)).toBeInTheDocument();
    expect(
      screen.queryByTestId("api-key-input-ANTHROPIC_API_KEY"),
    ).not.toBeInTheDocument();
  });

  it("clicking 'Use API key' reveals the input + flips the selected state", () => {
    render(<ApiKeyRow field={FIELD} value="" onChange={vi.fn()} />);
    fireEvent.click(screen.getByTestId("api-key-paste-ANTHROPIC_API_KEY"));
    expect(screen.getByTestId("api-key-paste-ANTHROPIC_API_KEY")).toHaveClass(
      /selected/,
    );
    expect(
      screen.getByTestId("api-key-input-ANTHROPIC_API_KEY"),
    ).toBeInTheDocument();
  });

  it("typing in the input fires onChange with the value", () => {
    const onChange = vi.fn();
    render(<ApiKeyRow field={FIELD} value="" onChange={onChange} />);
    fireEvent.click(screen.getByTestId("api-key-paste-ANTHROPIC_API_KEY"));
    fireEvent.change(screen.getByTestId("api-key-input-ANTHROPIC_API_KEY"), {
      target: { value: "sk-ant-test" },
    });
    expect(onChange).toHaveBeenCalledWith("sk-ant-test");
  });

  // Reviewer caught: clicking back to "Use CLI login" must clear the
  // pasted value so the wizard doesn't ship a key the user explicitly
  // toggled away from. Without this, the password input is hidden but
  // still in form state — silent surprise.
  it("clicking 'Use CLI login' after a paste clears the value", () => {
    const onChange = vi.fn();
    render(
      <ApiKeyRow field={FIELD} value="sk-ant-leaked" onChange={onChange} />,
    );
    fireEvent.click(screen.getByTestId("api-key-cli-ANTHROPIC_API_KEY"));
    expect(onChange).toHaveBeenCalledWith("");
  });

  // When a parent re-renders with a non-empty value (the user pasted
  // earlier and the form is rehydrating), the input shows pre-revealed
  // so the user can edit without re-clicking through.
  it("renders the input pre-revealed when value is non-empty on first mount", () => {
    render(<ApiKeyRow field={FIELD} value="sk-existing" onChange={vi.fn()} />);
    expect(screen.getByTestId("api-key-input-ANTHROPIC_API_KEY")).toHaveValue(
      "sk-existing",
    );
    expect(screen.getByTestId("api-key-paste-ANTHROPIC_API_KEY")).toHaveClass(
      /selected/,
    );
  });
});

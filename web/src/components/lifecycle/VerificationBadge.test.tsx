import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { TaskVerification, TaskVerificationResult } from "../../api/tasks";
import {
  resolveVerificationBadgeState,
  VerificationBadge,
} from "./VerificationBadge";

const COMMAND_CHECK: TaskVerification = {
  kind: "command",
  spec: "bash scripts/test-go.sh ./internal/team",
  required: true,
};

const PASS_RESULT: TaskVerificationResult = {
  pass: true,
  kind: "command",
  detail: "PASS\nok wuphf/internal/team",
  checked_at: "2026-06-10T09:14:00Z",
};

const FAIL_RESULT: TaskVerificationResult = {
  pass: false,
  kind: "command",
  detail: "FAIL: TestTaskVerificationGate",
  checked_at: "2026-06-10T09:14:00Z",
};

describe("resolveVerificationBadgeState", () => {
  it("maps the four wire shapes to the four states", () => {
    expect(resolveVerificationBadgeState(COMMAND_CHECK, PASS_RESULT)).toBe(
      "verified",
    );
    expect(resolveVerificationBadgeState(COMMAND_CHECK, FAIL_RESULT)).toBe(
      "failing",
    );
    expect(resolveVerificationBadgeState(COMMAND_CHECK, undefined)).toBe(
      "pending",
    );
    expect(resolveVerificationBadgeState(undefined, undefined)).toBe(
      "unverified",
    );
  });

  it("treats kind 'none' as unverified (U1.1: none renders as UNVERIFIED)", () => {
    expect(resolveVerificationBadgeState({ kind: "none" }, undefined)).toBe(
      "unverified",
    );
  });

  it("lets a stamped result win even when the spec is kind 'none'", () => {
    expect(resolveVerificationBadgeState({ kind: "none" }, PASS_RESULT)).toBe(
      "verified",
    );
  });
});

describe("<VerificationBadge>", () => {
  it("renders Verified and expands to show the proof", () => {
    render(
      <VerificationBadge verification={COMMAND_CHECK} result={PASS_RESULT} />,
    );
    const badge = screen.getByTestId("verification-badge");
    expect(badge).toHaveTextContent("Verified");
    expect(badge).toHaveAttribute("data-verification-state", "verified");
    expect(badge).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(badge);
    const detail = screen.getByTestId("verification-badge-detail");
    expect(detail).toHaveTextContent("command");
    expect(detail).toHaveTextContent("PASS");
    expect(detail).toHaveTextContent("Proof");
  });

  it("renders Check failing with the failure detail accessible", () => {
    render(
      <VerificationBadge verification={COMMAND_CHECK} result={FAIL_RESULT} />,
    );
    const badge = screen.getByTestId("verification-badge");
    expect(badge).toHaveTextContent("Check failing");
    expect(badge).toHaveAttribute("data-verification-state", "failing");

    fireEvent.click(badge);
    const detail = screen.getByTestId("verification-badge-detail");
    expect(detail).toHaveTextContent("Failure detail");
    expect(detail).toHaveTextContent("FAIL: TestTaskVerificationGate");
  });

  it("renders Check pending with the definition-of-done spec", () => {
    render(<VerificationBadge verification={COMMAND_CHECK} />);
    const badge = screen.getByTestId("verification-badge");
    expect(badge).toHaveTextContent("Check pending");
    expect(badge).toHaveAttribute("data-verification-state", "pending");

    fireEvent.click(badge);
    expect(screen.getByTestId("verification-badge-detail")).toHaveTextContent(
      "bash scripts/test-go.sh ./internal/team",
    );
  });

  it("renders a quiet non-interactive Unverified pill when no check exists", () => {
    render(<VerificationBadge />);
    const badge = screen.getByTestId("verification-badge");
    expect(badge).toHaveTextContent("Unverified");
    expect(badge).toHaveAttribute("data-verification-state", "unverified");
    expect(badge.tagName).toBe("SPAN");
    expect(
      screen.queryByTestId("verification-badge-detail"),
    ).not.toBeInTheDocument();
  });

  it("collapses the detail panel on a second click", () => {
    render(
      <VerificationBadge verification={COMMAND_CHECK} result={PASS_RESULT} />,
    );
    const badge = screen.getByTestId("verification-badge");
    fireEvent.click(badge);
    expect(screen.getByTestId("verification-badge-detail")).toBeInTheDocument();
    fireEvent.click(badge);
    expect(
      screen.queryByTestId("verification-badge-detail"),
    ).not.toBeInTheDocument();
  });
});

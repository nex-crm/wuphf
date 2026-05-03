import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../../api/client", () => ({
  getLocalProvidersStatus: vi.fn().mockResolvedValue([]),
}));

import { SetupStep } from "./Step5Setup";
import type { PrereqResult } from "./types";

interface Overrides {
  prereqs?: PrereqResult[];
  prereqsLoading?: boolean;
  prereqsError?: string;
  runtimePriority?: string[];
  apiKeys?: Record<string, string>;
  localProvider?: string;
  onSelectLocalProvider?: (kind: string) => void;
}

function setupProps(overrides: Overrides = {}) {
  const {
    prereqs = [],
    prereqsLoading = false,
    prereqsError = "",
    runtimePriority = [],
    apiKeys = {},
    localProvider = "",
    onSelectLocalProvider = () => {},
  } = overrides;
  return {
    prereqStatus: {
      items: prereqs,
      loading: prereqsLoading,
      error: prereqsError,
    },
    runtimeSelection: {
      priority: runtimePriority,
      onToggle: () => {},
      onReorder: () => {},
    },
    apiKeyState: {
      values: apiKeys,
      onChange: () => {},
    },
    localLLMState: {
      provider: localProvider,
      onSelectProvider: onSelectLocalProvider,
    },
    onNext: () => {},
    onBack: () => {},
  };
}

function renderSetup(overrides: Overrides = {}) {
  return render(<SetupStep {...setupProps(overrides)} />);
}

// Step 5's CTA text is `ONBOARDING_COPY.step2_cta` ("Ready"). Match
// from the start of the accessible name so tile buttons that happen to
// contain the word elsewhere don't false-match.
const ctaName = /^Ready/;

describe("SetupStep — canContinue gate", () => {
  it("disables Continue when no runtime, no API key, no local provider", () => {
    renderSetup();
    expect(screen.getByRole("button", { name: ctaName })).toBeDisabled();
  });

  it("enables Continue when an installed runtime is in priority", () => {
    renderSetup({
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: ["Claude Code"],
    });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });

  it("keeps providerless runtimes from satisfying the gate when prereqs fail", () => {
    renderSetup({
      prereqsError: "broker unavailable",
      runtimePriority: ["Cursor"],
    });
    expect(screen.getByRole("button", { name: ctaName })).toBeDisabled();
  });

  it("enables Continue when any API key is non-empty", () => {
    renderSetup({ apiKeys: { ANTHROPIC_API_KEY: "sk-x" } });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });

  it("enables Continue when a local LLM provider is picked", () => {
    renderSetup({ localProvider: "ollama" });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });

});

describe("SetupStep — surfaces", () => {
  it("renders the prereqs error banner when detection fails", () => {
    renderSetup({ prereqsError: "exec: command not found" });
    expect(screen.getByTestId("prereqs-error-banner")).toBeInTheDocument();
  });

  it("toggling the local-LLM tile off clears localProvider via callback", () => {
    const onSelectLocalProvider = vi.fn();
    renderSetup({ localProvider: "ollama", onSelectLocalProvider });
    fireEvent.click(screen.getByTestId("onboarding-local-llm-toggle"));
    expect(onSelectLocalProvider).toHaveBeenCalledWith("");
  });

  it("closes local-LLM mode when parent state clears localProvider", () => {
    const { rerender } = renderSetup({ localProvider: "ollama" });
    expect(screen.getByTestId("onboarding-local-llm-toggle")).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    rerender(<SetupStep {...setupProps({ localProvider: "" })} />);

    expect(screen.getByTestId("onboarding-local-llm-toggle")).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.queryByTestId("onboarding-local-llm-picker")).toBeNull();
  });
});

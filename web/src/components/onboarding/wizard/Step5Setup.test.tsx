import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SetupStep } from "./Step5Setup";
import type { MemoryBackend, PrereqResult } from "./types";

interface Overrides {
  prereqs?: PrereqResult[];
  prereqsLoading?: boolean;
  prereqsError?: string;
  runtimePriority?: string[];
  apiKeys?: Record<string, string>;
  memoryBackend?: MemoryBackend;
  nexApiKey?: string;
  gbrainOpenAIKey?: string;
  gbrainAnthropicKey?: string;
  localProvider?: string;
}

function renderSetup(overrides: Overrides = {}) {
  const props = {
    prereqs: [] as PrereqResult[],
    prereqsLoading: false,
    prereqsError: "",
    runtimePriority: [] as string[],
    onToggleRuntime: () => {},
    onReorderRuntime: () => {},
    apiKeys: {} as Record<string, string>,
    onChangeApiKey: () => {},
    memoryBackend: "markdown" as MemoryBackend,
    onChangeMemoryBackend: () => {},
    nexApiKey: "",
    onChangeNexApiKey: () => {},
    gbrainOpenAIKey: "",
    onChangeGBrainOpenAIKey: () => {},
    gbrainAnthropicKey: "",
    onChangeGBrainAnthropicKey: () => {},
    localProvider: "",
    onSelectLocalProvider: () => {},
    onNext: () => {},
    onBack: () => {},
    ...overrides,
  };
  return render(<SetupStep {...props} />);
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

  it("enables Continue when any API key is non-empty", () => {
    renderSetup({ apiKeys: { ANTHROPIC_API_KEY: "sk-x" } });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });

  it("enables Continue when a local LLM provider is picked", () => {
    renderSetup({ localProvider: "ollama" });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });

  it("disables Continue when GBrain is selected without an OpenAI key — even with an installed runtime", () => {
    renderSetup({
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: ["Claude Code"],
      memoryBackend: "gbrain",
      gbrainOpenAIKey: "",
    });
    expect(screen.getByRole("button", { name: ctaName })).toBeDisabled();
  });

  it("re-enables Continue once the GBrain OpenAI key is filled in", () => {
    renderSetup({
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: ["Claude Code"],
      memoryBackend: "gbrain",
      gbrainOpenAIKey: "sk-openai",
    });
    expect(screen.getByRole("button", { name: ctaName })).toBeEnabled();
  });
});

describe("SetupStep — surfaces", () => {
  it("renders the prereqs error banner when detection fails", () => {
    renderSetup({ prereqsError: "exec: command not found" });
    expect(screen.getByTestId("prereqs-error-banner")).toBeInTheDocument();
  });

  it("hides the Nex API key panel unless memoryBackend === 'nex'", () => {
    const { rerender } = renderSetup({ memoryBackend: "markdown" });
    expect(
      screen.queryByTestId("wizard-nex-api-key-panel"),
    ).not.toBeInTheDocument();

    rerender(
      <SetupStep
        prereqs={[]}
        prereqsLoading={false}
        prereqsError=""
        runtimePriority={[]}
        onToggleRuntime={() => {}}
        onReorderRuntime={() => {}}
        apiKeys={{}}
        onChangeApiKey={() => {}}
        memoryBackend="nex"
        onChangeMemoryBackend={() => {}}
        nexApiKey=""
        onChangeNexApiKey={() => {}}
        gbrainOpenAIKey=""
        onChangeGBrainOpenAIKey={() => {}}
        gbrainAnthropicKey=""
        onChangeGBrainAnthropicKey={() => {}}
        localProvider=""
        onSelectLocalProvider={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByTestId("wizard-nex-api-key-panel")).toBeInTheDocument();
  });

  it("toggling the local-LLM tile off clears localProvider via callback", () => {
    const onSelectLocalProvider = vi.fn();
    render(
      <SetupStep
        prereqs={[]}
        prereqsLoading={false}
        prereqsError=""
        runtimePriority={[]}
        onToggleRuntime={() => {}}
        onReorderRuntime={() => {}}
        apiKeys={{}}
        onChangeApiKey={() => {}}
        memoryBackend="markdown"
        onChangeMemoryBackend={() => {}}
        nexApiKey=""
        onChangeNexApiKey={() => {}}
        gbrainOpenAIKey=""
        onChangeGBrainOpenAIKey={() => {}}
        gbrainAnthropicKey=""
        onChangeGBrainAnthropicKey={() => {}}
        localProvider="ollama"
        onSelectLocalProvider={onSelectLocalProvider}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    fireEvent.click(screen.getByTestId("onboarding-local-llm-toggle"));
    expect(onSelectLocalProvider).toHaveBeenCalledWith("");
  });
});

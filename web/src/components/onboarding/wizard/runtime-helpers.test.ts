import { describe, expect, it } from "vitest";

import {
  localProviderKindFromRuntimePriority,
  runtimeLabelsFromProviderConfig,
} from "./runtime-helpers";

describe("runtimeLabelsFromProviderConfig", () => {
  it("prefers an explicit provider before a saved fallback chain", () => {
    expect(
      runtimeLabelsFromProviderConfig({
        llm_provider: "codex",
        llm_provider_configured: true,
        llm_provider_priority: ["claude-code", "codex"],
      }),
    ).toEqual(["Codex", "Claude Code"]);
  });

  it("ignores the install default when no provider was explicitly configured", () => {
    expect(
      runtimeLabelsFromProviderConfig({
        llm_provider: "claude-code",
        llm_provider_configured: false,
      }),
    ).toEqual([]);
  });

  it("maps local providers and removes duplicates", () => {
    expect(
      runtimeLabelsFromProviderConfig({
        llm_provider_priority: ["ollama", "codex", "ollama", ""],
      }),
    ).toEqual(["Ollama", "Codex"]);
  });
});

describe("localProviderKindFromRuntimePriority", () => {
  it("uses the first local provider in runtime priority order", () => {
    expect(
      localProviderKindFromRuntimePriority(["Exo", "Codex", "Ollama"]),
    ).toBe("exo");
  });

  it("returns null when the priority contains no local provider", () => {
    expect(localProviderKindFromRuntimePriority(["Codex", "Claude Code"])).toBe(
      null,
    );
  });
});

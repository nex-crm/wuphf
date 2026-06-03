import { describe, expect, it } from "vitest";

import {
  DEFAULT_EFFORT_VALUE,
  effortLevelsForKind,
  effortOptionsForKind,
  normalizeEffortForKind,
  runtimeSupportsEffort,
} from "./effortCatalog";

describe("effortCatalog (model-specific reasoning effort)", () => {
  it("offers claude's --effort levels for claude-code", () => {
    expect(effortLevelsForKind("claude-code")).toEqual([
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
    ]);
  });

  it("offers codex's model_reasoning_effort levels for codex", () => {
    expect(effortLevelsForKind("codex")).toEqual([
      "minimal",
      "low",
      "medium",
      "high",
      "xhigh",
    ]);
  });

  it("offers no effort levels for opencode/local/empty runtimes", () => {
    for (const kind of ["opencode", "mlx-lm", "ollama", "exo", ""] as const) {
      expect(effortLevelsForKind(kind)).toEqual([]);
      expect(runtimeSupportsEffort(kind)).toBe(false);
    }
  });

  it("always begins the options list with the default entry", () => {
    const options = effortOptionsForKind("claude-code");
    expect(options[0]).toEqual({
      value: DEFAULT_EFFORT_VALUE,
      label: "Default effort",
    });
    // "Extra high" is the friendly label for xhigh.
    expect(options.map((o) => o.label)).toContain("Extra high");
  });

  it("clamps a now-invalid level to default when the runtime changes", () => {
    // "max" is claude-only — invalid on codex, so it falls back to default.
    expect(normalizeEffortForKind("codex", "max")).toBe(DEFAULT_EFFORT_VALUE);
    // "minimal" is codex-only — invalid on claude.
    expect(normalizeEffortForKind("claude-code", "minimal")).toBe(
      DEFAULT_EFFORT_VALUE,
    );
    // A shared level survives the switch.
    expect(normalizeEffortForKind("codex", "high")).toBe("high");
    // Case + whitespace are normalised.
    expect(normalizeEffortForKind("claude-code", " HIGH ")).toBe("high");
    // Local runtimes never carry effort.
    expect(normalizeEffortForKind("ollama", "high")).toBe(DEFAULT_EFFORT_VALUE);
  });
});

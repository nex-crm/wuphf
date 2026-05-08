import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../../api/client", () => ({
  getLocalProvidersStatus: vi.fn().mockResolvedValue([]),
}));

import type { PackPreviewRequirement } from "./packPreview";
import { PackRequirementsPanel, SetupStep } from "./Step5Setup";
import type { PrereqResult } from "./types";

interface Overrides {
  prereqs?: PrereqResult[];
  prereqsLoading?: boolean;
  prereqsError?: string;
  runtimePriority?: string[];
  apiKeys?: Record<string, string>;
  localProvider?: string;
  packRequirements?: PackPreviewRequirement[];
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
    packRequirements = [],
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
    packRequirements,
    onNext: () => {},
    onBack: () => {},
  };
}

function renderSetup(overrides: Overrides = {}) {
  return render(<SetupStep {...setupProps(overrides)} />);
}

// Step 5's CTA text is `ONBOARDING_COPY.step2_cta` ("Continue"). Match
// from the start of the accessible name so tile buttons that happen to
// contain the word elsewhere don't false-match.
const ctaName = /^Continue/;

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

describe("SetupStep — pack requirements panel hidden without requirements", () => {
  it("does not render requirements panel when packRequirements is empty", () => {
    renderSetup({ packRequirements: [] });
    expect(
      screen.queryByTestId("pack-requirements-panel"),
    ).not.toBeInTheDocument();
  });
});

// ─── PackRequirementsPanel unit tests ───────────────────────────────────────

function renderReqPanel(
  requirements: PackPreviewRequirement[],
  opts: {
    prereqs?: PrereqResult[];
    prereqsError?: string;
    runtimePriority?: string[];
    apiKeys?: Record<string, string>;
  } = {},
) {
  return render(
    <PackRequirementsPanel
      requirements={requirements}
      prereqs={opts.prereqs ?? []}
      prereqsError={opts.prereqsError ?? ""}
      runtimePriority={opts.runtimePriority ?? []}
      apiKeys={opts.apiKeys ?? {}}
    />,
  );
}

describe("PackRequirementsPanel — empty state", () => {
  it("renders nothing when requirements array is empty", () => {
    const { container } = renderReqPanel([]);
    expect(container.firstChild).toBeNull();
  });
});

describe("PackRequirementsPanel — required vs optional labels", () => {
  const requirements: PackPreviewRequirement[] = [
    { kind: "runtime", name: "Claude Code", required: true },
    { kind: "api-key", name: "Anthropic", required: false },
  ];

  it("shows required badge for required items", () => {
    renderReqPanel(requirements);
    expect(screen.getByTestId("pack-req-badge-Claude Code")).toHaveTextContent(
      "required",
    );
  });

  it("shows optional badge for optional items", () => {
    renderReqPanel(requirements);
    expect(screen.getByTestId("pack-req-badge-Anthropic")).toHaveTextContent(
      "optional",
    );
  });

  it("renders a row for every requirement", () => {
    renderReqPanel(requirements);
    expect(screen.getByTestId("pack-req-row-Claude Code")).toBeInTheDocument();
    expect(screen.getByTestId("pack-req-row-Anthropic")).toBeInTheDocument();
  });
});

describe("PackRequirementsPanel — runtime readiness dots", () => {
  const req: PackPreviewRequirement = {
    kind: "runtime",
    name: "Claude Code",
    required: true,
  };

  it("shows ok dot when runtime is in priority and installed", () => {
    renderReqPanel([req], {
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: ["Claude Code"],
    });
    expect(screen.getByTestId("pack-req-row-Claude Code")).toHaveAttribute(
      "data-readiness",
      "ok",
    );
  });

  it("shows missing dot when runtime is not in priority", () => {
    renderReqPanel([req], {
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: [],
    });
    expect(screen.getByTestId("pack-req-row-Claude Code")).toHaveAttribute(
      "data-readiness",
      "missing",
    );
  });

  it("shows missing dot when runtime is in priority but not installed", () => {
    renderReqPanel([req], {
      prereqs: [{ name: "claude", required: true, found: false }],
      runtimePriority: ["Claude Code"],
    });
    expect(screen.getByTestId("pack-req-row-Claude Code")).toHaveAttribute(
      "data-readiness",
      "missing",
    );
  });

  it("shows ok dot when prereqsError truthy and runtime is selected (detection bypass)", () => {
    renderReqPanel([req], {
      prereqs: [],
      prereqsError: "detection failed",
      runtimePriority: ["Claude Code"],
    });
    expect(screen.getByTestId("pack-req-row-Claude Code")).toHaveAttribute(
      "data-readiness",
      "ok",
    );
  });
});

describe("PackRequirementsPanel — Provider Doctor link", () => {
  it("shows Provider Doctor link when a required runtime is missing", () => {
    renderReqPanel([{ kind: "runtime", name: "Claude Code", required: true }], {
      prereqs: [],
      runtimePriority: [],
    });
    expect(screen.getByTestId("pack-req-doctor-hint")).toBeInTheDocument();
    expect(screen.getByTestId("pack-req-doctor-link")).toBeInTheDocument();
  });

  it("does not show Provider Doctor link when all required runtimes are ready", () => {
    renderReqPanel([{ kind: "runtime", name: "Claude Code", required: true }], {
      prereqs: [{ name: "claude", required: true, found: true }],
      runtimePriority: ["Claude Code"],
    });
    expect(
      screen.queryByTestId("pack-req-doctor-hint"),
    ).not.toBeInTheDocument();
  });

  it("does not show Provider Doctor link for optional missing runtimes", () => {
    renderReqPanel(
      [{ kind: "runtime", name: "Claude Code", required: false }],
      { prereqs: [], runtimePriority: [] },
    );
    expect(
      screen.queryByTestId("pack-req-doctor-hint"),
    ).not.toBeInTheDocument();
  });
});

describe("PackRequirementsPanel — no API key values in UI", () => {
  it("shows api-key requirement name without revealing the key value", () => {
    renderReqPanel([{ kind: "api-key", name: "Anthropic", required: true }], {
      apiKeys: { ANTHROPIC_API_KEY: "sk-super-secret" },
    });
    // The name label is visible
    expect(screen.getByTestId("pack-req-row-Anthropic")).toBeInTheDocument();
    // The secret value must not appear anywhere in the DOM
    expect(screen.queryByText("sk-super-secret")).not.toBeInTheDocument();
  });
});

describe("PackRequirementsPanel — api-key readiness", () => {
  it("shows ok dot when the matching api key is filled", () => {
    renderReqPanel([{ kind: "api-key", name: "Anthropic", required: true }], {
      apiKeys: { ANTHROPIC_API_KEY: "sk-x" },
    });
    expect(screen.getByTestId("pack-req-row-Anthropic")).toHaveAttribute(
      "data-readiness",
      "ok",
    );
  });

  it("shows missing dot when the matching api key is empty", () => {
    renderReqPanel([{ kind: "api-key", name: "Anthropic", required: true }], {
      apiKeys: {},
    });
    expect(screen.getByTestId("pack-req-row-Anthropic")).toHaveAttribute(
      "data-readiness",
      "missing",
    );
  });
});

describe("PackRequirementsPanel — local-tool and unknown kind", () => {
  it("shows unknown readiness for local-tool requirements", () => {
    renderReqPanel([{ kind: "local-tool", name: "ffmpeg", required: true }]);
    expect(screen.getByTestId("pack-req-row-ffmpeg")).toHaveAttribute(
      "data-readiness",
      "unknown",
    );
  });

  it("shows unknown readiness for unrecognized kind (cast as unknown kind)", () => {
    // Cast to exercise the fallback branch in requirementReadiness.
    renderReqPanel([
      {
        kind: "local-tool" as const,
        name: "Google Drive",
        required: false,
      },
    ]);
    expect(screen.getByTestId("pack-req-row-Google Drive")).toHaveAttribute(
      "data-readiness",
      "unknown",
    );
  });
});

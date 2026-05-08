import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OutcomeSummary } from "./OutcomeSummary";
import type { BlueprintAgent, BlueprintTemplate } from "./types";

// ── Fixtures ────────────────────────────────────────────────────────────

const AGENTS: BlueprintAgent[] = [
  { slug: "ceo", name: "CEO", role: "lead", checked: true, built_in: true },
  { slug: "engineer", name: "Engineer", role: "engineering", checked: true },
  { slug: "designer", name: "Designer", role: "design", checked: false },
];

const BLUEPRINTS: BlueprintTemplate[] = [
  {
    id: "saas",
    name: "SaaS Startup",
    description: "Standard SaaS blueprint",
  },
];

function renderOutcome(
  overrides: Partial<{
    agents: BlueprintAgent[];
    selectedBlueprint: string | null;
    blueprints: BlueprintTemplate[];
    primaryRuntime: string;
    taskText: string;
    taskSkipped: boolean;
    apiKeyCount: number;
    onEnter: () => void;
  }> = {},
) {
  return render(
    <OutcomeSummary
      agents={overrides.agents ?? AGENTS}
      selectedBlueprint={
        overrides.selectedBlueprint !== undefined
          ? overrides.selectedBlueprint
          : "saas"
      }
      blueprints={overrides.blueprints ?? BLUEPRINTS}
      primaryRuntime={overrides.primaryRuntime ?? "Claude Code"}
      taskText={overrides.taskText ?? "ship the MVP"}
      taskSkipped={overrides.taskSkipped ?? false}
      apiKeyCount={overrides.apiKeyCount ?? 2}
      onEnter={overrides.onEnter ?? (() => {})}
    />,
  );
}

// ── Tests ────────────────────────────────────────────────────────────────

describe("OutcomeSummary", () => {
  describe("agents panel", () => {
    it("renders only checked agents", () => {
      renderOutcome();
      const list = screen.getByTestId("outcome-agents");
      // CEO and Engineer are checked; Designer is not.
      expect(list).toHaveTextContent("CEO");
      expect(list).toHaveTextContent("Engineer");
      expect(list).not.toHaveTextContent("Designer");
    });

    it("shows @slug for each agent", () => {
      renderOutcome();
      const list = screen.getByTestId("outcome-agents");
      expect(list).toHaveTextContent("@ceo");
      expect(list).toHaveTextContent("@engineer");
    });

    it("marks the built-in lead agent with a lead badge", () => {
      const { container } = renderOutcome();
      const badges = container.querySelectorAll(".wiz-team-lead-badge");
      expect(badges).toHaveLength(1);
    });

    it("shows agent count in panel title", () => {
      renderOutcome();
      // 2 checked agents
      expect(screen.getByTestId("outcome-summary")).toHaveTextContent(
        "Team created (2 agents)",
      );
    });

    it("handles singular agent count", () => {
      renderOutcome({
        agents: [
          {
            slug: "ceo",
            name: "CEO",
            role: "lead",
            checked: true,
            built_in: true,
          },
        ],
      });
      expect(screen.getByTestId("outcome-summary")).toHaveTextContent(
        "Team created (1 agent)",
      );
    });

    it("shows fallback message when no agents are checked", () => {
      renderOutcome({
        agents: [
          {
            slug: "ceo",
            name: "CEO",
            role: "lead",
            checked: false,
          },
        ],
      });
      expect(screen.queryByTestId("outcome-agents")).toBeNull();
      expect(screen.getByTestId("outcome-summary")).toHaveTextContent(
        "No agents selected",
      );
    });
  });

  describe("setup panel", () => {
    it("shows the blueprint name resolved from id", () => {
      renderOutcome();
      expect(screen.getByTestId("outcome-blueprint")).toHaveTextContent(
        "SaaS Startup",
      );
    });

    it("shows 'Start from scratch' when selectedBlueprint is null", () => {
      renderOutcome({ selectedBlueprint: null });
      expect(screen.getByTestId("outcome-blueprint")).toHaveTextContent(
        "Start from scratch",
      );
    });

    it("falls back to blueprint id when name is not in list", () => {
      renderOutcome({ selectedBlueprint: "unknown-bp", blueprints: [] });
      expect(screen.getByTestId("outcome-blueprint")).toHaveTextContent(
        "unknown-bp",
      );
    });

    it("shows the primary runtime", () => {
      renderOutcome({ primaryRuntime: "Codex" });
      expect(screen.getByTestId("outcome-runtime")).toHaveTextContent("Codex");
    });

    it("shows 'Not selected' when runtime is empty", () => {
      renderOutcome({ primaryRuntime: "" });
      expect(screen.getByTestId("outcome-runtime")).toHaveTextContent(
        "Not selected",
      );
    });

    it("shows #general channel", () => {
      renderOutcome();
      expect(screen.getByTestId("outcome-channels")).toHaveTextContent(
        "#general",
      );
    });

    it("shows wiki scaffolding row", () => {
      renderOutcome();
      expect(screen.getByTestId("outcome-wiki")).toHaveTextContent(
        "Git-native team wiki",
      );
    });

    it("shows api key count when non-zero", () => {
      renderOutcome({ apiKeyCount: 3 });
      expect(screen.getByTestId("outcome-api-keys")).toHaveTextContent(
        "3 keys saved",
      );
    });

    it("hides api key row when count is zero", () => {
      renderOutcome({ apiKeyCount: 0 });
      expect(screen.queryByTestId("outcome-api-keys")).toBeNull();
    });

    it("shows singular key label", () => {
      renderOutcome({ apiKeyCount: 1 });
      expect(screen.getByTestId("outcome-api-keys")).toHaveTextContent(
        "1 key saved",
      );
    });

    it("shows task text when not skipped", () => {
      renderOutcome({ taskText: "ship the MVP", taskSkipped: false });
      expect(screen.getByTestId("outcome-task")).toHaveTextContent(
        "ship the MVP",
      );
    });

    it("shows skipped message when taskSkipped is true", () => {
      renderOutcome({ taskText: "", taskSkipped: true });
      expect(screen.getByTestId("outcome-task")).toHaveTextContent("Skipped");
    });

    it("shows skipped message when taskText is empty even if taskSkipped is false", () => {
      renderOutcome({ taskText: "   ", taskSkipped: false });
      expect(screen.getByTestId("outcome-task")).toHaveTextContent("Skipped");
    });
  });

  describe("next actions panel", () => {
    it("renders all four navigation links", () => {
      renderOutcome();
      expect(screen.getByTestId("outcome-link-general")).toBeInTheDocument();
      expect(screen.getByTestId("outcome-link-tasks")).toBeInTheDocument();
      expect(screen.getByTestId("outcome-link-wiki")).toBeInTheDocument();
      expect(screen.getByTestId("outcome-link-settings")).toBeInTheDocument();
    });

    it("general channel link routes to #/channels/general", () => {
      renderOutcome();
      const link = screen.getByTestId("outcome-link-general");
      expect(link).toHaveAttribute("href", "#/channels/general");
    });

    it("tasks link routes to #/tasks", () => {
      renderOutcome();
      const link = screen.getByTestId("outcome-link-tasks");
      expect(link).toHaveAttribute("href", "#/tasks");
    });

    it("wiki link routes to #/wiki", () => {
      renderOutcome();
      const link = screen.getByTestId("outcome-link-wiki");
      expect(link).toHaveAttribute("href", "#/wiki");
    });

    it("settings link routes to #/apps/settings", () => {
      renderOutcome();
      const link = screen.getByTestId("outcome-link-settings");
      expect(link).toHaveAttribute("href", "#/apps/settings");
    });
  });

  describe("entry button", () => {
    it("calls onEnter when the CTA is clicked", () => {
      const onEnter = vi.fn();
      renderOutcome({ onEnter });
      fireEvent.click(screen.getByTestId("outcome-enter-button"));
      expect(onEnter).toHaveBeenCalledTimes(1);
    });

    it("renders the headline 'Office is open'", () => {
      renderOutcome();
      expect(screen.getByTestId("outcome-headline")).toHaveTextContent(
        "Office is open",
      );
    });
  });

  describe("partial failure / minimal data states", () => {
    it("renders cleanly with no agents, no blueprint, no runtime, skipped task", () => {
      renderOutcome({
        agents: [],
        selectedBlueprint: null,
        blueprints: [],
        primaryRuntime: "",
        taskText: "",
        taskSkipped: true,
        apiKeyCount: 0,
      });
      expect(screen.getByTestId("outcome-summary")).toBeInTheDocument();
      expect(screen.getByTestId("outcome-blueprint")).toHaveTextContent(
        "Start from scratch",
      );
      expect(screen.getByTestId("outcome-runtime")).toHaveTextContent(
        "Not selected",
      );
      expect(screen.getByTestId("outcome-task")).toHaveTextContent("Skipped");
    });
  });
});

import { describe, expect, it } from "vitest";

import { adaptPackPreview } from "./packPreview";
import type { BlueprintTemplate } from "./types";

describe("adaptPackPreview", () => {
  it("derives outcome from BLUEPRINT_DISPLAY when backend omits it", () => {
    // bookkeeping-invoicing-service is keyed in BLUEPRINT_DISPLAY
    // with "Books · invoices · monthly close" as its short
    // description. When the wire payload omits outcome (older
    // broker), the adapter should fall back to the display override
    // rather than dumping the long description into the card.
    const tpl: BlueprintTemplate = {
      id: "bookkeeping-invoicing-service",
      name: "Bookkeeping",
      description:
        "Long backend description that should not appear on the card.",
    };
    const preview = adaptPackPreview(tpl);
    expect(preview.outcome).toBe("Books · invoices · monthly close");
    expect(preview.category).toBe("services");
    expect(preview.icon).toBeDefined();
  });

  it("prefers backend outcome over BLUEPRINT_DISPLAY override", () => {
    const tpl: BlueprintTemplate = {
      id: "bookkeeping-invoicing-service",
      name: "Bookkeeping",
      description: "Long",
      outcome: "Backend-supplied outcome wins",
    };
    expect(adaptPackPreview(tpl).outcome).toBe("Backend-supplied outcome wins");
  });

  it("returns category 'other' for unknown blueprint ids without an explicit category", () => {
    const tpl: BlueprintTemplate = {
      id: "future-pack-not-keyed",
      name: "Mystery",
      description: "from-scratch backend",
    };
    expect(adaptPackPreview(tpl).category).toBe("other");
  });

  it("returns empty arrays when backend metadata is absent", () => {
    const tpl: BlueprintTemplate = {
      id: "future-pack-not-keyed",
      name: "Mystery",
      description: "no metadata",
    };
    const preview = adaptPackPreview(tpl);
    expect(preview.firstTasks).toEqual([]);
    expect(preview.skills).toEqual([]);
    expect(preview.requirements).toEqual([]);
    expect(preview.wikiScaffold).toEqual([]);
    expect(preview.channels).toEqual([]);
    expect(preview.exampleArtifacts).toEqual([]);
    expect(preview.estimatedSetupMinutes).toBeUndefined();
  });

  it("normalizes requirement kinds and tolerates unknown values", () => {
    const tpl: BlueprintTemplate = {
      id: "p",
      name: "P",
      description: "d",
      requirements: [
        { kind: "api-key", name: "ANTHROPIC_API_KEY", required: true },
        { kind: "weird-kind", name: "Mystery dep" },
      ],
    };
    const { requirements: reqs } = adaptPackPreview(tpl);
    expect(reqs[0]).toEqual({
      kind: "api-key",
      name: "ANTHROPIC_API_KEY",
      required: true,
      detail: undefined,
    });
    // Unknown kinds collapse to "runtime" so the chip still renders.
    expect(reqs[1].kind).toBe("runtime");
  });

  it("preserves built_in flag from BlueprintAgent payload", () => {
    const tpl: BlueprintTemplate = {
      id: "p",
      name: "P",
      description: "d",
      agents: [
        {
          slug: "ceo",
          name: "CEO",
          role: "lead",
          built_in: true,
          checked: true,
        },
        { slug: "designer", name: "Designer", role: "design", checked: true },
      ],
    };
    const { agents } = adaptPackPreview(tpl);
    expect(agents[0].builtIn).toBe(true);
    expect(agents[1].builtIn).toBe(false);
  });

  it("truncates long fallback descriptions on a word boundary", () => {
    const tpl: BlueprintTemplate = {
      id: "future-pack-not-keyed",
      name: "Mystery",
      description:
        "This is a very long backend description that exceeds the eighty-character outcome budget and must wrap",
    };
    const preview = adaptPackPreview(tpl);
    expect(preview.outcome.length).toBeLessThanOrEqual(85);
    expect(preview.outcome.endsWith("...")).toBe(true);
  });

  it("maps wire fields one-to-one when fully populated", () => {
    const tpl: BlueprintTemplate = {
      id: "youtube-factory",
      name: "YouTube Factory",
      description: "long",
      outcome: "Script · film · publish",
      category: "media",
      estimated_setup_minutes: 7,
      channels: [
        { slug: "scripts", name: "scripts", purpose: "Draft scripts" },
      ],
      skills: [{ name: "Scriptwriting", purpose: "Strong hooks" }],
      wiki_scaffold: [{ path: "team/scripts/intro.md", title: "Intro" }],
      first_tasks: [
        {
          id: "ten-video-slate",
          title: "Plan the first 10-video slate",
          prompt: "Plan it.",
          expected_output: "10-row table.",
        },
      ],
      requirements: [
        { kind: "runtime", name: "Claude Code or Codex CLI", required: true },
      ],
      example_artifacts: [{ kind: "plan", title: "Slate" }],
    };
    const preview = adaptPackPreview(tpl);
    expect(preview.outcome).toBe("Script · film · publish");
    expect(preview.category).toBe("media");
    expect(preview.estimatedSetupMinutes).toBe(7);
    expect(preview.channels).toHaveLength(1);
    expect(preview.skills[0].name).toBe("Scriptwriting");
    expect(preview.wikiScaffold[0].title).toBe("Intro");
    expect(preview.firstTasks[0].expectedOutput).toBe("10-row table.");
    expect(preview.requirements[0].required).toBe(true);
    expect(preview.exampleArtifacts[0].title).toBe("Slate");
  });
});

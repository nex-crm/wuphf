// packPreview.ts — frontend adapter that turns a BlueprintTemplate
// payload (from GET /onboarding/blueprints) into the PackPreview shape
// the wizard's pack library renders. The backend feeds in the new
// outcome/category/first_tasks/skills/wiki_scaffold/requirements
// fields when present; missing fields degrade gracefully (empty
// arrays, undefined optional values) so the detail panel still renders
// for older brokers or unfamiliar blueprint ids.

import { BLUEPRINT_DISPLAY } from "./constants";
import type {
  BlueprintCategoryKey,
  BlueprintRequirementKind,
  BlueprintTemplate,
} from "./types";

export type PackCategory = BlueprintCategoryKey | "other";

export interface PackPreviewAgent {
  slug: string;
  name: string;
  role: string;
  builtIn: boolean;
}

export interface PackPreviewChannel {
  slug: string;
  name: string;
  purpose: string;
}

export interface PackPreviewSkill {
  name: string;
  purpose: string;
}

export interface PackPreviewWikiEntry {
  path: string;
  title: string;
}

export interface PackPreviewFirstTask {
  id: string;
  title: string;
  prompt: string;
  expectedOutput: string;
}

export interface PackPreviewRequirement {
  kind: BlueprintRequirementKind;
  name: string;
  required: boolean;
  detail?: string;
}

export interface PackPreviewExampleArtifact {
  kind: string;
  title: string;
}

export interface PackPreview {
  id: string;
  name: string;
  category: PackCategory;
  outcome: string;
  description: string;
  icon?: string;
  agents: PackPreviewAgent[];
  channels: PackPreviewChannel[];
  skills: PackPreviewSkill[];
  wikiScaffold: PackPreviewWikiEntry[];
  firstTasks: PackPreviewFirstTask[];
  requirements: PackPreviewRequirement[];
  estimatedSetupMinutes?: number;
  exampleArtifacts: PackPreviewExampleArtifact[];
}

const KNOWN_CATEGORIES: ReadonlyArray<BlueprintCategoryKey> = [
  "services",
  "media",
  "product",
];

const KNOWN_REQUIREMENT_KINDS: ReadonlyArray<BlueprintRequirementKind> = [
  "runtime",
  "api-key",
  "local-tool",
];

const OUTCOME_FALLBACK_LIMIT = 80;

function isKnownCategory(value: string | undefined): value is PackCategory {
  return !!value && KNOWN_CATEGORIES.includes(value as BlueprintCategoryKey);
}

function normalizeRequirementKind(
  value: string | undefined,
): BlueprintRequirementKind {
  if (
    value &&
    KNOWN_REQUIREMENT_KINDS.includes(value as BlueprintRequirementKind)
  ) {
    return value as BlueprintRequirementKind;
  }
  return "runtime";
}

function deriveOutcome(template: BlueprintTemplate): string {
  if (template.outcome && template.outcome.trim() !== "") {
    return template.outcome.trim();
  }
  const display = BLUEPRINT_DISPLAY[template.id];
  if (display?.shortDescription) {
    return display.shortDescription;
  }
  const description = (template.description ?? "").trim();
  if (description.length <= OUTCOME_FALLBACK_LIMIT) {
    return description;
  }
  // Trim on a word boundary so the outcome reads as a sentence
  // fragment, not a mid-word cut.
  const truncated = description.slice(0, OUTCOME_FALLBACK_LIMIT);
  const lastSpace = truncated.lastIndexOf(" ");
  if (lastSpace > 40) {
    return `${truncated.slice(0, lastSpace).trimEnd()}...`;
  }
  return `${truncated.trimEnd()}...`;
}

function deriveCategory(template: BlueprintTemplate): PackCategory {
  if (isKnownCategory(template.category)) {
    return template.category;
  }
  const display = BLUEPRINT_DISPLAY[template.id];
  if (display?.category) {
    return display.category;
  }
  return "other";
}

export function adaptPackPreview(template: BlueprintTemplate): PackPreview {
  const display = BLUEPRINT_DISPLAY[template.id];
  return {
    id: template.id,
    name: template.name,
    description: template.description ?? "",
    outcome: deriveOutcome(template),
    category: deriveCategory(template),
    icon: display?.icon ?? template.emoji,
    agents: (template.agents ?? []).map((agent) => ({
      slug: agent.slug,
      name: agent.name,
      role: agent.role ?? "",
      builtIn: agent.built_in === true,
    })),
    channels: (template.channels ?? []).map((channel) => ({
      slug: channel.slug,
      name: (channel.name ?? channel.slug).trim(),
      purpose: (channel.purpose ?? "").trim(),
    })),
    skills: (template.skills ?? []).map((skill) => ({
      name: skill.name,
      purpose: (skill.purpose ?? "").trim(),
    })),
    wikiScaffold: (template.wiki_scaffold ?? []).map((entry) => ({
      path: entry.path,
      title: (entry.title ?? entry.path).trim(),
    })),
    firstTasks: (template.first_tasks ?? []).map((task) => ({
      id: task.id,
      title: task.title,
      prompt: (task.prompt ?? "").trim(),
      expectedOutput: (task.expected_output ?? "").trim(),
    })),
    requirements: (template.requirements ?? []).map((req) => ({
      kind: normalizeRequirementKind(req.kind),
      name: req.name,
      required: req.required === true,
      detail: req.detail?.trim() || undefined,
    })),
    estimatedSetupMinutes:
      typeof template.estimated_setup_minutes === "number" &&
      template.estimated_setup_minutes > 0
        ? template.estimated_setup_minutes
        : undefined,
    exampleArtifacts: (template.example_artifacts ?? []).map((item) => ({
      kind: (item.kind ?? "").trim(),
      title: item.title,
    })),
  };
}

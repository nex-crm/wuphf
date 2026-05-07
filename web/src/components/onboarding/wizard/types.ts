// Shared types for the onboarding wizard. Extracted from Wizard.tsx so
// each step component (Step1Welcome.tsx … Step7Ready.tsx) can import
// only what it needs. The corresponding runtime constants live in
// constants.ts; keeping types in their own file means a step file that
// only consumes shapes (no constant tables) imports zero runtime code.

export interface BlueprintTemplate {
  id: string;
  name: string;
  description: string;
  emoji?: string;
  agents?: BlueprintAgent[];
  tasks?: TaskTemplate[];
  // Pack-library wire fields. The broker surfaces these from the
  // operation blueprint yaml; older binaries omit them entirely. The
  // adapter in packPreview.ts handles missing fields by returning empty
  // arrays so the detail panel still renders.
  outcome?: string;
  category?: string;
  estimated_setup_minutes?: number;
  channels?: BlueprintChannel[];
  skills?: BlueprintSkill[];
  wiki_scaffold?: BlueprintWikiScaffoldEntry[];
  first_tasks?: BlueprintFirstTask[];
  requirements?: BlueprintRequirement[];
  example_artifacts?: BlueprintExampleArtifact[];
}

export interface BlueprintChannel {
  slug: string;
  name?: string;
  purpose?: string;
}

export interface BlueprintSkill {
  name: string;
  purpose?: string;
}

export interface BlueprintWikiScaffoldEntry {
  path: string;
  title?: string;
}

export interface BlueprintFirstTask {
  id: string;
  title: string;
  prompt?: string;
  expected_output?: string;
}

export type BlueprintRequirementKind = "runtime" | "api-key" | "local-tool";

export interface BlueprintRequirement {
  kind?: BlueprintRequirementKind | (string & {});
  name: string;
  required?: boolean;
  detail?: string;
}

export interface BlueprintExampleArtifact {
  kind?: string;
  title: string;
}

export interface BlueprintAgent {
  slug: string;
  name: string;
  role: string;
  emoji?: string;
  checked?: boolean;
  // built_in marks the lead agent — always included, never removable.
  // The backend also refuses to disable or remove a BuiltIn member, so
  // even if someone bypassed this UI, the broker would reject the write.
  built_in?: boolean;
}

export interface TaskTemplate {
  id: string;
  name: string;
  description: string;
  emoji?: string;
  prompt?: string;
}

export type WizardStep =
  | "welcome"
  | "templates"
  | "identity"
  | "analysis"
  | "team"
  | "setup"
  | "task"
  | "ready";

// Each runtime has a display label, the binary name the broker's prereqs
// check looks for, a canonical install page to link to when missing, and
// — for the runtimes the broker can actually dispatch agents to — the
// provider id the broker expects on POST /config.
export interface RuntimeSpec {
  label: string;
  binary: string;
  installUrl: string;
  provider: "claude-code" | "codex" | "opencode" | null;
}

export interface PrereqResult {
  name: string;
  required: boolean;
  found: boolean;
  ok?: boolean;
  version?: string;
  install_url?: string;
}

export type BlueprintCategoryKey = "services" | "media" | "product";

export interface BlueprintDisplay {
  category: BlueprintCategoryKey;
  shortDescription: string;
  icon: string;
}

export type MemoryBackend = "markdown" | "nex" | "gbrain" | "none";

export type NexSignupStatus =
  | "hidden"
  | "open"
  | "submitting"
  | "ok"
  | "fallback";

export type ReadinessStatus = "ready" | "next" | "missing";

export interface ReadinessCheck {
  label: string;
  status: ReadinessStatus;
  detail: string;
}

import type { ComponentType, CSSProperties } from "react";

import type { ConfigSnapshot, ConfigUpdate } from "../../../api/client";

// Shared types for the SettingsApp + its section panels. Extracted from
// SettingsApp.tsx so each section file (lands in subsequent stack PRs)
// can import only what it needs without dragging in section bodies.

export type SectionId =
  | "general"
  | "local-llms"
  | "image-gen"
  | "company"
  | "keys"
  | "integrations"
  | "intervals"
  | "flags"
  | "danger";

export interface Section {
  id: SectionId;
  Icon: ComponentType<{ className?: string; style?: CSSProperties }>;
  name: string;
}

export interface SectionGroup {
  label: string;
  items: Section[];
}

// SectionProps is the contract every section panel implements. The
// parent SettingsApp passes the live config + a save callback that
// debounces via react-query's mutation pipeline.
export interface SectionProps {
  cfg: ConfigSnapshot;
  save: (patch: ConfigUpdate) => Promise<void>;
}

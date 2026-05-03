import {
  Building,
  Key,
  MediaImage,
  Puzzle,
  Settings as SettingsIcon,
  Terminal,
  Timer,
  WarningTriangle,
} from "iconoir-react";

import type { SectionGroup } from "./types";

// Sidebar nav grouping. The order here is the user-facing order in the
// settings shell. Adding a new section means adding a row to the right
// group + an entry in SectionId in types.ts + a `case` in SettingsApp's
// section switch.
export const SECTION_GROUPS: SectionGroup[] = [
  {
    label: "Workspace",
    items: [
      { id: "general", Icon: SettingsIcon, name: "General" },
      { id: "local-llms", Icon: Terminal, name: "Local LLMs" },
      { id: "image-gen", Icon: MediaImage, name: "Image generation" },
      { id: "company", Icon: Building, name: "Company" },
    ],
  },
  {
    label: "Credentials",
    items: [
      { id: "keys", Icon: Key, name: "API Keys" },
      { id: "integrations", Icon: Puzzle, name: "Integrations" },
    ],
  },
  {
    label: "System",
    items: [
      { id: "intervals", Icon: Timer, name: "Polling" },
      { id: "flags", Icon: Terminal, name: "CLI Flags" },
    ],
  },
  {
    label: "Advanced",
    items: [{ id: "danger", Icon: WarningTriangle, name: "Danger Zone" }],
  },
];

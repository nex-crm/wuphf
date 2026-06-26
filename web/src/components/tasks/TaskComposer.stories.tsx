import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import type { ConfigSnapshot, OfficeMember } from "../../api/client";
import { TaskComposer } from "./TaskComposer";

const SAMPLE_MEMBERS: OfficeMember[] = [
  {
    slug: "ceo",
    name: "CEO",
    role: "supervisor",
    emoji: "👔",
    built_in: true,
    provider: { kind: "claude-code", model: "claude-opus-4-8" },
  },
  {
    slug: "librarian",
    name: "Librarian",
    role: "knowledge",
    emoji: "📚",
    built_in: true,
    provider: { kind: "claude-code", model: "claude-sonnet-4-6" },
  },
  {
    slug: "eng",
    name: "Engineer",
    role: "specialist",
    emoji: "🛠️",
    provider: { kind: "codex", model: "gpt-5.5" },
  },
  {
    slug: "researcher",
    name: "Researcher",
    role: "specialist",
    emoji: "🔍",
    // No explicit binding → inherits the install default runtime.
  },
];

const SAMPLE_CONFIG: Partial<ConfigSnapshot> = {
  llm_provider: "claude-code",
  llm_provider_kinds: ["claude-code", "codex", "opencode", "ollama"],
  team_lead_slug: "ceo",
};

function buildClient(members: OfficeMember[] = SAMPLE_MEMBERS) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    },
  });
  client.setQueryData(["office-members"], { members });
  client.setQueryData(["config"], SAMPLE_CONFIG);
  client.setQueryData(["local-providers"], []);
  return client;
}

const meta: Meta<typeof TaskComposer> = {
  title: "Tasks / TaskComposer",
  component: TaskComposer,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <QueryClientProvider client={buildClient()}>
        <div style={{ display: "flex", height: "100vh" }}>
          <Story />
        </div>
      </QueryClientProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof TaskComposer>;

// Default: owner = CEO (claude-code), so the Effort chip is active with
// claude's levels (low/medium/high/xhigh/max).
export const Default: Story = {};

// Owner with a codex binding — switch the Owner chip to "Engineer" to see the
// Model list and Effort levels (minimal/low/medium/high/xhigh) repopulate for
// codex. Demonstrates the model→effort coupling.
export const CodexOwner: Story = {
  decorators: [
    (Story) => (
      <QueryClientProvider client={buildClient(SAMPLE_MEMBERS)}>
        <div style={{ display: "flex", height: "100vh" }}>
          <Story />
        </div>
      </QueryClientProvider>
    ),
  ],
};

// No members loaded yet (cold boot) — the composer still renders with a CEO
// fallback owner and the install-default runtime.
export const NoMembers: Story = {
  decorators: [
    (Story) => (
      <QueryClientProvider client={buildClient([])}>
        <div style={{ display: "flex", height: "100vh" }}>
          <Story />
        </div>
      </QueryClientProvider>
    ),
  ],
};

import { useEffect, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import type { Channel, OfficeMember } from "../../api/client";
import { Button } from "../ui/Button";
import { IssueCreateDialog } from "./IssueCreateDialog";

const SAMPLE_CHANNELS: Channel[] = [
  { slug: "general", name: "General" },
  { slug: "bookkeeping", name: "Bookkeeping" },
  { slug: "sales-pipeline", name: "Sales pipeline" },
  { slug: "ops-week", name: "Ops week" },
];

const SAMPLE_MEMBERS: OfficeMember[] = [
  { slug: "ceo", name: "CEO", role: "supervisor", emoji: "👔" },
  { slug: "bookkeeper", name: "Bookkeeper", role: "specialist", emoji: "📒" },
  { slug: "planner", name: "Planner", role: "specialist", emoji: "🗓️" },
  { slug: "researcher", name: "Researcher", role: "specialist", emoji: "🔍" },
];

function buildClient(opts?: {
  channels?: Channel[];
  members?: OfficeMember[];
}) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    },
  });
  client.setQueryData(["channels"], {
    channels: opts?.channels ?? SAMPLE_CHANNELS,
  });
  client.setQueryData(["office-members"], {
    members: opts?.members ?? SAMPLE_MEMBERS,
  });
  return client;
}

interface HarnessProps {
  defaultChannel?: string;
  defaultAssignee?: string;
  startOpen?: boolean;
  client?: QueryClient;
}

function Harness({
  defaultChannel,
  defaultAssignee,
  startOpen = true,
  client,
}: HarnessProps) {
  const [open, setOpen] = useState(startOpen);
  const queryClient = client ?? buildClient();
  // Reflect the prop into local state when the story re-mounts so toggling
  // controls in Storybook actually reopens the modal.
  useEffect(() => {
    setOpen(startOpen);
  }, [startOpen]);
  return (
    <QueryClientProvider client={queryClient}>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "flex-start",
          gap: 12,
          minHeight: 200,
        }}
      >
        <Button onClick={() => setOpen(true)}>Open create-issue dialog</Button>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            maxWidth: 320,
          }}
        >
          Submit with Cmd/Ctrl+Enter. Submission is a no-op in Storybook (no
          broker), but the loading state is exercised by the form's own mutation
          flow.
        </p>
        <IssueCreateDialog
          open={open}
          onOpenChange={setOpen}
          defaultChannel={defaultChannel}
          defaultAssignee={defaultAssignee}
          navigateOnCreate={false}
        />
      </div>
    </QueryClientProvider>
  );
}

const meta: Meta<typeof Harness> = {
  title: "Features/Issues/IssueCreateDialog",
  component: Harness,
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Linear-inspired issue creation dialog. Title is the hero input, description is a markdown textarea, channel + assignee live as chip-style pickers in the footer. Cmd/Ctrl+Enter submits.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof Harness>;

export const Default: Story = {
  args: { startOpen: true },
};

export const PreselectedChannel: Story = {
  args: { startOpen: true, defaultChannel: "bookkeeping" },
};

export const PreselectedAssignee: Story = {
  args: { startOpen: true, defaultAssignee: "bookkeeper" },
};

export const NoChannelsYet: Story = {
  args: { startOpen: true },
  render: (args) => (
    <Harness {...args} client={buildClient({ channels: [], members: [] })} />
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import type { ExtractedWorkflow } from "../../api/workflows";
import ExtractedWorkflows from "./ExtractedWorkflows";

// Seed the React Query cache so the feed renders without a broker. retry/refetch
// are disabled so the seeded data is what the story shows.
function seeded(workflows: ExtractedWorkflow[]) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnMount: false, refetchInterval: false },
    },
  });
  qc.setQueryData(["workflows", "extracted"], { workflows });
  return qc;
}

function Wrapped({ workflows }: { workflows: ExtractedWorkflow[] }) {
  return (
    <QueryClientProvider client={seeded(workflows)}>
      <div style={{ padding: 24, maxWidth: 820 }}>
        <ExtractedWorkflows />
      </div>
    </QueryClientProvider>
  );
}

const digest: ExtractedWorkflow = {
  fingerprint: "gmail_fetch_emails>slack_send_message",
  name: "Urgent Inbox → Slack Alert",
  description:
    "Pulls the day's important emails from Gmail and posts a summary of the urgent ones to Slack #general.",
  why: "Two ordered steps form a repeatable procedure with a clear outcome; the time-bounded query implies a recurring daily digest, not a one-off lookup.",
  confidence: 0.9,
  trigger: { kind: "schedule", interval_minutes: 1440 },
  recurrence: 1,
  task_ids: ["OFFICE-443"],
  wiki_context: ["playbooks/inbox-triage.md", "team/escalation.md"],
  spec: {
    version: "1",
    id: "extracted-gmail-fetch-emails-slack-send-message",
    goal: "Urgent Inbox → Slack Alert",
    operator: "outbound",
    initial: "start",
    states: [{ id: "start" }, { id: "step_1" }, { id: "done" }],
    events: [{ id: "run" }, { id: "gmail_fetch_emails_done" }],
    transitions: [
      { from: "start", to: "step_1", on: "run" },
      { from: "step_1", to: "done", on: "gmail_fetch_emails_done" },
    ],
    actions: [
      {
        id: "gmail_fetch_emails",
        kind: "deterministic",
        platform: "gmail",
        action_id: "GMAIL_FETCH_EMAILS",
        params: { query: "is:important newer_than:1d" },
        result_path: "data.messages",
        expose: ["sender", "subject"],
      },
      {
        id: "slack_send_message",
        kind: "external",
        platform: "slack",
        action_id: "SLACK_SEND_MESSAGE",
        params: { channel: "general" },
      },
    ],
  },
};

const recurring: ExtractedWorkflow = {
  ...digest,
  fingerprint: "gmail_fetch_emails>gmail_create_email_draft",
  name: "Draft replies to waiting emails",
  description:
    "Finds Gmail threads awaiting a reply and drafts a response for each, ready for review.",
  why: "The same fetch-then-draft procedure recurred across four tasks — a strong signal it's worth automating.",
  confidence: 0.82,
  trigger: { kind: "manual" },
  recurrence: 4,
  task_ids: ["OFFICE-201", "OFFICE-260", "OFFICE-330", "OFFICE-401"],
  wiki_context: ["playbooks/reply-tone.md"],
};

const meta: Meta<typeof Wrapped> = {
  title: "Apps / Detected Workflows",
  component: Wrapped,
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj<typeof Wrapped>;

export const SingleDetection: Story = { args: { workflows: [digest] } };

export const WithRecurrence: Story = {
  args: { workflows: [recurring, digest] },
};

export const Empty: Story = { args: { workflows: [] } };

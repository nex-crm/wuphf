import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { GettingStartedChecklist } from "./GettingStartedChecklist";
import type { OnboardingChecklistItem, OnboardingState } from "./types";
import { ONBOARDING_STATE_QUERY_KEY } from "./useGettingStartedChecklist";

function fullChecklist(
  done: Partial<Record<string, boolean>> = {},
): OnboardingChecklistItem[] {
  return [
    { id: "pick_team", done: done.pick_team ?? false },
    { id: "second_key", done: done.second_key ?? false },
    { id: "github_repo", done: done.github_repo ?? false },
    { id: "github_star", done: done.github_star ?? false },
    { id: "discord", done: done.discord ?? false },
  ];
}

function buildClient(state: OnboardingState) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    },
  });
  client.setQueryData(ONBOARDING_STATE_QUERY_KEY, state);
  return client;
}

interface HarnessProps {
  state: OnboardingState;
}

function Harness({ state }: HarnessProps) {
  return (
    <QueryClientProvider client={buildClient(state)}>
      <div style={{ maxWidth: 360 }}>
        <GettingStartedChecklist />
      </div>
    </QueryClientProvider>
  );
}

const meta: Meta<typeof Harness> = {
  title: "Onboarding/GettingStartedChecklist",
  component: Harness,
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Post-onboarding 'Settle into your office' panel. Renders the five getting-started items with WUPHF copy, a green tick per done item, and a dismiss control. Hides itself once dismissed or fully complete. Actions are no-ops in Storybook (no broker).",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof Harness>;

export const FreshOffice: Story = {
  args: {
    state: { checklist: fullChecklist(), checklist_dismissed: false },
  },
};

export const PartiallySettled: Story = {
  args: {
    state: {
      checklist: fullChecklist({ pick_team: true, github_star: true }),
      checklist_dismissed: false,
    },
  },
};

export const OneItemLeft: Story = {
  args: {
    state: {
      checklist: fullChecklist({
        pick_team: true,
        second_key: true,
        github_repo: true,
        github_star: true,
      }),
      checklist_dismissed: false,
    },
  },
};

/**
 * Every item complete. The panel has done its job, so it hides itself
 * entirely. The story canvas renders nothing on purpose.
 */
export const AllDone: Story = {
  args: {
    state: {
      checklist: fullChecklist({
        pick_team: true,
        second_key: true,
        github_repo: true,
        github_star: true,
        discord: true,
      }),
      checklist_dismissed: false,
    },
  },
};

/**
 * The user clicked "I am settled in". The panel is dismissed and hides
 * itself even though items remain undone. The story canvas renders nothing
 * on purpose.
 */
export const Dismissed: Story = {
  args: {
    state: {
      checklist: fullChecklist(),
      checklist_dismissed: true,
    },
  },
};

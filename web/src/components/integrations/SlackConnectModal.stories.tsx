import type { Meta, StoryObj } from "@storybook/react";

import { SlackConnectModal } from "./SlackConnectModal";

/**
 * The guided Slack-onboarding wizard. Each story renders a single step via
 * `initialStep` so the whole flow is reviewable (and theme-checkable) without a
 * live broker. Switch the theme from the toolbar to confirm all three skins.
 *
 * The "Create app" step fetches the manifest from the broker, so in Storybook
 * (no backend) it shows its loading state — the other steps render fully.
 */
const meta: Meta<typeof SlackConnectModal> = {
  title: "Integrations / Slack Connect Wizard",
  component: SlackConnectModal,
  parameters: { layout: "fullscreen" },
  args: { open: true, onClose: () => {} },
};
export default meta;

type Story = StoryObj<typeof SlackConnectModal>;

export const Intro: Story = { args: { initialStep: "intro" } };
export const CreateApp: Story = { args: { initialStep: "create" } };
export const Tokens: Story = { args: { initialStep: "tokens" } };
export const Channel: Story = { args: { initialStep: "channel" } };
export const Activating: Story = { args: { initialStep: "activating" } };
export const Live: Story = { args: { initialStep: "done" } };

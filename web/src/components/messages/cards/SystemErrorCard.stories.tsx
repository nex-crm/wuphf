import type { Meta, StoryObj } from "@storybook/react-vite";

import { SystemErrorCard } from "./SystemErrorCard";

const meta: Meta<typeof SystemErrorCard> = {
  title: "Messages / Cards / SystemErrorCard",
  component: SystemErrorCard,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof SystemErrorCard>;

export const ClaudeCode: Story = {
  args: {
    payload: {
      provider: "claude-code",
      sign_in_command: "claude auth login",
      detail:
        "Claude CLI requires login. Run `claude login` or use /init to choose a different provider.",
      reporter: "ceo",
    },
  },
};

export const Codex: Story = {
  args: {
    payload: {
      provider: "codex",
      sign_in_command: "codex login",
      detail:
        "Codex CLI requires login. Run `codex login` or use /provider to choose a different provider.",
      reporter: "eng",
    },
  },
};

export const Opencode: Story = {
  args: {
    payload: {
      provider: "opencode",
      sign_in_command: "opencode auth login",
      detail: "Opencode reports no provider credentials configured.",
    },
  },
};

export const WithRetry: Story = {
  args: {
    payload: {
      provider: "claude-code",
      sign_in_command: "claude auth login",
      detail: "Claude CLI requires login.",
    },
    onRetry: () => {
      // Storybook noop — wire to a real handler in production.
    },
  },
};

export const NoProvider: Story = {
  args: {
    payload: {
      detail: "Provider authentication required.",
    },
  },
};

export const NoCommand: Story = {
  args: {
    payload: {
      provider: "unknown",
      detail: "We can't determine the sign-in flow for this provider.",
    },
  },
};

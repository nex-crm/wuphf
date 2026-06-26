import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ComposioOnboarding } from "./ComposioOnboarding";

function Wrapped() {
  const qc = new QueryClient();
  return (
    <QueryClientProvider client={qc}>
      <div className="op-page" style={{ maxWidth: 720 }}>
        <header className="op-page-header">
          <h2>Integrations</h2>
          <p>External accounts, gateways, channels, and action audit.</p>
        </header>
        <ComposioOnboarding onConnected={() => {}} />
      </div>
    </QueryClientProvider>
  );
}

const meta: Meta<typeof Wrapped> = {
  title: "Features/Integrations/ComposioOnboarding",
  component: Wrapped,
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof Wrapped>;

export const FirstRun: Story = {};

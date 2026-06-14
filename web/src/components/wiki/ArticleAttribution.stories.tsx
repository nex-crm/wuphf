import type { Meta, StoryObj } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import type { ArticleAttribution as Attribution } from "../../api/attribution";
import { ArticleAttribution } from "./ArticleAttribution";

// Seed the query cache so the chip renders without a network call.
function Seeded({
  articleRef,
  value,
}: {
  articleRef: string;
  value: Attribution | null;
}) {
  const qc = new QueryClient();
  qc.setQueryData(["article-attribution", articleRef], value);
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 24 }}>
        <ArticleAttribution articleRef={articleRef} />
      </div>
    </QueryClientProvider>
  );
}

const meta: Meta<typeof Seeded> = {
  title: "Wiki/ArticleAttribution",
  component: Seeded,
};

export default meta;
type Story = StoryObj<typeof Seeded>;

export const ProducedForTask: Story = {
  args: {
    articleRef: "ra_abc",
    value: {
      taskId: "OFFICE-1",
      taskTitle: "Q2 pricing launch",
      owner: "revops",
    },
  },
};

export const NoProducingTask: Story = {
  args: { articleRef: "ra_x", value: null },
};

import { useEffect, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import type { WikiFSTreeNode } from "../../api/wiki";
import WikiSidebar from "./WikiSidebar";
import "../../styles/wiki.css";

/**
 * The wiki's persistent left navigation: fixed menu links (Overview /
 * Recent changes / Wiki health) above the page tree — spaces (Companies,
 * People, Playbooks, …) as root groups with articles nested beneath,
 * search at the top of the tree.
 *
 * The tree fetches GET /wiki/tree, so the stories stub `window.fetch`
 * with a seeded tree rather than mounting the app shell.
 */

const TREE: WikiFSTreeNode[] = [
  {
    name: "companies",
    path: "team/companies",
    type: "dir",
    title: "Companies",
    children: [
      {
        name: "acme.md",
        path: "team/companies/acme.md",
        type: "page",
        title: "Acme Corp",
      },
      {
        name: "globex.md",
        path: "team/companies/globex.md",
        type: "page",
        title: "Globex",
      },
    ],
  },
  {
    name: "people",
    path: "team/people",
    type: "dir",
    title: "People",
    children: [
      {
        name: "nazz.md",
        path: "team/people/nazz.md",
        type: "page",
        title: "Nazz",
      },
      {
        name: "sarah-chen.md",
        path: "team/people/sarah-chen.md",
        type: "page",
        title: "Sarah Chen",
      },
    ],
  },
  {
    name: "playbooks",
    path: "team/playbooks",
    type: "dir",
    title: "Playbooks",
    children: [
      {
        name: "renewal.md",
        path: "team/playbooks/renewal.md",
        type: "page",
        title: "Renewal Playbook",
      },
    ],
  },
  {
    name: "assets",
    path: "team/assets",
    type: "dir",
    title: "Assets",
    children: [
      {
        name: "report.pdf",
        path: "team/assets/report.pdf",
        type: "file",
        title: "report.pdf",
        ext: ".pdf",
      },
    ],
  },
];

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

/** Stub /wiki/tree so the page tree renders without a broker. */
function useTreeFetchStub(nodes: WikiFSTreeNode[]): boolean {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const original = window.fetch;
    window.fetch = (async (input: RequestInfo | URL) => {
      const url = new URL(
        typeof input === "string" ? input : input.toString(),
        window.location.origin,
      );
      if (url.pathname.endsWith("/wiki/tree")) {
        return jsonResponse({ nodes });
      }
      return jsonResponse({});
    }) as typeof window.fetch;
    setReady(true);
    return () => {
      window.fetch = original;
    };
  }, [nodes]);
  return ready;
}

interface HarnessProps {
  currentPath: string | null;
}

function Harness({ currentPath }: HarnessProps) {
  const ready = useTreeFetchStub(TREE);
  if (!ready) return null;
  return (
    <div
      className="wiki-root"
      style={{ height: 560, width: 320, display: "flex" }}
    >
      <WikiSidebar currentPath={currentPath} onNavigate={() => {}} />
    </div>
  );
}

const meta: Meta<typeof Harness> = {
  title: "Wiki / WikiSidebar",
  component: Harness,
};
export default meta;

type Story = StoryObj<typeof Harness>;

/** Overview view: no page open, the root spaces collapsed. */
export const Default: Story = {
  args: { currentPath: "" },
};

/** An article open: its branch highlights once expanded. */
export const WithActiveArticle: Story = {
  args: { currentPath: "team/companies/acme.md" },
};

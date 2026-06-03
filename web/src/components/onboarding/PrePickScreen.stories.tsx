import { useEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { PrePickScreen } from "./PrePickScreen";
import type { PrereqResult } from "./runtimes";

/**
 * PrePickScreen stories.
 *
 * The screen reads /onboarding/prereqs and (when a runtime guide is expanded)
 * /onboarding/install-steps + /onboarding/verify through the raw api client,
 * which calls `fetch`. There is no MSW in this Storybook, so each story stubs
 * `window.fetch` with a path-keyed responder and seeds:
 *   - the prereq detection state (drives the card labels), and
 *   - the verify classification the backend would return when the user presses
 *     Verify inside an expanded guide.
 * The static guided steps come from a canned install-steps payload so the
 * numbered setup renders without a broker.
 */

const CLAUDE_INSTALL_STEPS = {
  runtime: "claude",
  steps: [
    {
      title: "Install Claude Code",
      detail: "One npm install and the CLI is on your PATH.",
      command: "npm install -g @anthropic-ai/claude-code",
      link_label: "Install guide",
      link_url: "https://claude.ai/code",
    },
    {
      title: "Sign in to Claude",
      detail: "Sign in once and the office can run turns on your account.",
      command: "claude auth login",
    },
    {
      title: "Verify",
      detail: "Press Verify and we confirm Claude is installed and signed in.",
    },
  ],
};

interface PrereqSeed {
  found?: boolean;
  version?: string;
  session_probed?: boolean;
  signed_in?: boolean;
  sign_in_command?: string;
}

type VerifySeed = Record<string, unknown>;

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function buildPrereqs(seed: Record<string, PrereqSeed>): PrereqResult[] {
  const names = ["claude", "codex", "opencode"];
  return names.map((name) => ({
    name,
    required: false,
    found: seed[name]?.found ?? false,
    version: seed[name]?.version,
    session_probed: seed[name]?.session_probed,
    signed_in: seed[name]?.signed_in,
    sign_in_command: seed[name]?.sign_in_command,
  }));
}

interface SeedArgs {
  prereqs: Record<string, PrereqSeed>;
  verify: VerifySeed;
}

/**
 * Install a path-keyed fetch responder for the lifetime of the story. Returns
 * a teardown so React strict re-renders do not stack stubs.
 */
function useFetchStub({ prereqs, verify }: SeedArgs) {
  useEffect(() => {
    const original = window.fetch;
    window.fetch = (async (input: RequestInfo | URL) => {
      const url = new URL(
        typeof input === "string" ? input : input.toString(),
        window.location.origin,
      );
      const path = url.pathname.replace(/^\/api/, "");
      if (path === "/onboarding/prereqs") {
        return jsonResponse(buildPrereqs(prereqs));
      }
      if (path === "/onboarding/install-steps") {
        return jsonResponse(CLAUDE_INSTALL_STEPS);
      }
      if (path === "/onboarding/verify") {
        return jsonResponse(verify);
      }
      if (path === "/onboarding/state") {
        return jsonResponse({});
      }
      // /config, /onboarding/transition, local-providers: empty OK.
      return jsonResponse({});
    }) as typeof window.fetch;
    return () => {
      window.fetch = original;
    };
  }, [prereqs, verify]);
}

function Harness(args: SeedArgs) {
  useFetchStub(args);
  return <PrePickScreen onComplete={() => {}} />;
}

const meta: Meta<typeof Harness> = {
  title: "Onboarding/PrePickScreen",
  component: Harness,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "The pre-office provider picker. Each runtime card carries a 'Set up & verify' toggle that expands a guided setup (numbered steps with copyable commands and doc links) plus a live Verify button. Verify returns a classified result: pass (green), auth_required (amber, surfaces the sign-in command), or not_installed (neutral, surfaces the install command). Stories seed both the detection state and the verify classification. Expand a Claude card and press Verify to see the seeded result.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof Harness>;

/** Fresh machine: nothing detected yet, cards read "Not installed". */
export const Detecting: Story = {
  args: {
    prereqs: {},
    verify: {
      status: "not_installed",
      runtime: "claude",
      command: "npm install -g @anthropic-ai/claude-code",
      hint: "claude is not on your PATH yet. Run the install command, then verify again.",
      failed_step: "Install claude",
    },
  },
};

/** Claude is installed and signed in. Verify will classify it ready. */
export const Ready: Story = {
  args: {
    prereqs: {
      claude: {
        found: true,
        version: "1.2.3",
        session_probed: true,
        signed_in: true,
      },
    },
    verify: { status: "pass", runtime: "claude", version: "1.2.3" },
  },
};

/** Claude is installed but not signed in (issue #932 copy-sign-in path). */
export const LoginRequired: Story = {
  args: {
    prereqs: {
      claude: {
        found: true,
        version: "1.2.3",
        session_probed: true,
        signed_in: false,
        sign_in_command: "claude auth login",
      },
    },
    verify: {
      status: "auth_required",
      runtime: "claude",
      command: "claude auth login",
      sign_in_command: "claude auth login",
      hint: "Run the sign-in command, then verify again.",
      failed_step: "Sign in to Claude",
    },
  },
};

/** Claude is not installed. Verify surfaces the install command + hint. */
export const NotInstalled: Story = {
  args: {
    prereqs: { claude: { found: false } },
    verify: {
      status: "not_installed",
      runtime: "claude",
      command: "npm install -g @anthropic-ai/claude-code",
      hint: "claude is not on your PATH yet. Run the install command, then verify again.",
      failed_step: "Install claude",
    },
  },
};

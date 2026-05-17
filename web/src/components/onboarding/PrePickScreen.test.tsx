import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PrePickScreen } from "./PrePickScreen";

// The pre-pick screen reads /onboarding/prereqs and POSTs /config +
// /onboarding/complete. Stub both so tests stay focused on the screen's
// own behavior — no broker contract is exercised here.
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    get: vi.fn(),
    post: vi.fn(),
  };
});

import { get, post } from "../../api/client";

const getMock = vi.mocked(get);
const postMock = vi.mocked(post);

beforeEach(() => {
  getMock.mockReset();
  postMock.mockReset();
  postMock.mockResolvedValue({});
});

afterEach(() => {
  cleanup();
});

function mockPrereqs(found: Record<string, boolean>) {
  getMock.mockResolvedValue([
    {
      name: "claude",
      required: false,
      found: found.claude ?? false,
      version: "v1.2.3",
    },
    {
      name: "codex",
      required: false,
      found: found.codex ?? false,
      version: "v0.8.1",
    },
    { name: "opencode", required: false, found: found.opencode ?? false },
  ]);
}

describe("PrePickScreen", () => {
  it("renders the three dispatchable runtime cards plus a skip affordance", async () => {
    mockPrereqs({ claude: true, codex: true });
    render(<PrePickScreen onComplete={vi.fn()} />);

    expect(
      await screen.findByTestId("pre-pick-card-claude-code"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("pre-pick-card-codex")).toBeInTheDocument();
    expect(screen.getByTestId("pre-pick-card-opencode")).toBeInTheDocument();
    expect(screen.getByTestId("pre-pick-skip")).toBeInTheDocument();
  });

  it("posts /config and /onboarding/complete when a detected runtime is picked", async () => {
    mockPrereqs({ claude: true });
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const card = await screen.findByTestId("pre-pick-card-claude-code");
    await waitFor(() => expect(card).not.toBeDisabled());
    fireEvent.click(card);

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));

    expect(postMock).toHaveBeenCalledWith(
      "/config",
      expect.objectContaining({
        llm_provider: "claude-code",
        llm_provider_priority: ["claude-code"],
        memory_backend: "markdown",
      }),
    );
    expect(postMock).toHaveBeenCalledWith(
      "/onboarding/complete",
      expect.objectContaining({
        skip_task: true,
        blueprint: "",
        agents: [],
        runtime: "claude-code",
      }),
    );
  });

  it("posts /onboarding/complete with no runtime when the user picks the skip affordance", async () => {
    mockPrereqs({});
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const skip = await screen.findByTestId("pre-pick-skip");
    fireEvent.click(skip);

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));

    // /config must NOT be called for sandbox mode — that's the path that
    // distinguishes evaluators ("look around first" via Marcus) from
    // committed runtime pickers in Phase 2.
    expect(
      postMock.mock.calls.find((call) => call[0] === "/config"),
    ).toBeUndefined();
    expect(postMock).toHaveBeenCalledWith(
      "/onboarding/complete",
      expect.objectContaining({
        skip_task: true,
        runtime: "",
        runtime_priority: [],
      }),
    );
  });

  it("opens the install URL in a new tab when a missing runtime card is clicked", async () => {
    mockPrereqs({});
    const open = vi.fn();
    const originalOpen = window.open;
    Object.defineProperty(window, "open", {
      configurable: true,
      writable: true,
      value: open,
    });
    try {
      render(<PrePickScreen onComplete={vi.fn()} />);
      const card = await screen.findByTestId("pre-pick-card-opencode");
      // Wait for prereqs fetch so the status text settles on "Not installed".
      await waitFor(() => expect(card.textContent).toMatch(/Not installed/));
      fireEvent.click(card);
      expect(open).toHaveBeenCalledWith(
        "https://opencode.ai",
        "_blank",
        "noopener,noreferrer",
      );
      // No backend writes should fire from an install-link click.
      expect(postMock).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, "open", {
        configurable: true,
        writable: true,
        value: originalOpen,
      });
    }
  });

  it("surfaces an error if /onboarding/complete fails and does not invoke onComplete", async () => {
    mockPrereqs({ codex: true });
    postMock.mockImplementation(async (path: string) => {
      if (path === "/onboarding/complete") {
        throw new Error("broker unreachable");
      }
      return {};
    });
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const card = await screen.findByTestId("pre-pick-card-codex");
    await waitFor(() => expect(card).not.toBeDisabled());
    fireEvent.click(card);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /broker unreachable/i,
    );
    expect(onComplete).not.toHaveBeenCalled();
  });
});

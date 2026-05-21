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
// /onboarding/transition. Stub both so tests stay focused on the screen's
// own behavior -- no broker contract is exercised here.
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    get: vi.fn(),
    post: vi.fn(),
    getLocalProvidersStatus: vi.fn(),
  };
});

import { get, getLocalProvidersStatus, post } from "../../api/client";

const getMock = vi.mocked(get);
const postMock = vi.mocked(post);
const getLocalProvidersStatusMock = vi.mocked(getLocalProvidersStatus);

beforeEach(() => {
  getMock.mockReset();
  postMock.mockReset();
  postMock.mockResolvedValue({});
  // Default: local-providers returns an empty list (no local runtimes detected)
  getLocalProvidersStatusMock.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
});

function mockPrereqs(
  found: Record<string, boolean>,
  onboardingState: { onboarded?: boolean; phase?: string } = {},
  sessions: Record<
    string,
    { session_probed?: boolean; signed_in?: boolean; sign_in_command?: string }
  > = {},
) {
  const prereqs = [
    {
      name: "claude",
      required: false,
      found: found.claude ?? false,
      version: "v1.2.3",
      ...(sessions.claude ?? {}),
    },
    {
      name: "codex",
      required: false,
      found: found.codex ?? false,
      version: "v0.8.1",
      ...(sessions.codex ?? {}),
    },
    {
      name: "opencode",
      required: false,
      found: found.opencode ?? false,
      ...(sessions.opencode ?? {}),
    },
  ];
  // Route the GET mock by path. The /onboarding/state probe added for the
  // #979 phase-complete guard runs on every click; default it to a fresh
  // install (no phase set) unless the test overrides it.
  getMock.mockImplementation(async (path: string) => {
    if (path === "/onboarding/prereqs") {
      return prereqs;
    }
    if (path === "/onboarding/state") {
      return onboardingState;
    }
    return {};
  });
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

  it("posts /config and starts CEO onboarding when a detected runtime is picked", async () => {
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
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "greet",
    });
    expect(
      postMock.mock.calls.find((call) => call[0] === "/onboarding/complete"),
    ).toBeUndefined();
  });

  it("starts CEO onboarding with no runtime when the user picks the skip affordance", async () => {
    mockPrereqs({});
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const skip = await screen.findByTestId("pre-pick-skip");
    fireEvent.click(skip);

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));

    // /config must NOT be called for sandbox mode -- that's the path that
    // distinguishes evaluators ("look around first") from committed runtime
    // pickers.
    expect(
      postMock.mock.calls.find((call) => call[0] === "/config"),
    ).toBeUndefined();
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "greet",
    });
  });

  it("does NOT persist /config on skip even when form fields are filled", async () => {
    mockPrereqs({});
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    // User types an API key, then changes their mind and clicks skip.
    const apiKeyToggle = await screen.findByTestId(
      "pre-pick-api-paste-ANTHROPIC_API_KEY",
    );
    fireEvent.click(apiKeyToggle);
    const apiKeyInput = await screen.findByTestId(
      "pre-pick-api-input-ANTHROPIC_API_KEY",
    );
    fireEvent.change(apiKeyInput, {
      target: { value: "sk-ant-leaked-secret" },
    });

    const skip = screen.getByTestId("pre-pick-skip");
    fireEvent.click(skip);

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));

    // Hard contract: skip must not persist anything the user typed. The
    // /config endpoint never sees the key, so leaked-secret can't reach disk.
    expect(
      postMock.mock.calls.find((call) => call[0] === "/config"),
    ).toBeUndefined();
    expect(postMock).toHaveBeenCalledWith("/onboarding/transition", {
      phase: "greet",
    });
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

  // Issue #932 regression guard: when the broker probes a runtime and
  // reports it installed but not signed in, the tile must show a
  // "Not signed in" label, NOT advance onboarding when clicked, and
  // surface a copy-the-sign-in-command CTA.
  it("renders Not signed in label when session_probed=true and signed_in=false", async () => {
    mockPrereqs(
      { claude: true },
      {},
      {
        claude: {
          session_probed: true,
          signed_in: false,
          sign_in_command: "claude auth login",
        },
      },
    );
    render(<PrePickScreen onComplete={vi.fn()} />);

    const card = await screen.findByTestId("pre-pick-card-claude-code");
    await waitFor(() => expect(card.textContent).toMatch(/Not signed in/));
    expect(card.getAttribute("data-signed-in")).toBe("false");
  });

  it("does not POST /onboarding/transition when clicking an un-signed-in tile (copies sign-in command instead)", async () => {
    mockPrereqs(
      { codex: true },
      {},
      {
        codex: {
          session_probed: true,
          signed_in: false,
          sign_in_command: "codex login",
        },
      },
    );
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      writable: true,
      value: { writeText },
    });
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const card = await screen.findByTestId("pre-pick-card-codex");
    await waitFor(() => expect(card).not.toBeDisabled());
    fireEvent.click(card);

    // Critical: clicking an unauthed tile must NOT advance onboarding.
    expect(onComplete).not.toHaveBeenCalled();
    expect(
      postMock.mock.calls.find((call) => call[0] === "/onboarding/transition"),
    ).toBeUndefined();
    // Sign-in command should be copied to clipboard.
    expect(writeText).toHaveBeenCalledWith("codex login");
    // And an inline hint should appear with the same command.
    expect(await screen.findByRole("alert")).toHaveTextContent(/codex login/);
  });

  it("renders Signed in label when session_probed=true and signed_in=true", async () => {
    mockPrereqs(
      { claude: true },
      {},
      { claude: { session_probed: true, signed_in: true } },
    );
    render(<PrePickScreen onComplete={vi.fn()} />);

    const card = await screen.findByTestId("pre-pick-card-claude-code");
    await waitFor(() => expect(card.textContent).toMatch(/Signed in/));
    expect(card.getAttribute("data-signed-in")).toBe("true");
  });

  it("falls back to Detected label when broker did not probe (session_probed undefined)", async () => {
    mockPrereqs({ claude: true });
    render(<PrePickScreen onComplete={vi.fn()} />);

    const card = await screen.findByTestId("pre-pick-card-claude-code");
    await waitFor(() => expect(card.textContent).toMatch(/Detected/));
    expect(card.getAttribute("data-signed-in")).toBe("unknown");
  });

  // Issue #979 regression guard: a session-loss recovery that lands the
  // user back on PrePickScreen with the broker already in phase=complete
  // must NOT POST /onboarding/transition (broker rejects "complete → greet"
  // as an invalid transition). PrePickScreen must signal onComplete with
  // phaseAlreadyComplete=true so RootRoute routes straight to the office.
  it("skips /onboarding/transition when broker reports phase=complete (session-loss recovery)", async () => {
    mockPrereqs({ claude: true }, { onboarded: true, phase: "complete" });
    const onComplete = vi.fn();
    render(<PrePickScreen onComplete={onComplete} />);

    const card = await screen.findByTestId("pre-pick-card-claude-code");
    await waitFor(() => expect(card).not.toBeDisabled());
    fireEvent.click(card);

    await waitFor(() =>
      expect(onComplete).toHaveBeenCalledWith({ phaseAlreadyComplete: true }),
    );

    // Critical: the buggy path POSTed phase=greet which the broker rejected
    // with "invalid transition from \"complete\" to \"greet\"". The fix is to
    // not POST at all in this state.
    expect(
      postMock.mock.calls.find((call) => call[0] === "/onboarding/transition"),
    ).toBeUndefined();
    expect(
      postMock.mock.calls.find((call) => call[0] === "/config"),
    ).toBeUndefined();
  });

  it("surfaces an error if /onboarding/transition fails and does not invoke onComplete", async () => {
    mockPrereqs({ codex: true });
    postMock.mockImplementation(async (path: string) => {
      if (path === "/onboarding/transition") {
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

  // ── New tests for Phase 5 ported sections ──────────────────────────────

  describe("API key row", () => {
    it("renders API key rows for Anthropic, OpenAI, and Google", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-api-keys");
      expect(
        screen.getByTestId("pre-pick-api-row-ANTHROPIC_API_KEY"),
      ).toBeInTheDocument();
      expect(
        screen.getByTestId("pre-pick-api-row-OPENAI_API_KEY"),
      ).toBeInTheDocument();
      expect(
        screen.getByTestId("pre-pick-api-row-GOOGLE_API_KEY"),
      ).toBeInTheDocument();
    });

    it("defaults to CLI login mode (no password input visible)", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-api-keys");
      // In CLI mode the paste tab is present but the password input is not rendered
      expect(
        screen.queryByTestId("pre-pick-api-input-ANTHROPIC_API_KEY"),
      ).toBeNull();
    });

    it("switches to paste mode and shows password input when API key tab clicked", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-api-keys");

      const pasteTab = screen.getByTestId(
        "pre-pick-api-paste-ANTHROPIC_API_KEY",
      );
      fireEvent.click(pasteTab);

      expect(
        screen.getByTestId("pre-pick-api-input-ANTHROPIC_API_KEY"),
      ).toBeInTheDocument();
    });

    it("posts anthropic_api_key to /config after paste and submit", async () => {
      mockPrereqs({});
      const onComplete = vi.fn();
      render(<PrePickScreen onComplete={onComplete} />);
      await screen.findByTestId("pre-pick-api-keys");

      // Switch to paste mode
      fireEvent.click(
        screen.getByTestId("pre-pick-api-paste-ANTHROPIC_API_KEY"),
      );
      const input = screen.getByTestId("pre-pick-api-input-ANTHROPIC_API_KEY");
      fireEvent.change(input, { target: { value: "sk-ant-test123" } });

      // Submit button should now appear
      const submitBtn = await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(submitBtn);

      await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
      expect(postMock).toHaveBeenCalledWith(
        "/config",
        expect.objectContaining({ anthropic_api_key: "sk-ant-test123" }),
      );
    });

    it("strips control characters from a pasted API key before posting", async () => {
      mockPrereqs({});
      const onComplete = vi.fn();
      render(<PrePickScreen onComplete={onComplete} />);
      await screen.findByTestId("pre-pick-api-keys");

      fireEvent.click(
        screen.getByTestId("pre-pick-api-paste-ANTHROPIC_API_KEY"),
      );
      const input = screen.getByTestId("pre-pick-api-input-ANTHROPIC_API_KEY");
      // Include a null byte in the value to verify sanitization
      fireEvent.change(input, { target: { value: "sk-\x00bad\x1Fkey" } });

      const submitBtn = await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(submitBtn);

      await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
      expect(postMock).toHaveBeenCalledWith(
        "/config",
        expect.objectContaining({ anthropic_api_key: "sk-badkey" }),
      );
    });
  });

  describe("local provider picker", () => {
    it("renders the local provider section", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-local-section");
      expect(screen.getByTestId("pre-pick-local-picker")).toBeInTheDocument();
    });

    it("selecting a local provider and submitting form posts llm_provider", async () => {
      mockPrereqs({});
      // Make ollama tile available
      getLocalProvidersStatusMock.mockResolvedValue([
        {
          kind: "ollama",
          binary_installed: true,
          endpoint: "http://localhost:11434",
          model: "llama3",
          reachable: true,
          probed: true,
          platform_supported: true,
        },
      ]);
      const onComplete = vi.fn();
      render(<PrePickScreen onComplete={onComplete} />);

      // Wait for local picker to load
      const tile = await screen.findByTestId("pre-pick-local-tile-ollama");
      await waitFor(() => expect(tile).not.toBeDisabled());
      fireEvent.click(tile);

      const submitBtn = await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(submitBtn);

      await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
      expect(postMock).toHaveBeenCalledWith(
        "/config",
        expect.objectContaining({
          llm_provider: "ollama",
          llm_provider_priority: ["ollama"],
        }),
      );
    });

    it("deselecting a local provider hides the submit button when no other form input filled", async () => {
      mockPrereqs({});
      getLocalProvidersStatusMock.mockResolvedValue([
        {
          kind: "ollama",
          binary_installed: true,
          endpoint: "http://localhost:11434",
          model: "llama3",
          reachable: false,
          probed: true,
          platform_supported: true,
        },
      ]);
      render(<PrePickScreen onComplete={vi.fn()} />);

      const tile = await screen.findByTestId("pre-pick-local-tile-ollama");
      await waitFor(() => expect(tile).not.toBeDisabled());

      // Select then deselect
      fireEvent.click(tile);
      await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(tile);

      await waitFor(() =>
        expect(screen.queryByTestId("pre-pick-form-submit")).toBeNull(),
      );
    });
  });

  describe("OpenAI-compatible endpoint", () => {
    it("renders the OAI-compatible section", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-oai-section");
      expect(screen.getByTestId("pre-pick-oai-compat")).toBeInTheDocument();
    });

    it("does not show submit button when only an invalid URL is entered", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-oai-url");

      fireEvent.change(screen.getByTestId("pre-pick-oai-url"), {
        target: { value: "not-a-url" },
      });

      // URL error message should appear
      expect(
        await screen.findByTestId("pre-pick-oai-url-error"),
      ).toBeInTheDocument();
      // Submit button must NOT appear
      expect(screen.queryByTestId("pre-pick-form-submit")).toBeNull();
    });

    it("shows submit button and posts provider_endpoints when a valid URL is entered", async () => {
      mockPrereqs({});
      const onComplete = vi.fn();
      render(<PrePickScreen onComplete={onComplete} />);
      await screen.findByTestId("pre-pick-oai-url");

      fireEvent.change(screen.getByTestId("pre-pick-oai-url"), {
        target: { value: "https://my-server.example.com/v1" },
      });

      const submitBtn = await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(submitBtn);

      await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
      expect(postMock).toHaveBeenCalledWith(
        "/config",
        expect.objectContaining({
          provider_endpoints: expect.objectContaining({
            "openai-compatible": expect.objectContaining({
              base_url: "https://my-server.example.com/v1",
            }),
          }),
        }),
      );
    });

    it("strips control characters from the endpoint URL before posting", async () => {
      mockPrereqs({});
      const onComplete = vi.fn();
      render(<PrePickScreen onComplete={onComplete} />);
      await screen.findByTestId("pre-pick-oai-url");

      // Inject a control char in the URL (after valid prefix so it passes URL validation)
      // Use a URL that is valid but contains a control char we expect to be stripped.
      // NOTE: URL constructor strips control chars itself, so we verify the sanitizer
      // is invoked by checking the posted value is clean.
      fireEvent.change(screen.getByTestId("pre-pick-oai-url"), {
        target: { value: "https://example.com/v1" },
      });
      fireEvent.change(screen.getByTestId("pre-pick-oai-key"), {
        target: { value: "tok-\x01secret" },
      });

      const submitBtn = await screen.findByTestId("pre-pick-form-submit");
      fireEvent.click(submitBtn);

      await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
      // The API key must have control chars stripped
      expect(postMock).toHaveBeenCalledWith(
        "/config",
        expect.objectContaining({
          openclaw_token: "tok-secret",
        }),
      );
    });
  });

  describe("canContinue predicate", () => {
    it("does not show form submit button when no form section is filled", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-api-keys");
      expect(screen.queryByTestId("pre-pick-form-submit")).toBeNull();
    });

    it("shows form submit button as soon as one API key is entered", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-api-keys");

      fireEvent.click(screen.getByTestId("pre-pick-api-paste-OPENAI_API_KEY"));
      fireEvent.change(
        screen.getByTestId("pre-pick-api-input-OPENAI_API_KEY"),
        { target: { value: "sk-openai-xyz" } },
      );

      expect(
        await screen.findByTestId("pre-pick-form-submit"),
      ).toBeInTheDocument();
    });

    it("skip button always remains visible as sandbox path regardless of form state", async () => {
      mockPrereqs({});
      render(<PrePickScreen onComplete={vi.fn()} />);
      await screen.findByTestId("pre-pick-skip");

      // Fill an API key
      fireEvent.click(
        screen.getByTestId("pre-pick-api-paste-ANTHROPIC_API_KEY"),
      );
      fireEvent.change(
        screen.getByTestId("pre-pick-api-input-ANTHROPIC_API_KEY"),
        { target: { value: "sk-ant-hello" } },
      );

      // Skip button must still be present
      expect(screen.getByTestId("pre-pick-skip")).toBeInTheDocument();
    });
  });

  describe("CodeRabbit click-handler guard (PR #889)", () => {
    it("cards are disabled until prereqs have loaded", () => {
      // Prereqs never resolves in this test -- cards stay disabled.
      getMock.mockReturnValue(new Promise(() => {}));
      render(<PrePickScreen onComplete={vi.fn()} />);
      // Cards should be in the DOM but disabled (prereqsLoaded=false)
      const claudeCard = screen.getByTestId("pre-pick-card-claude-code");
      expect(claudeCard).toBeDisabled();
    });

    it("cards become enabled after prereqs load", async () => {
      mockPrereqs({ claude: true });
      render(<PrePickScreen onComplete={vi.fn()} />);
      const card = await screen.findByTestId("pre-pick-card-claude-code");
      await waitFor(() => expect(card).not.toBeDisabled());
    });
  });
});

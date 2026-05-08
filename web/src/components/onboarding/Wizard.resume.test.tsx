import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAppStore } from "../../stores/app";
import { Wizard } from "./Wizard";
import {
  CURRENT_VERSION,
  LOCAL_STORAGE_KEY,
  STALE_BANNER_KEY,
} from "./wizard/onboardingDraft";

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    get: vi.fn().mockResolvedValue({ templates: [], prereqs: [] }),
    post: vi.fn().mockResolvedValue({}),
    getConfig: vi.fn().mockResolvedValue({}),
  };
});

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.history.replaceState({}, "", "/");
  useAppStore.setState({ onboardingComplete: false });
});

afterEach(() => {
  cleanup();
});

function seedDraft(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  const draft = {
    version: CURRENT_VERSION,
    step: "identity",
    selectedBlueprint: null,
    company: "Acme",
    description: "We do things",
    priority: "growth",
    runtimePriority: ["Claude Code"],
    localProvider: "",
    selectedTaskTemplate: null,
    taskText: "",
    savedAt: new Date().toISOString(),
    ...overrides,
  };
  window.localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(draft));
  return draft;
}

describe("Wizard resume", () => {
  it("restores wizard state from a saved draft on mount", async () => {
    seedDraft({ step: "identity" });

    render(<Wizard onComplete={vi.fn()} />);

    // Identity step renders with company prefilled
    const company = (await waitFor(() =>
      screen.getByLabelText(/Office name/i),
    )) as HTMLInputElement;
    expect(company.value).toBe("Acme");

    const description = screen.getByLabelText(
      /Short description/i,
    ) as HTMLInputElement;
    expect(description.value).toBe("We do things");

    const priority = screen.getByLabelText(/Top priority/i) as HTMLInputElement;
    expect(priority.value).toBe("growth");

    expect(screen.getByTestId("onboarding-resume-banner")).toBeInTheDocument();
  });

  it("Reset link on welcome screen wipes state and clears draft", async () => {
    seedDraft({ step: "welcome", company: "Acme" });

    render(<Wizard onComplete={vi.fn()} />);

    const trigger = await waitFor(() =>
      screen.getByTestId("welcome-reset-trigger"),
    );
    fireEvent.click(trigger);
    fireEvent.click(screen.getByTestId("welcome-reset-confirm"));

    // Draft cleared synchronously
    expect(window.localStorage.getItem(LOCAL_STORAGE_KEY)).toBeNull();
    // Banner gone, Reset link gone (no draft)
    expect(screen.queryByTestId("onboarding-resume-banner")).toBeNull();
    expect(screen.queryByTestId("welcome-reset-trigger")).toBeNull();
  });

  it("stale-draft banner appears once on the next mount and dismisses", async () => {
    window.sessionStorage.setItem(STALE_BANNER_KEY, "42");

    render(<Wizard onComplete={vi.fn()} />);

    const banner = await waitFor(() =>
      screen.getByTestId("onboarding-stale-banner"),
    );
    expect(banner).toHaveTextContent(/42 days ago/);

    fireEvent.click(screen.getByTestId("onboarding-stale-dismiss"));
    expect(screen.queryByTestId("onboarding-stale-banner")).toBeNull();

    // The flag was consumed at mount, so it would not show again on
    // a hypothetical remount.
    expect(window.sessionStorage.getItem(STALE_BANNER_KEY)).toBeNull();
  });

  it("does not show the resume banner when there is no draft", () => {
    render(<Wizard onComplete={vi.fn()} />);
    expect(screen.queryByTestId("onboarding-resume-banner")).toBeNull();
  });
});

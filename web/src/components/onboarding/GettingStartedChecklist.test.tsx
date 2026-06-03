/**
 * GettingStartedChecklist tests
 *
 * 1. Hides while the state query is still loading.
 * 2. Hides when checklist_dismissed is true.
 * 3. Hides when every known item is done.
 * 4. Renders the five WUPHF-copy labels + a count.
 * 5. Renders a green tick only for done items.
 * 6. External links open in a new tab with rel="noopener noreferrer".
 * 7. Clicking an item action marks it done (POST .../done).
 * 8. Clicking an internal-nav action marks it done AND navigates.
 * 9. The dismiss button POSTs .../dismiss.
 */

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, get: vi.fn(), post: vi.fn() };
});

vi.mock("../../lib/router", () => ({
  router: { navigate: vi.fn() },
}));

import { get, post } from "../../api/client";
import { router } from "../../lib/router";
import {
  DISCORD_INVITE_URL,
  GettingStartedChecklist,
  GITHUB_REPO_URL,
} from "./GettingStartedChecklist";
import type { OnboardingChecklistItem } from "./types";

const getMock = vi.mocked(get);
const postMock = vi.mocked(post);
const navigateMock = vi.mocked(router.navigate);

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function makeChecklist(
  overrides: Partial<Record<string, boolean>> = {},
): OnboardingChecklistItem[] {
  return [
    { id: "pick_team", done: overrides.pick_team ?? false },
    { id: "second_key", done: overrides.second_key ?? false },
    { id: "github_repo", done: overrides.github_repo ?? false },
    { id: "github_star", done: overrides.github_star ?? false },
    { id: "discord", done: overrides.discord ?? false },
  ];
}

beforeEach(() => {
  getMock.mockReset();
  postMock.mockReset();
  navigateMock.mockReset();
  postMock.mockResolvedValue({ status: "ok" });
});

afterEach(() => {
  cleanup();
});

describe("GettingStartedChecklist", () => {
  it("renders nothing while loading", () => {
    getMock.mockReturnValue(new Promise(() => {}));
    render(<GettingStartedChecklist />, { wrapper });
    expect(
      screen.queryByText("Settle into your office"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing when dismissed", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: true,
    });
    render(<GettingStartedChecklist />, { wrapper });
    await new Promise((r) => setTimeout(r, 30));
    expect(
      screen.queryByText("Settle into your office"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing when every item is done", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist({
        pick_team: true,
        second_key: true,
        github_repo: true,
        github_star: true,
        discord: true,
      }),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });
    await new Promise((r) => setTimeout(r, 30));
    expect(
      screen.queryByText("Settle into your office"),
    ).not.toBeInTheDocument();
  });

  it("renders the five WUPHF labels and a count", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist({ pick_team: true }),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    expect(await screen.findByText("Settle into your office")).toBeVisible();
    expect(screen.getByText("Pick or trim your team")).toBeVisible();
    expect(
      screen.getByText("Add a second runtime so the office never stalls"),
    ).toBeVisible();
    expect(
      screen.getByText("Connect a GitHub repo and bring real work in"),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Star WUPHF on GitHub (Michael would be proud, probably)",
      ),
    ).toBeVisible();
    expect(
      screen.getByText("Join the Discord and meet other founders"),
    ).toBeVisible();
    expect(screen.getByText("1/5")).toBeVisible();
  });

  it("announces completion to screen readers, not via strikethrough alone", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist({ pick_team: true }),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    // The done item's row carries a visually-hidden "Done." so SR users hear
    // the state; not-done rows do not.
    const doneRow = (await screen.findByText("Pick or trim your team")).closest(
      "li",
    );
    expect(doneRow?.querySelector(".sr-only")?.textContent).toBe("Done. ");

    const notDoneRow = screen
      .getByText("Join the Discord and meet other founders")
      .closest("li");
    expect(notDoneRow?.querySelector(".sr-only")).toBeNull();
  });

  it("external links open in a new tab with rel=noopener noreferrer", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    // github_star is the external GitHub link; github_repo is internal nav
    // (covered separately below).
    const starAnchor = (
      await screen.findByText(
        "Star WUPHF on GitHub (Michael would be proud, probably)",
      )
    )
      .closest("li")
      ?.querySelector("a");
    expect(starAnchor).toHaveAttribute("href", GITHUB_REPO_URL);
    expect(starAnchor).toHaveAttribute("target", "_blank");
    expect(starAnchor).toHaveAttribute("rel", "noopener noreferrer");

    const discordLink = screen
      .getByText("Join the Discord and meet other founders")
      .closest("li")
      ?.querySelector("a");
    expect(discordLink).toHaveAttribute("href", DISCORD_INVITE_URL);
    expect(discordLink).toHaveAttribute("target", "_blank");
    expect(discordLink).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("Connect a GitHub repo is internal nav to the how-to page, not an external link", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    const repoRow = (
      await screen.findByText("Connect a GitHub repo and bring real work in")
    ).closest("li");
    // It must NOT be an external anchor (would dump the user on the source
    // repo); it is an internal-nav button to the seeded how-to wiki page.
    expect(repoRow?.querySelector("a")).toBeNull();
    const button = repoRow?.querySelector("button");
    if (!button) throw new Error("github_repo nav button not found");

    fireEvent.click(button);
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/wiki/$",
      params: { _splat: "team/getting-started/connecting-your-work" },
    });
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith(
        "/onboarding/checklist/github_repo/done",
      ),
    );
  });

  it("marks an external item done when its link is clicked", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    const anchor = (
      await screen.findByText("Join the Discord and meet other founders")
    )
      .closest("li")
      ?.querySelector("a");
    if (!anchor) throw new Error("discord anchor not found");
    fireEvent.click(anchor);

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith(
        "/onboarding/checklist/discord/done",
      ),
    );
  });

  it("marks an internal-nav item done and navigates", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    const button = (await screen.findByText("Open Settings")).closest("button");
    if (!button) throw new Error("settings button not found");
    fireEvent.click(button);

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith(
        "/onboarding/checklist/second_key/done",
      ),
    );
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/apps/$appId",
      params: { appId: "settings" },
    });
  });

  it("dismisses the panel when the settle-in button is clicked", async () => {
    getMock.mockResolvedValue({
      checklist: makeChecklist(),
      checklist_dismissed: false,
    });
    render(<GettingStartedChecklist />, { wrapper });

    fireEvent.click(await screen.findByText("I am settled in"));

    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith("/onboarding/checklist/dismiss"),
    );
  });
});

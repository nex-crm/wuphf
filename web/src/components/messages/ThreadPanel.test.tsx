import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Message, OfficeMember } from "../../api/client";
import { useAppStore } from "../../stores/app";

const officeMembers: OfficeMember[] = [
  {
    slug: "ceo",
    name: "Carmen",
    role: "CEO",
    emoji: "👑",
    built_in: true,
  } as OfficeMember,
  {
    slug: "pm",
    name: "Mara",
    role: "Product",
    emoji: "📋",
    built_in: false,
  } as OfficeMember,
];

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: () => ({ data: officeMembers, isLoading: false }),
}));

vi.mock("../../hooks/useMessages", () => ({
  useThreadMessages: () => ({
    data: [
      {
        id: "thread-1",
        from: "ceo",
        content: "Parent message",
        channel: "general",
      } as Message,
    ],
    isLoading: false,
  }),
}));

vi.mock("../../hooks/useCommands", async () => {
  const actual = await vi.importActual<typeof import("../../hooks/useCommands")>(
    "../../hooks/useCommands",
  );
  return {
    ...actual,
    useCommands: () => actual.FALLBACK_SLASH_COMMANDS,
  };
});

vi.mock("./MessageBubble", () => ({
  MessageBubble: ({ message }: { message: Message }) => (
    <div data-testid="bubble">{message.content}</div>
  ),
}));

vi.mock("../../api/client", async () => {
  const actual = await vi.importActual<typeof import("../../api/client")>(
    "../../api/client",
  );
  return {
    ...actual,
    getConfig: vi.fn().mockResolvedValue({ team_lead_slug: "ceo" }),
    postMessage: vi.fn().mockResolvedValue({ id: "reply-1" }),
  };
});

import { ThreadPanel } from "./ThreadPanel";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  useAppStore.getState().setActiveThread({
    id: "thread-1",
    channelSlug: "general",
  });
});

afterEach(() => {
  useAppStore.getState().setActiveThread(null);
});

describe("ThreadPanel autocomplete popovers", () => {
  it("opens the slash-command popover when the user types '/'", () => {
    render(wrap(<ThreadPanel />));

    // Before typing, no popover.
    expect(document.querySelector(".autocomplete.open")).toBeNull();

    const textarea = screen.getByPlaceholderText(
      "Reply to thread…",
    ) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "/" } });

    const popover = document.querySelector(".autocomplete.open");
    expect(popover).not.toBeNull();
    // Renders at least one slash command (e.g. /clear from FALLBACK).
    expect(popover?.textContent).toMatch(/\//);
  });

  it("opens the @-mention popover when the user types '@'", () => {
    render(wrap(<ThreadPanel />));

    const textarea = screen.getByPlaceholderText(
      "Reply to thread…",
    ) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "@" } });

    const popover = document.querySelector(".autocomplete.open");
    expect(popover).not.toBeNull();
    // Should at least surface @all and a non-human member.
    expect(popover?.textContent).toContain("@all");
    expect(popover?.textContent).toContain("@ceo");
  });

  it("filters @-mentions by partial match on the query", () => {
    render(wrap(<ThreadPanel />));

    const textarea = screen.getByPlaceholderText(
      "Reply to thread…",
    ) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "@ce" } });

    const popover = document.querySelector(".autocomplete.open");
    expect(popover).not.toBeNull();
    expect(popover?.textContent).toContain("@ceo");
    expect(popover?.textContent).not.toContain("@pm");
  });

  it("hides the popover once the trigger no longer applies (text after slash)", () => {
    render(wrap(<ThreadPanel />));

    const textarea = screen.getByPlaceholderText(
      "Reply to thread…",
    ) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "/clear" } });
    expect(document.querySelector(".autocomplete.open")).not.toBeNull();

    fireEvent.change(textarea, { target: { value: "/clear extra" } });
    expect(document.querySelector(".autocomplete.open")).toBeNull();
  });
});

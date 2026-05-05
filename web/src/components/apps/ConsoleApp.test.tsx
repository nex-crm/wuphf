import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Message, SlashCommandDescriptor } from "../../api/client";
import { FALLBACK_SLASH_COMMANDS } from "../../hooks/useCommands";
import { useMessages } from "../../hooks/useMessages";
import { useFallbackChannelSlug } from "../../routes/useCurrentRoute";
import { __test__, ConsoleApp } from "./ConsoleApp";

// ConsoleApp reads its channel from useFallbackChannelSlug (URL channel
// first, then last-visited fallback). Mock it so tests can swap the
// active channel without rendering inside a RouterProvider — useMatches
// would otherwise throw outside the router context.
vi.mock("../../routes/useCurrentRoute", () => ({
  useFallbackChannelSlug: vi.fn(),
}));

const mockUseFallbackChannelSlug = vi.mocked(useFallbackChannelSlug);

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    fetchCommands: vi.fn().mockResolvedValue([]),
    getRequests: vi.fn().mockResolvedValue({ requests: [] }),
  };
});

vi.mock("../../api/tasks", () => ({
  getOfficeTasks: vi.fn().mockResolvedValue({ tasks: [] }),
}));

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: vi.fn(() => ({ data: [] })),
}));

vi.mock("../../hooks/useMessages", () => ({
  useMessages: vi.fn(() => ({ data: [] })),
}));

const {
  activeTaskCount,
  commandRowsFromRegistry,
  openRequestCount,
  terminalLineFromMessage,
} = __test__;

function wrap(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockUseFallbackChannelSlug.mockReturnValue("general");
});

describe("ConsoleApp helpers", () => {
  it("formats message lines for the terminal mirror", () => {
    const line = terminalLineFromMessage({
      from: "ceo",
      content: "First line\n\nsecond line",
      timestamp: "2026-05-03T10:05:00Z",
    } as Message);

    expect(line.speaker).toBe("@ceo");
    expect(line.content).toBe("First line second line");
    expect(line.time).toMatch(/\d{1,2}:\d{2}/);
  });

  it("uses a stable fallback for invalid timestamps and empty content", () => {
    const line = terminalLineFromMessage({
      from: "you",
      content: "   ",
      timestamp: "not-a-date",
    } as Message);

    expect(line).toMatchObject({
      time: "--:--",
      speaker: "you",
      content: "(empty)",
    });
  });

  it("maps broker commands to insertable command rows", () => {
    const rows = commandRowsFromRegistry([
      { name: "tasks", description: "Open task board", webSupported: true },
      { name: "doctor", description: "Run checks", webSupported: false },
      {
        name: "agent",
        description: "Agent commands",
        webSupported: false,
      },
    ] satisfies SlashCommandDescriptor[]);

    expect(rows).toEqual([
      {
        name: "/tasks",
        description: "Open task board",
        webSupported: true,
      },
      {
        name: "/doctor",
        description: "Run checks",
        webSupported: false,
      },
      {
        name: "/agent",
        description: "Agent commands",
        webSupported: false,
      },
    ]);
  });

  it("falls back to bundled slash commands when registry data is empty or missing", () => {
    const expectedRows = FALLBACK_SLASH_COMMANDS.map((command) => ({
      name: command.name,
      description: command.desc,
      webSupported: true,
    }));

    expect(commandRowsFromRegistry(undefined)).toEqual(expectedRows);
    expect(commandRowsFromRegistry([])).toEqual(expectedRows);
    for (const row of commandRowsFromRegistry(undefined)) {
      expect(row.name).toMatch(/^\/\S+/);
      expect(row.description).toBeTruthy();
      expect(row.webSupported).toBe(true);
    }
  });

  it("counts active tasks and open requests", () => {
    expect(
      activeTaskCount([
        { status: "open" },
        { status: "in_progress" },
        { status: "done" },
        { status: "cancelled" },
      ]),
    ).toBe(2);

    expect(
      openRequestCount([
        { status: "" },
        { status: "pending" },
        { status: "answered" },
      ]),
    ).toBe(2);
  });
});

describe("<ConsoleApp>", () => {
  it("renders the active channel from the URL", () => {
    render(wrap(<ConsoleApp />));

    expect(screen.getAllByText("#general").length).toBeGreaterThan(1);
    expect(screen.getByText("wuphf:general$")).toBeInTheDocument();
    expect(vi.mocked(useMessages)).toHaveBeenCalledWith("general");
  });

  it("clears local prompt echoes when switching channels", async () => {
    const { rerender } = render(wrap(<ConsoleApp />));

    const input = screen.getByTestId("console-input");
    fireEvent.change(input, { target: { value: "/ask launch plan" } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    expect(screen.getByText("/ask launch plan")).toBeInTheDocument();

    // RTL's `rerender` already wraps the render in `act` internally,
    // so an explicit `act(() => ...)` here would be redundant.
    mockUseFallbackChannelSlug.mockReturnValue("launch");
    rerender(wrap(<ConsoleApp />));

    await waitFor(() => {
      expect(screen.queryByText("/ask launch plan")).not.toBeInTheDocument();
    });
    expect(screen.getByText("wuphf:launch$")).toBeInTheDocument();
  });
});

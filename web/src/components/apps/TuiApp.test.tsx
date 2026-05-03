import { describe, expect, it } from "vitest";

import type { Message, SlashCommandDescriptor } from "../../api/client";
import { __test__ } from "./TuiApp";

const {
  activeTaskCount,
  commandRowsFromRegistry,
  openRequestCount,
  terminalLineFromMessage,
} = __test__;

describe("TuiApp helpers", () => {
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

  it("maps broker commands to web rows with app jump targets", () => {
    const rows = commandRowsFromRegistry([
      { name: "tasks", description: "Open task board", webSupported: true },
      { name: "doctor", description: "Run checks", webSupported: false },
    ] satisfies SlashCommandDescriptor[]);

    expect(rows).toEqual([
      {
        name: "/tasks",
        description: "Open task board",
        webSupported: true,
        appTarget: "tasks",
      },
      {
        name: "/doctor",
        description: "Run checks",
        webSupported: false,
        appTarget: undefined,
      },
    ]);
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

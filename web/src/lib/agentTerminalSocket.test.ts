import { describe, expect, it, vi } from "vitest";

import { agentTerminalPath, connectAgentTerminal } from "./agentTerminalSocket";

describe("agentTerminalPath", () => {
  it("builds an agent terminal path with optional task scope", () => {
    expect(agentTerminalPath("builder one")).toBe(
      "/terminal/agents/builder%20one",
    );
    expect(agentTerminalPath("builder", "task 7")).toBe(
      "/terminal/agents/builder?task=task+7",
    );
  });
});

describe("connectAgentTerminal", () => {
  it("forwards websocket data and resize messages", () => {
    const sent: string[] = [];
    const sockets: Array<{
      onopen: ((event: Event) => void) | null;
      onmessage: ((event: MessageEvent) => void) | null;
      onerror: ((event: Event) => void) | null;
      onclose: ((event: CloseEvent) => void) | null;
      send: (data: string) => void;
      close: () => void;
    }> = [];
    const onData = vi.fn();
    const onOpen = vi.fn();
    const onClose = vi.fn();

    const terminal = connectAgentTerminal(
      "builder",
      "task-7",
      { onData, onOpen, onClose },
      {
        socketFactory: (url) => {
          expect(url).toContain("/terminal/agents/builder?task=task-7");
          const socket = {
            onopen: null,
            onmessage: null,
            onerror: null,
            onclose: null,
            send: (data: string) => sent.push(data),
            close: vi.fn(),
          };
          sockets.push(socket);
          return socket;
        },
      },
    );

    const [socket] = sockets;
    socket?.onopen?.(new Event("open"));
    socket?.onmessage?.(new MessageEvent("message", { data: "hello\r\n" }));
    terminal.resize(120, 40);
    terminal.close();

    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onData).toHaveBeenCalledWith("hello\r\n");
    expect(sent).toEqual([
      JSON.stringify({ type: "resize", cols: 120, rows: 40 }),
    ]);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("queues resize messages until the websocket opens", () => {
    const sent: string[] = [];
    const sockets: Array<{
      onopen: ((event: Event) => void) | null;
      onmessage: ((event: MessageEvent) => void) | null;
      onerror: ((event: Event) => void) | null;
      onclose: ((event: CloseEvent) => void) | null;
      send: (data: string) => void;
      close: () => void;
    }> = [];

    const terminal = connectAgentTerminal(
      "builder",
      "task-7",
      {},
      {
        socketFactory: () => {
          const socket = {
            onopen: null,
            onmessage: null,
            onerror: null,
            onclose: null,
            send: (data: string) => sent.push(data),
            close: vi.fn(),
          };
          sockets.push(socket);
          return socket;
        },
      },
    );

    terminal.resize(80, 24);
    terminal.resize(120, 40);
    expect(sent).toEqual([]);

    const [socket] = sockets;
    socket?.onopen?.(new Event("open"));

    expect(sent).toEqual([
      JSON.stringify({ type: "resize", cols: 120, rows: 40 }),
    ]);
  });
});

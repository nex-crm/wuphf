import { useCallback, useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import { createTerminalWriteBuffer } from "../../lib/agentTerminalBuffer";
import {
  type AgentTerminalSocket,
  connectAgentTerminal,
} from "../../lib/agentTerminalSocket";

interface AgentTerminalProps {
  slug: string | null;
  taskId?: string | null;
  title?: string;
  emptyLabel?: string;
}

export function AgentTerminal({
  slug,
  taskId = null,
  title = "Live output",
  emptyLabel = "Waiting for output...",
}: AgentTerminalProps) {
  const terminalHostRef = useRef<HTMLDivElement>(null);
  const [connected, setConnected] = useState(false);
  const [hasOutput, setHasOutput] = useState(false);
  const connectedRef = useRef(false);
  const hasOutputRef = useRef(false);

  const setConnectionState = useCallback((next: boolean) => {
    if (connectedRef.current === next) return;
    connectedRef.current = next;
    setConnected(next);
  }, []);

  const setOutputState = useCallback((next: boolean) => {
    if (hasOutputRef.current === next) return;
    hasOutputRef.current = next;
    setHasOutput(next);
  }, []);

  useEffect(() => {
    const host = terminalHostRef.current;
    if (!(host && slug)) {
      setConnectionState(false);
      setOutputState(false);
      return;
    }

    setConnectionState(false);
    setOutputState(false);
    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: true,
      cursorBlink: false,
      disableStdin: true,
      fontFamily: "var(--font-mono)",
      fontSize: 11,
      lineHeight: 1.35,
      scrollback: 3000,
      theme: {
        background: "#0b0f14",
        foreground: "#d8dee9",
        cursor: "#d8dee9",
        selectionBackground: "#2d3748",
        black: "#0b0f14",
        blue: "#7aa2f7",
        cyan: "#7dcfff",
        green: "#9ece6a",
        magenta: "#bb9af7",
        red: "#f7768e",
        white: "#d8dee9",
        yellow: "#e0af68",
      },
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(host);
    fitAddon.fit();

    let terminalSocket: AgentTerminalSocket | null = null;
    function sendResize() {
      terminalSocket?.resize(terminal.cols, terminal.rows);
    }

    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            fitAddon.fit();
            sendResize();
          });
    resizeObserver?.observe(host);

    const buffer = createTerminalWriteBuffer((text) => terminal.write(text));
    terminalSocket = connectAgentTerminal(slug, taskId, {
      onOpen: () => setConnectionState(true),
      onData: (data) => {
        if (!data) return;
        setOutputState(true);
        buffer.enqueue(data);
      },
      onError: () => setConnectionState(false),
      onClose: () => setConnectionState(false),
    });
    sendResize();

    return () => {
      terminalSocket?.close();
      buffer.dispose();
      resizeObserver?.disconnect();
      terminal.dispose();
    };
  }, [setConnectionState, setOutputState, slug, taskId]);

  return (
    <div className="agent-terminal-shell">
      <div className="agent-terminal-header">
        <div className="agent-terminal-title">
          <span
            className={`status-dot ${connected ? "active pulse" : "lurking"}`}
          />
          <span>{title}</span>
        </div>
        <span className="agent-terminal-meta">
          {connected ? "live" : "idle"}
        </span>
      </div>
      <div className="agent-terminal-frame">
        {!hasOutput ? (
          <div className="agent-terminal-empty">{emptyLabel}</div>
        ) : null}
        <div
          className="agent-terminal-host"
          ref={terminalHostRef}
          role="log"
          aria-label={title}
        />
      </div>
    </div>
  );
}

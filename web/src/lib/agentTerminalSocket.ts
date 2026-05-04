import { websocketURL } from "../api/client";

export interface AgentTerminalSocketHandlers {
  onOpen?: () => void;
  onData?: (data: string) => void;
  onError?: () => void;
  onClose?: () => void;
}

export interface AgentTerminalSocket {
  close: () => void;
  resize: (cols: number, rows: number) => void;
}

export interface AgentTerminalSocketOptions {
  socketFactory?: (url: string) => WebSocketLike;
}

export interface WebSocketLike {
  binaryType?: BinaryType;
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  send: (data: string) => void;
  close: () => void;
}

export function agentTerminalPath(
  slug: string,
  taskId?: string | null,
): string {
  const params = new URLSearchParams();
  if (taskId?.trim()) params.set("task", taskId.trim());
  const qs = params.toString();
  return `/terminal/agents/${encodeURIComponent(slug)}${qs ? `?${qs}` : ""}`;
}

export function connectAgentTerminal(
  slug: string,
  taskId: string | null | undefined,
  handlers: AgentTerminalSocketHandlers,
  options: AgentTerminalSocketOptions = {},
): AgentTerminalSocket {
  const trimmedSlug = slug.trim();
  if (!trimmedSlug) {
    handlers.onClose?.();
    return noopSocket;
  }
  if (!(options.socketFactory || globalThis.WebSocket)) {
    handlers.onError?.();
    handlers.onClose?.();
    return noopSocket;
  }

  let closed = false;
  let opened = false;
  let pendingResize: { cols: number; rows: number } | null = null;
  let socket: WebSocketLike;
  try {
    socket = (options.socketFactory ?? defaultSocketFactory)(
      websocketURL(agentTerminalPath(trimmedSlug, taskId)),
    );
  } catch {
    handlers.onError?.();
    handlers.onClose?.();
    return noopSocket;
  }

  function sendResize(cols: number, rows: number): void {
    try {
      socket.send(JSON.stringify({ type: "resize", cols, rows }));
    } catch {
      handlers.onError?.();
    }
  }

  socket.onopen = (event) => {
    if (closed) return;
    opened = true;
    handlers.onOpen?.();
    if (pendingResize) {
      const { cols, rows } = pendingResize;
      pendingResize = null;
      sendResize(cols, rows);
    }
    void event;
  };
  socket.onmessage = (event) => {
    if (closed) return;
    if (typeof event.data === "string") {
      handlers.onData?.(event.data);
    }
  };
  socket.onerror = () => {
    if (!closed) handlers.onError?.();
  };
  socket.onclose = () => {
    if (closed) return;
    closed = true;
    handlers.onClose?.();
  };

  return {
    close: () => {
      if (closed) return;
      closed = true;
      socket.close();
      handlers.onClose?.();
    },
    resize: (cols, rows) => {
      if (closed) return;
      if (!opened) {
        pendingResize = { cols, rows };
        return;
      }
      sendResize(cols, rows);
    },
  };
}

const noopSocket: AgentTerminalSocket = {
  close: () => undefined,
  resize: () => undefined,
};

function defaultSocketFactory(url: string): WebSocketLike {
  return new WebSocket(url);
}

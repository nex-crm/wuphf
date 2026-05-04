import { sseURL } from "../api/client";

export interface AgentStreamHandlers {
  onOpen?: () => void;
  onLine?: (line: string) => void;
  onError?: () => void;
  onClose?: () => void;
}

export interface AgentStreamEventSource {
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent<string>) => void) | null;
  onerror: ((event: Event) => void) | null;
  // 0 = CONNECTING, 1 = OPEN, 2 = CLOSED. Some test factories omit it,
  // so the error path treats undefined as terminal instead of reconnectable.
  readyState?: number;
  close: () => void;
}

export type AgentStreamEventSourceFactory = (
  url: string,
) => AgentStreamEventSource;

export interface AgentStreamSubscription {
  close: () => void;
}

export interface AgentStreamSubscriptionOptions {
  eventSourceFactory?: AgentStreamEventSourceFactory;
}

export function agentStreamPath(slug: string): string {
  return `/agent-stream/${encodeURIComponent(slug)}`;
}

export function subscribeAgentStream(
  slug: string,
  handlers: AgentStreamHandlers,
  options: AgentStreamSubscriptionOptions = {},
): AgentStreamSubscription {
  const trimmedSlug = slug.trim();
  if (!trimmedSlug) {
    handlers.onClose?.();
    return { close: () => undefined };
  }

  if (!(options.eventSourceFactory || globalThis.EventSource)) {
    handlers.onError?.();
    handlers.onClose?.();
    return { close: () => undefined };
  }

  const createSource = options.eventSourceFactory ?? defaultEventSourceFactory;
  const source = createSource(sseURL(agentStreamPath(trimmedSlug)));
  let closed = false;

  source.onopen = () => {
    if (!closed) handlers.onOpen?.();
  };

  source.onmessage = (event) => {
    if (!closed) handlers.onLine?.(event.data);
  };

  source.onerror = () => {
    if (closed) return;
    handlers.onError?.();
    if (source.readyState === undefined || source.readyState === 2) {
      close();
    }
  };

  function close() {
    if (closed) return;
    closed = true;
    source.close();
    handlers.onClose?.();
  }

  return { close };
}

function defaultEventSourceFactory(url: string): AgentStreamEventSource {
  const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
  if (!ES) {
    throw new Error("EventSource is not available in this environment");
  }
  return new ES(url);
}

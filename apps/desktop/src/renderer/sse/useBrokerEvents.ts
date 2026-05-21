import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import {
  type ApprovalStreamEvent,
  type ApprovalStreamEventKind,
  type ThreadStreamEvent,
  type ThreadStreamEventKind,
  validateApprovalStreamEvent,
  validateThreadStreamEvent,
} from "@wuphf/protocol/browser";
import {
  createContext,
  createElement,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import type { ApiToken, BrokerUrl } from "../bootstrap/types.ts";
import { useBrokerBootstrap } from "../bootstrap/useBrokerBootstrap.ts";
import { approvalQueryKeys, threadQueryKeys } from "../query/queries.ts";

type FetchLike = typeof fetch;
type StreamStateSink = (state: BrokerStreamState) => void;

export interface SseFrame {
  readonly event: string;
  readonly data: string;
}

export type BrokerStreamState =
  | { readonly status: "idle" }
  | { readonly status: "connecting"; readonly attempt: number }
  | { readonly status: "connected" }
  | { readonly status: "reconnecting"; readonly attempt: number; readonly retryInMs: number }
  | { readonly status: "bearer-expired" }
  | { readonly status: "dead"; readonly reason: string };

export interface BrokerStreamReconnectOptions {
  readonly initialDelayMs: number;
  readonly maxDelayMs: number;
  readonly maxReconnectAttempts: number;
  readonly jitterMs: (baseDelayMs: number, attempt: number) => number;
  readonly frameByteLimit: number;
}

interface BrokerStreamStateContextValue {
  readonly state: BrokerStreamState;
  readonly setState: StreamStateSink;
}

const DEFAULT_STREAM_STATE: BrokerStreamState = { status: "idle" };
const DEFAULT_RECONNECT_OPTIONS: BrokerStreamReconnectOptions = {
  initialDelayMs: 250,
  maxDelayMs: 5_000,
  maxReconnectAttempts: 5,
  jitterMs: defaultJitterMs,
  frameByteLimit: 64 * 1024,
};
const UTF8_ENCODER = new TextEncoder();
const BrokerStreamStateContext = createContext<BrokerStreamStateContextValue | null>(null);

export function BrokerStreamStateProvider({ children }: { readonly children: ReactNode }) {
  const [state, setState] = useState<BrokerStreamState>(DEFAULT_STREAM_STATE);
  const value = useMemo(() => ({ state, setState }), [state]);
  return createElement(BrokerStreamStateContext.Provider, { value }, children);
}

export function useBrokerStreamState(): BrokerStreamState {
  const context = useContext(BrokerStreamStateContext);
  if (context === null) {
    throw new Error("useBrokerStreamState must be used inside BrokerStreamStateProvider");
  }
  return context.state;
}

export function useBrokerEvents(): void {
  const bootstrap = useBrokerBootstrap();
  const queryClient = useQueryClient();
  const setStreamState = useBrokerStreamStateSink();

  useEffect(() => {
    if (bootstrap.status !== "ready") {
      setStreamState(DEFAULT_STREAM_STATE);
      return;
    }
    const controller = new AbortController();
    void consumeBrokerEvents({
      brokerUrl: bootstrap.brokerUrl,
      bearer: bootstrap.bearer,
      queryClient,
      signal: controller.signal,
      fetchImpl: fetch,
      onStateChange: setStreamState,
    });
    return () => {
      controller.abort();
    };
  }, [bootstrap, queryClient, setStreamState]);
}

export async function consumeBrokerEvents(args: {
  readonly brokerUrl: BrokerUrl;
  readonly bearer: ApiToken;
  readonly queryClient: QueryClient;
  readonly signal: AbortSignal;
  readonly fetchImpl: FetchLike;
  readonly onStateChange?: StreamStateSink | undefined;
  readonly reconnect?: Partial<BrokerStreamReconnectOptions> | undefined;
}): Promise<void> {
  const options = { ...DEFAULT_RECONNECT_OPTIONS, ...args.reconnect };
  let reconnects = 0;

  for (;;) {
    const attempt = createLinkedAbortController(args.signal);
    if (args.signal.aborted) {
      attempt.cleanup();
      return;
    }
    args.onStateChange?.(
      reconnects === 0
        ? { status: "connecting", attempt: 1 }
        : { status: "connecting", attempt: reconnects + 1 },
    );

    try {
      const response = await args.fetchImpl(`${args.brokerUrl}/api/events`, {
        headers: {
          Accept: "text/event-stream",
          Authorization: `Bearer ${args.bearer}`,
        },
        signal: attempt.controller.signal,
      });

      if (response.status === 401) {
        attempt.cleanup();
        args.onStateChange?.({ status: "bearer-expired" });
        return;
      }
      if (!response.ok || response.body === null) {
        throw new TransientStreamError(`stream open failed with HTTP ${String(response.status)}`);
      }

      args.onStateChange?.({ status: "connected" });
      await readSseFrames(
        response.body,
        (frame) => {
          invalidateForBrokerFrame(args.queryClient, frame);
        },
        {
          signal: attempt.controller.signal,
          frameByteLimit: options.frameByteLimit,
        },
      );
      throw new TransientStreamError("stream closed");
    } catch (error) {
      attempt.cleanup();
      if (args.signal.aborted) return;
      if (error instanceof SseFrameTooLargeError) {
        attempt.controller.abort();
        args.onStateChange?.({ status: "dead", reason: error.message });
        return;
      }
      if (reconnects >= options.maxReconnectAttempts) {
        args.onStateChange?.({
          status: "dead",
          reason: error instanceof Error ? error.message : "stream failed",
        });
        return;
      }

      const nextAttempt = reconnects + 1;
      const retryInMs = reconnectDelayMs(options, reconnects, nextAttempt);
      reconnects = nextAttempt;
      args.onStateChange?.({ status: "reconnecting", attempt: reconnects, retryInMs });
      if (!(await waitForReconnect(args.signal, retryInMs))) return;
    }
  }
}

export function invalidateForBrokerFrame(queryClient: QueryClient, frame: SseFrame): boolean {
  if (frame.event === "ready") {
    void queryClient.invalidateQueries({ queryKey: threadQueryKeys.all });
    void queryClient.invalidateQueries({ queryKey: approvalQueryKeys.all });
    return true;
  }

  const parsed = parseBrokerStreamEvent(frame);
  if (parsed === null) return false;

  if (isThreadStreamEvent(parsed)) {
    void queryClient.invalidateQueries({ queryKey: threadQueryKeys.list() });
    void queryClient.invalidateQueries({
      queryKey: threadQueryKeys.detail(parsed.payload.threadId),
    });
    if (parsed.kind === "thread.pinned_approvals.changed") {
      void queryClient.invalidateQueries({
        queryKey: threadQueryKeys.pinnedApprovals(parsed.payload.threadId),
      });
      void queryClient.invalidateQueries({ queryKey: approvalQueryKeys.all });
    }
    return true;
  }

  void queryClient.invalidateQueries({ queryKey: approvalQueryKeys.list() });
  void queryClient.invalidateQueries({
    queryKey: approvalQueryKeys.detail(parsed.payload.requestId),
  });
  if (parsed.payload.threadId !== undefined) {
    void queryClient.invalidateQueries({ queryKey: threadQueryKeys.list() });
    void queryClient.invalidateQueries({
      queryKey: threadQueryKeys.detail(parsed.payload.threadId),
    });
    void queryClient.invalidateQueries({
      queryKey: threadQueryKeys.pinnedApprovals(parsed.payload.threadId),
    });
  }
  return true;
}

async function readSseFrames(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: SseFrame) => void,
  options: {
    readonly signal: AbortSignal;
    readonly frameByteLimit: number;
  },
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const abortReader = () => {
    void reader.cancel();
  };
  options.signal.addEventListener("abort", abortReader, { once: true });
  let buffer = "";
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      // Normalize CRLF on the *accumulated* buffer rather than per
      // chunk. If `\r` ends one decoded chunk and `\n` starts the next,
      // per-chunk replacement leaves `\r\n` in the buffer and the frame
      // splitter misses the boundary.
      buffer += decoder.decode(chunk.value, { stream: true });
      buffer = buffer.replace(/\r\n/g, "\n");
      buffer = drainFrames(buffer, onFrame, options.frameByteLimit);
      assertFrameByteLimit(buffer, options.frameByteLimit);
    }
    buffer += decoder.decode();
    buffer = buffer.replace(/\r\n/g, "\n");
    drainFrames(`${buffer}\n\n`, onFrame, options.frameByteLimit);
  } catch (error) {
    try {
      await reader.cancel();
    } catch {
      // The stream is already closing; preserve the original read failure.
    }
    throw error;
  } finally {
    options.signal.removeEventListener("abort", abortReader);
  }
}

function drainFrames(
  buffer: string,
  onFrame: (frame: SseFrame) => void,
  frameByteLimit: number,
): string {
  let remaining = buffer;
  for (;;) {
    const frameEnd = remaining.indexOf("\n\n");
    if (frameEnd < 0) return remaining;
    const rawFrame = remaining.slice(0, frameEnd);
    assertFrameByteLimit(rawFrame, frameByteLimit);
    remaining = remaining.slice(frameEnd + 2);
    const frame = parseSseFrame(rawFrame);
    if (frame !== null) onFrame(frame);
  }
}

function parseSseFrame(rawFrame: string): SseFrame | null {
  let event = "message";
  const dataLines: string[] = [];
  for (const line of rawFrame.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
}

function parseBrokerStreamEvent(frame: SseFrame): ThreadStreamEvent | ApprovalStreamEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(frame.data);
  } catch {
    return null;
  }

  if (isThreadStreamEventKind(frame.event)) {
    const result = validateThreadStreamEvent(parsed);
    if (!result.ok) return null;
    const event = parsed as ThreadStreamEvent;
    return event.kind === frame.event ? event : null;
  }
  if (isApprovalStreamEventKind(frame.event)) {
    const result = validateApprovalStreamEvent(parsed);
    if (!result.ok) return null;
    const event = parsed as ApprovalStreamEvent;
    return event.kind === frame.event ? event : null;
  }
  return null;
}

function useBrokerStreamStateSink(): StreamStateSink {
  const context = useContext(BrokerStreamStateContext);
  return context?.setState ?? noopStreamStateSink;
}

function isThreadStreamEvent(
  event: ThreadStreamEvent | ApprovalStreamEvent,
): event is ThreadStreamEvent {
  return isThreadStreamEventKind(event.kind);
}

function isThreadStreamEventKind(value: string): value is ThreadStreamEventKind {
  return (
    value === "thread.created" ||
    value === "thread.updated" ||
    value === "thread.pinned_approvals.changed"
  );
}

function isApprovalStreamEventKind(value: string): value is ApprovalStreamEventKind {
  return value === "approval.requested" || value === "approval.decided";
}

function assertFrameByteLimit(frame: string, frameByteLimit: number): void {
  if (UTF8_ENCODER.encode(frame).byteLength > frameByteLimit) {
    throw new SseFrameTooLargeError(frameByteLimit);
  }
}

function reconnectDelayMs(
  options: BrokerStreamReconnectOptions,
  reconnects: number,
  nextAttempt: number,
): number {
  const baseDelay = Math.min(options.maxDelayMs, options.initialDelayMs * 2 ** reconnects);
  return Math.min(options.maxDelayMs, baseDelay + options.jitterMs(baseDelay, nextAttempt));
}

function defaultJitterMs(baseDelayMs: number): number {
  const spread = Math.floor(baseDelayMs * 0.2);
  if (spread <= 0) return 0;
  return Math.floor(Math.random() * (spread + 1));
}

function waitForReconnect(signal: AbortSignal, delayMs: number): Promise<boolean> {
  if (signal.aborted) return Promise.resolve(false);
  return new Promise((resolve) => {
    const timeout = globalThis.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve(true);
    }, delayMs);
    const onAbort = () => {
      globalThis.clearTimeout(timeout);
      resolve(false);
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function createLinkedAbortController(parent: AbortSignal): {
  readonly controller: AbortController;
  readonly cleanup: () => void;
} {
  const controller = new AbortController();
  if (parent.aborted) {
    controller.abort();
    return { controller, cleanup: noop };
  }

  const abort = () => {
    controller.abort();
  };
  parent.addEventListener("abort", abort, { once: true });
  return {
    controller,
    cleanup: () => {
      parent.removeEventListener("abort", abort);
    },
  };
}

function noop(): void {}

function noopStreamStateSink(): void {}

class TransientStreamError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransientStreamError";
  }
}

class SseFrameTooLargeError extends Error {
  constructor(frameByteLimit: number) {
    super(`SSE frame exceeded ${String(frameByteLimit)} bytes`);
    this.name = "SseFrameTooLargeError";
  }
}

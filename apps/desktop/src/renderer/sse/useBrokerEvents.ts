import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import type { ApiToken, BrokerUrl } from "../bootstrap/types.ts";
import { useBrokerBootstrap } from "../bootstrap/useBrokerBootstrap.ts";
import { approvalQueryKeys, threadQueryKeys } from "../query/queries.ts";

type FetchLike = typeof fetch;
type ThreadEventKind = "thread.created" | "thread.updated" | "thread.pinned_approvals.changed";

interface ThreadStreamEvent {
  readonly kind: ThreadEventKind;
  readonly payload: {
    readonly threadId: string;
    readonly headLsn: string;
  };
}

interface SseFrame {
  readonly event: string;
  readonly data: string;
}

interface UnknownThreadEventRecord {
  readonly kind?: unknown;
  readonly payload?: unknown;
}

interface UnknownThreadEventPayload {
  readonly threadId?: unknown;
  readonly headLsn?: unknown;
}

export function useBrokerEvents(): void {
  const bootstrap = useBrokerBootstrap();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (bootstrap.status !== "ready") return;
    const controller = new AbortController();
    void consumeBrokerEvents({
      brokerUrl: bootstrap.brokerUrl,
      bearer: bootstrap.bearer,
      queryClient,
      signal: controller.signal,
      fetchImpl: fetch,
    }).catch(() => undefined);
    return () => {
      controller.abort();
    };
  }, [bootstrap, queryClient]);
}

export async function consumeBrokerEvents(args: {
  readonly brokerUrl: BrokerUrl;
  readonly bearer: ApiToken;
  readonly queryClient: QueryClient;
  readonly signal: AbortSignal;
  readonly fetchImpl: FetchLike;
}): Promise<void> {
  const response = await args.fetchImpl(`${args.brokerUrl}/api/events`, {
    headers: {
      Accept: "text/event-stream",
      Authorization: `Bearer ${args.bearer}`,
    },
    signal: args.signal,
  });
  if (!response.ok || response.body === null) return;

  await readSseFrames(response.body, (frame) => {
    invalidateForBrokerFrame(args.queryClient, frame);
  });
}

export function invalidateForBrokerFrame(queryClient: QueryClient, frame: SseFrame): boolean {
  if (frame.event === "ready") {
    void queryClient.invalidateQueries({ queryKey: threadQueryKeys.all });
    void queryClient.invalidateQueries({ queryKey: approvalQueryKeys.all });
    return true;
  }

  const parsed = parseThreadEvent(frame.data);
  if (parsed === null || parsed.kind !== frame.event) return false;

  void queryClient.invalidateQueries({ queryKey: threadQueryKeys.list() });
  void queryClient.invalidateQueries({ queryKey: threadQueryKeys.detail(parsed.payload.threadId) });
  if (parsed.kind === "thread.pinned_approvals.changed") {
    void queryClient.invalidateQueries({
      queryKey: threadQueryKeys.pinnedApprovals(parsed.payload.threadId),
    });
    void queryClient.invalidateQueries({ queryKey: approvalQueryKeys.all });
  }
  return true;
}

async function readSseFrames(
  body: ReadableStream<Uint8Array>,
  onFrame: (frame: SseFrame) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const chunk = await reader.read();
    if (chunk.done) break;
    buffer += decoder.decode(chunk.value, { stream: true });
    buffer = drainFrames(buffer, onFrame);
  }
  buffer += decoder.decode();
  drainFrames(`${buffer}\n\n`, onFrame);
}

function drainFrames(buffer: string, onFrame: (frame: SseFrame) => void): string {
  let remaining = buffer;
  for (;;) {
    const frameEnd = remaining.indexOf("\n\n");
    if (frameEnd < 0) return remaining;
    const rawFrame = remaining.slice(0, frameEnd);
    remaining = remaining.slice(frameEnd + 2);
    const frame = parseSseFrame(rawFrame);
    if (frame !== null) onFrame(frame);
  }
}

function parseSseFrame(rawFrame: string): SseFrame | null {
  let event = "message";
  const dataLines: string[] = [];
  for (const line of rawFrame.split(/\r?\n/)) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
}

function parseThreadEvent(data: string): ThreadStreamEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(parsed)) return null;
  const record = parsed as UnknownThreadEventRecord;
  const kind = record.kind;
  const payload = record.payload;
  if (!isThreadEventKind(kind) || !isRecord(payload)) {
    return null;
  }
  const payloadRecord = payload as UnknownThreadEventPayload;
  const threadId = payloadRecord.threadId;
  const headLsn = payloadRecord.headLsn;
  if (typeof threadId !== "string" || typeof headLsn !== "string") return null;
  return { kind, payload: { threadId, headLsn } };
}

function isThreadEventKind(value: unknown): value is ThreadEventKind {
  return (
    value === "thread.created" ||
    value === "thread.updated" ||
    value === "thread.pinned_approvals.changed"
  );
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}

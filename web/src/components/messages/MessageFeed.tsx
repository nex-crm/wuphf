import { useEffect, useRef } from "react";

import type { Message } from "../../api/client";
import { useMessages } from "../../hooks/useMessages";
import { formatDateLabel } from "../../lib/format";
import { useAppStore } from "../../stores/app";
import { MessageBubble } from "./MessageBubble";

function dateDayKey(ts: string): string {
  const d = new Date(ts);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

type ThreadMessage = {
  message: Message;
  grouped: boolean;
};

type FeedElement =
  | { type: "date"; key: string; label: string }
  | {
      type: "thread";
      key: string;
      parent: ThreadMessage;
      replies: ThreadMessage[];
    };

export function MessageFeed() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId);
  const collapsedThreads = useAppStore((s) => s.collapsedThreads);
  const toggleThreadCollapsed = useAppStore((s) => s.toggleThreadCollapsed);
  const containerRef = useRef<HTMLDivElement>(null);
  const prevLengthRef = useRef(0);

  const copyMessageLink = (id: string) => {
    const url = new URL(window.location.href);
    url.hash = `#msg-${id}`;
    navigator.clipboard?.writeText(url.toString()).catch(() => {});
  };

  const { data: messages = [], isLoading } = useMessages(currentChannel);

  // Auto-scroll when new messages arrive
  useEffect(() => {
    if (messages.length > prevLengthRef.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
    prevLengthRef.current = messages.length;
  }, [messages.length]);

  if (isLoading && messages.length === 0) {
    return (
      <div
        className="messages"
        style={{ alignItems: "center", justifyContent: "center" }}
      >
        <span style={{ color: "var(--text-tertiary)", fontSize: 13 }}>
          Loading messages...
        </span>
      </div>
    );
  }

  if (messages.length === 0) {
    return (
      <div className="messages">
        <div className="channel-empty-state">
          <span className="eyebrow">quiet before the standup</span>
          <span className="title">#{currentChannel} is empty. For now.</span>
          <span className="body">
            This is where your agents will argue, claim tasks, and show
            progress. Unlike Ryan Howard, they actually ship.
          </span>
          <div className="channel-empty-hints">
            <div>
              Try <code>@ceo what should we build this week?</code>
            </div>
            <div>
              Type <code>/</code> for commands, <code>@</code> to mention an
              agent.
            </div>
          </div>
          <span className="channel-empty-foot">
            Michael would be proud. Probably.
          </span>
        </div>
      </div>
    );
  }

  // Build thread-aware element list. Top-level channel messages become thread
  // heads; direct replies become their children; deep-thread replies stay in
  // the side panel. Grouping by sender + 5-min window applies both to parents
  // and to consecutive replies within the same thread so long exchanges read
  // as one continuous block.
  const elements: FeedElement[] = [];
  const byId = new Map<string, Message>();
  for (const m of messages) byId.set(m.id, m);

  const repliesByParent = new Map<string, Message[]>();
  for (const msg of messages) {
    if (msg.content?.startsWith("[STATUS]")) continue;
    if (!msg.reply_to) continue;
    const parent = byId.get(msg.reply_to);
    if (!parent) continue;
    if (parent.reply_to) continue; // deep thread lives in the side panel
    const list = repliesByParent.get(parent.id) ?? [];
    list.push(msg);
    repliesByParent.set(parent.id, list);
  }

  let lastDate = "";
  let lastFrom = "";
  let lastTime = "";

  const wrap = (msg: Message): ThreadMessage => {
    let grouped = false;
    if (lastFrom === msg.from && msg.timestamp && lastTime) {
      const delta =
        new Date(msg.timestamp).getTime() - new Date(lastTime).getTime();
      if (delta >= 0 && delta < 5 * 60 * 1000) grouped = true;
    }
    lastFrom = msg.from;
    lastTime = msg.timestamp || lastTime;
    return { message: msg, grouped };
  };

  const maybeEmitDateSeparator = (msg: Message) => {
    if (!msg.timestamp) return;
    const dayKey = dateDayKey(msg.timestamp);
    if (dayKey === lastDate) return;
    elements.push({
      type: "date",
      key: `date-${dayKey}`,
      label: formatDateLabel(msg.timestamp),
    });
    lastDate = dayKey;
    lastFrom = "";
    lastTime = "";
  };

  for (const msg of messages) {
    if (msg.content?.startsWith("[STATUS]")) continue;
    if (msg.reply_to) continue; // only top-level messages seed threads

    maybeEmitDateSeparator(msg);
    const parent = wrap(msg);

    const rawReplies = repliesByParent.get(msg.id) ?? [];
    const replies: ThreadMessage[] = [];
    for (const r of rawReplies) {
      maybeEmitDateSeparator(r);
      replies.push(wrap(r));
    }

    elements.push({
      type: "thread",
      key: `thread-${msg.id}`,
      parent,
      replies,
    });
  }

  return (
    <div className="messages" ref={containerRef}>
      {elements.map((el) => {
        if (el.type === "date") {
          return (
            <div key={el.key} className="date-separator">
              <div className="date-separator-line" />
              <span className="date-separator-text">{el.label}</span>
              <div className="date-separator-line" />
            </div>
          );
        }
        const hasReplies = el.replies.length > 0;
        const parentId = el.parent.message.id;
        const isCollapsed = hasReplies && (collapsedThreads[parentId] ?? false);
        return (
          <div
            key={el.key}
            className={`thread-group${hasReplies ? " thread-group-has-replies" : ""}${isCollapsed ? " thread-group-collapsed" : ""}`}
          >
            <MessageBubble
              message={el.parent.message}
              grouped={el.parent.grouped}
              replyCount={el.replies.length}
              onOpenThread={(id) => setActiveThreadId(id)}
              onCopyLink={copyMessageLink}
            />
            {hasReplies && (
              <button
                type="button"
                className="thread-collapse-toggle"
                onClick={() => toggleThreadCollapsed(parentId)}
                aria-expanded={!isCollapsed}
                aria-controls={`thread-${parentId}-replies`}
              >
                <svg
                  className="thread-collapse-chevron"
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d={isCollapsed ? "m9 18 6-6-6-6" : "m6 9 6 6 6-6"} />
                </svg>
                {isCollapsed
                  ? `Show ${el.replies.length} ${el.replies.length === 1 ? "reply" : "replies"}`
                  : "Hide thread"}
              </button>
            )}
            {hasReplies && !isCollapsed && (
              <div className="thread-replies" id={`thread-${parentId}-replies`}>
                {el.replies.map((r) => (
                  <MessageBubble
                    key={r.message.id}
                    message={r.message}
                    grouped={r.grouped}
                    isReply={true}
                    onOpenThread={(id) => setActiveThreadId(id)}
                    onCopyLink={copyMessageLink}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

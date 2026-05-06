import { useEffect, useRef } from "react";

import { useAgentStream } from "../../hooks/useAgentStream";
import { useMessages } from "../../hooks/useMessages";
import { Composer } from "./Composer";
import { InterviewBar } from "./InterviewBar";
import { MessageBubble } from "./MessageBubble";
import { StreamLineView } from "./StreamLineView";
import { TypingIndicator } from "./TypingIndicator";

interface DMViewProps {
  agentSlug: string;
  channelSlug: string;
}

/**
 * DMView is rendered only when MainContent's route dispatch matches the
 * `dm` kind. Receiving the agent + channel slugs as props keeps the
 * route discrimination in one place — and prevents an empty channel
 * slug from ever reaching useMessages, which would issue a real broker
 * request for `["messages", ""]`.
 */
export function DMView({ agentSlug, channelSlug }: DMViewProps) {
  const { data: messages = [] } = useMessages(channelSlug);
  const { lines, connected } = useAgentStream(agentSlug);
  const messagesRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<HTMLDivElement>(null);
  // appendStreamLine merges consecutive raw chunks into the last line's
  // `data` without growing the array, so depending on length alone would
  // freeze the scroll while a model is still streaming text. Track the
  // last line's id+data so coalesced updates retrigger the effect too.
  const lastLine = lines[lines.length - 1];

  // Auto-scroll messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, []);

  // Auto-scroll stream — but only when the user is already near the bottom,
  // so a reader scrolled back through history isn't yanked away by every
  // new line that lands.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on every new line so the log auto-scrolls.
  useEffect(() => {
    const el = streamRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 32) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length, lastLine?.id, lastLine?.data]);

  return (
    <>
      {/* Split layout: messages left, live stream right */}
      <div style={{ flex: 1, display: "flex", overflow: "hidden" }}>
        {/* Left: Messages + Composer */}
        <div
          style={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            overflow: "hidden",
          }}
        >
          <div ref={messagesRef} className="messages">
            {messages.map((msg) => (
              <MessageBubble key={msg.id} message={msg} />
            ))}
          </div>
          <TypingIndicator />
          <InterviewBar />
          <Composer />
        </div>

        {/* Right: Live stream */}
        <div
          style={{
            width: 320,
            flexShrink: 0,
            borderLeft: "1px solid var(--border)",
            display: "flex",
            flexDirection: "column",
            overflow: "hidden",
          }}
        >
          <div
            style={{
              padding: "8px 12px",
              borderBottom: "1px solid var(--border)",
              display: "flex",
              alignItems: "center",
              gap: 8,
              fontSize: 13,
              fontWeight: 600,
            }}
          >
            <span
              className={`status-dot ${connected ? "active pulse" : "lurking"}`}
            />
            <span>Live output</span>
          </div>
          <div
            ref={streamRef}
            style={{
              flex: 1,
              overflowY: "auto",
              padding: 8,
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              lineHeight: 1.5,
              color: "var(--text-secondary)",
            }}
          >
            {lines.length === 0 ? (
              <div style={{ color: "var(--text-tertiary)", padding: 8 }}>
                {connected ? "Waiting for output..." : "Stream idle"}
              </div>
            ) : (
              lines.map((line) => (
                <StreamLineView key={line.id} line={line} compact={true} />
              ))
            )}
          </div>
        </div>
      </div>
    </>
  );
}

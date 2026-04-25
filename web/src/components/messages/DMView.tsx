import { useEffect, useRef } from "react";

import { useAgentStream } from "../../hooks/useAgentStream";
import { useMessages } from "../../hooks/useMessages";
import { isDMChannel, useAppStore } from "../../stores/app";
import { Composer } from "./Composer";
import { InterviewBar } from "./InterviewBar";
import { MessageBubble } from "./MessageBubble";
import { StreamLineView } from "./StreamLineView";
import { TypingIndicator } from "./TypingIndicator";

export function DMView() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const dm = isDMChannel(currentChannel, channelMeta);
  const dmAgentSlug = dm?.agentSlug ?? null;
  const { data: messages = [] } = useMessages(currentChannel);
  const { lines, connected } = useAgentStream(dmAgentSlug);
  const messagesRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<HTMLDivElement>(null);

  // Auto-scroll messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, []);

  // Auto-scroll stream
  useEffect(() => {
    if (streamRef.current) {
      streamRef.current.scrollTop = streamRef.current.scrollHeight;
    }
  }, []);

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

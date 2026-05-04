import { useEffect, useRef } from "react";

import { useMessages } from "../../hooks/useMessages";
import { AgentTerminal } from "../agents/AgentTerminal";
import { Composer } from "./Composer";
import { InterviewBar } from "./InterviewBar";
import { MessageBubble } from "./MessageBubble";
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
  const messagesRef = useRef<HTMLDivElement>(null);

  // Auto-scroll messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
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
          <AgentTerminal slug={agentSlug} title="Live output" />
        </div>
      </div>
    </>
  );
}

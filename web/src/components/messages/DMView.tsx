import { useEffect, useRef } from "react";

import { useMessages } from "../../hooks/useMessages";
import { isDMChannel, useAppStore } from "../../stores/app";
import { AgentTerminal } from "../agents/AgentTerminal";
import { Composer } from "./Composer";
import { InterviewBar } from "./InterviewBar";
import { MessageBubble } from "./MessageBubble";
import { TypingIndicator } from "./TypingIndicator";

export function DMView() {
  const currentChannel = useAppStore((s) => s.currentChannel);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const dm = isDMChannel(currentChannel, channelMeta);
  const dmAgentSlug = dm?.agentSlug ?? null;
  const { data: messages = [] } = useMessages(currentChannel);
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
          <AgentTerminal slug={dmAgentSlug} title="Live output" />
        </div>
      </div>
    </>
  );
}

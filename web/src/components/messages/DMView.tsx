import { useEffect, useMemo, useRef, useState } from "react";
import { NavArrowDown, NavArrowUp } from "iconoir-react";

import { useMessages } from "../../hooks/useMessages";
import { AgentWorkbenchPane } from "../agents/AgentWorkbenchPane";
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
 *
 * Layout: the agent workbench fills the page (active tasks, live
 * stream, recent activity — each section collapsible). A chat drawer
 * sits at the bottom: collapsed it shows the composer plus the latest
 * message so the user can still send and reply with one keystroke;
 * expanded it grows into a tall overlay over the workbench so chat
 * history is fully reachable without leaving the workbench surface.
 */
export function DMView({ agentSlug, channelSlug }: DMViewProps) {
  const { data: messages = [] } = useMessages(channelSlug);
  const [chatExpanded, setChatExpanded] = useState(false);
  const messagesRef = useRef<HTMLDivElement>(null);

  const lastMessage = useMemo(
    () => (messages.length ? messages[messages.length - 1] : null),
    [messages],
  );

  // `messages.length` is the trigger for re-firing this effect: when a new
  // message arrives while the drawer is expanded, we want to follow it to
  // the bottom. The body doesn't read the value, but the dep IS the signal —
  // same `void` pattern other route-driven effects in this codebase use to
  // satisfy useExhaustiveDependencies without dropping the dependency.
  const messagesLength = messages.length;
  useEffect(() => {
    void messagesLength;
    if (chatExpanded && messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, [chatExpanded, messagesLength]);

  return (
    <div className="dm-workbench" data-testid="dm-workbench">
      <div className="dm-workbench-body">
        <AgentWorkbenchPane agentSlug={agentSlug} />
      </div>

      <div
        className={`dm-chat-drawer${chatExpanded ? " is-expanded" : ""}`}
        data-testid="dm-chat-drawer"
      >
        <button
          type="button"
          className="dm-chat-drawer-toggle"
          onClick={() => setChatExpanded((value) => !value)}
          aria-expanded={chatExpanded}
          aria-controls="dm-chat-drawer-body"
          title={chatExpanded ? "Collapse chat" : "Expand chat"}
        >
          <span className="dm-chat-drawer-toggle-label">
            Chat with @{agentSlug}
          </span>
          <span className="dm-chat-drawer-toggle-icon" aria-hidden="true">
            {chatExpanded ? (
              <NavArrowDown width={16} height={16} />
            ) : (
              <NavArrowUp width={16} height={16} />
            )}
          </span>
        </button>

        <div className="dm-chat-drawer-body" id="dm-chat-drawer-body">
          {chatExpanded ? (
            <div ref={messagesRef} className="messages dm-chat-messages">
              {messages.map((msg) => (
                <MessageBubble key={msg.id} message={msg} />
              ))}
            </div>
          ) : lastMessage ? (
            <div className="dm-chat-preview">
              <MessageBubble message={lastMessage} />
            </div>
          ) : (
            <div className="dm-chat-preview dm-chat-preview-empty">
              No messages yet — start the conversation below.
            </div>
          )}
          <TypingIndicator />
          <InterviewBar />
          <Composer />
        </div>
      </div>
    </div>
  );
}

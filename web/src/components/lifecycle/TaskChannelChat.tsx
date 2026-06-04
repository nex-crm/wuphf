import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { postTaskComment } from "../../api/lifecycle";
import { useMessages } from "../../hooks/useMessages";
import { MessageBubble } from "../messages/MessageBubble";

interface TaskChannelChatProps {
  taskId: string;
  channel: string;
  /**
   * Called after the human posts, so the parent can refresh derived
   * surfaces (the decision packet, lifecycle queries).
   */
  onCommentPosted?: () => void;
}

/**
 * TaskChannelChat — the task's real conversation surface.
 *
 * Every task owns a channel (`task-<id>`) whose default members are the
 * owner, the CEO, and the Librarian. This renders that channel's LIVE
 * message stream (the agents' working chat) and lets the human post into
 * it. It replaces the old packet-comments timeline, which only showed a
 * curated subset of comments and hid the agent conversation entirely — so
 * there was no way to actually reach the per-task chat from the task.
 *
 * Reuses the same primitives as the rest of the app's chat: `useMessages`
 * (2s live polling, viewer = human) for the stream and `MessageBubble` for
 * rendering. Posting goes through `postTaskComment`, which the broker
 * routes to the task's channel.
 */
export function TaskChannelChat({
  taskId,
  channel,
  onCommentPosted,
}: TaskChannelChatProps) {
  const { data: messages = [], isPending } = useMessages(channel);
  const queryClient = useQueryClient();
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const trimmed = body.trim();

  const mutation = useMutation({
    mutationFn: (text: string) => postTaskComment(taskId, channel, text),
    onSuccess: () => {
      setBody("");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["messages", channel] });
      onCommentPosted?.();
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not post message.");
    },
  });

  // Follow new messages to the bottom. messages.length is the signal (the
  // body doesn't read it) — same void-dep pattern used in DMView.
  const messageCount = messages.length;
  useEffect(() => {
    void messageCount;
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messageCount]);

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmed || mutation.isPending) return;
    mutation.mutate(trimmed);
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (trimmed && !mutation.isPending) {
        mutation.mutate(trimmed);
      }
    }
  }

  return (
    <section
      className="task-channel-chat"
      aria-label="Task channel conversation"
      data-testid="task-channel-chat"
    >
      <div className="task-channel-chat-messages messages" ref={scrollRef}>
        {isPending ? (
          <p className="task-channel-chat-empty">Loading the conversation…</p>
        ) : messages.length === 0 ? (
          <p
            className="task-channel-chat-empty"
            data-testid="task-channel-chat-empty"
          >
            No messages yet. The owner, the CEO, and the Librarian work in this
            channel — post here to talk to them about this task.
          </p>
        ) : (
          messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))
        )}
      </div>

      <form className="task-channel-chat-composer" onSubmit={submit}>
        <textarea
          className="task-channel-chat-input"
          value={body}
          onChange={(event) => setBody(event.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Message the team on this task… (⌘/Ctrl+Enter to send)"
          rows={2}
          data-testid="task-channel-chat-input"
        />
        {error ? (
          <p className="task-channel-chat-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="task-channel-chat-actions">
          <button
            type="submit"
            className="task-channel-chat-send"
            disabled={!trimmed || mutation.isPending}
            data-testid="task-channel-chat-send"
          >
            {mutation.isPending ? "Sending…" : "Send"}
          </button>
        </div>
      </form>
    </section>
  );
}

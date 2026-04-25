import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import type { Message } from "../../api/client";
import { postMessage } from "../../api/client";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useThreadMessages } from "../../hooks/useMessages";
import { extractTaggedMentions } from "../../lib/mentions";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";
import { MessageBubble } from "./MessageBubble";

export function ThreadPanel() {
  const activeThreadId = useAppStore((s) => s.activeThreadId);
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId);
  const currentChannel = useAppStore((s) => s.currentChannel);
  const [text, setText] = useState("");
  const [quoting, setQuoting] = useState<Message | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const messagesRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const { data: members = [] } = useOfficeMembers();
  const knownSlugs = useMemo(() => members.map((m) => m.slug), [members]);

  const { data: messages = [] } = useThreadMessages(
    currentChannel,
    activeThreadId,
  );

  // Split the thread query response into parent + replies so we can render
  // the parent prominently at the top (like Slack's thread pane). The broker
  // returns both in the same list because thread_id matches either id or
  // reply_to.
  const { parent, replies } = useMemo(() => {
    let parent: Message | null = null;
    const replies: Message[] = [];
    for (const m of messages) {
      if (m.id === activeThreadId) parent = m;
      else if (m.reply_to) replies.push(m);
    }
    return { parent, replies };
  }, [messages, activeThreadId]);

  // Auto-scroll to the bottom when a new reply arrives. Anchoring at the
  // bottom means the composer is always in context and new agent replies
  // land where your eye already is.
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, []);

  // Reset the quote chip when the panel closes OR when the user switches
  // to a different thread. Persisting the quote would mean a stale reply_to
  // fires against the wrong thread on the next send.
  useEffect(() => {
    setQuoting(null);
    setText("");
  }, []);

  // Focus the composer on open so users can start typing immediately.
  useEffect(() => {
    if (activeThreadId && textareaRef.current) {
      textareaRef.current.focus();
    }
  }, [activeThreadId]);

  // Escape closes the panel — matches the close button affordance.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && activeThreadId) {
        setActiveThreadId(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [activeThreadId, setActiveThreadId]);

  // The thread target is the quoted reply if the user clicked "quote" on a
  // specific reply; otherwise it's the parent. Broker thread semantics
  // don't distinguish depth — any reply with reply_to in the chain shows up
  // under this thread_id — so quoting-a-reply just tags the new message
  // against that reply's id for display, while still appearing in the same
  // thread panel.
  const replyTarget = quoting?.id ?? activeThreadId ?? undefined;

  const sendReply = useMutation({
    mutationFn: (content: string) =>
      postMessage(
        content,
        currentChannel,
        replyTarget,
        extractTaggedMentions(content, knownSlugs),
      ),
    onSuccess: () => {
      setText("");
      setQuoting(null);
      queryClient.invalidateQueries({
        queryKey: ["thread-messages", currentChannel, activeThreadId],
      });
      queryClient.invalidateQueries({ queryKey: ["messages", currentChannel] });
    },
    onError: (err: unknown) => {
      const message =
        err instanceof Error ? err.message : "Failed to send reply";
      showNotice(message, "error");
    },
  });

  const handleSend = () => {
    const trimmed = text.trim();
    if (!trimmed || sendReply.isPending) return;
    sendReply.mutate(trimmed);
  };

  if (!activeThreadId) return null;

  return (
    <div className="thread-panel open" role="complementary" aria-label="Thread">
      <div className="thread-panel-header">
        <div className="thread-panel-title-group">
          <span className="thread-panel-title">Thread</span>
          <span className="thread-panel-channel">#{currentChannel}</span>
        </div>
        <button
          className="thread-panel-close"
          onClick={() => setActiveThreadId(null)}
          aria-label="Close thread"
          title="Close (Esc)"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M18 6 6 18" />
            <path d="m6 6 12 12" />
          </svg>
        </button>
      </div>

      <div ref={messagesRef} className="thread-panel-body">
        {parent && (
          <div className="thread-panel-parent">
            <MessageBubble message={parent} />
          </div>
        )}
        {replies.length > 0 && (
          <div className="thread-panel-replies-count">
            {replies.length} {replies.length === 1 ? "reply" : "replies"}
          </div>
        )}
        {replies.length === 0 ? (
          <div className="thread-panel-empty">
            No replies yet. Start the conversation below.
          </div>
        ) : (
          replies.map((msg) => (
            <MessageBubble
              key={msg.id}
              message={msg}
              onQuoteReply={(m) => {
                setQuoting(m);
                textareaRef.current?.focus();
              }}
            />
          ))
        )}
      </div>

      {/* Composer. If the user clicked "quote" on a reply, show a small
          chip above the input that names who they're replying to and
          offers a dismiss. This mirrors Slack's "Replying to …" affordance
          and makes the active reply_to target visible. */}
      <div className="composer">
        {quoting && (
          <div className="thread-quote-chip">
            <svg
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M3 21v-5a5 5 0 0 1 5-5h13" />
              <path d="m16 16-5-5 5-5" />
            </svg>
            <span className="thread-quote-label">
              Replying to <strong>@{quoting.from}</strong>
            </span>
            <span className="thread-quote-preview">
              {truncate(quoting.content, 60)}
            </span>
            <button
              className="thread-quote-dismiss"
              onClick={() => setQuoting(null)}
              aria-label="Cancel quote"
              title="Cancel quote"
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M18 6 6 18" />
                <path d="m6 6 12 12" />
              </svg>
            </button>
          </div>
        )}
        <div className="composer-inner">
          <textarea
            ref={textareaRef}
            className="composer-input"
            placeholder={
              quoting ? `Reply to @${quoting.from}…` : "Reply to thread…"
            }
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
              if (e.key === "Escape" && quoting) {
                e.preventDefault();
                setQuoting(null);
              }
            }}
            rows={1}
          />
          <button
            className="composer-send"
            disabled={!text.trim() || sendReply.isPending}
            onClick={handleSend}
            aria-label="Send reply"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="m22 2-7 20-4-9-9-4Z" />
              <path d="M22 2 11 13" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  );
}

function truncate(s: string, n: number): string {
  const oneLine = s.replace(/\s+/g, " ").trim();
  return oneLine.length > n ? `${oneLine.slice(0, n - 1)}…` : oneLine;
}

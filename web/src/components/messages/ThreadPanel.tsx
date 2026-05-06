import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type QueryClient,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { getConfig, type Message, postMessage } from "../../api/client";
import { useCommands } from "../../hooks/useCommands";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useThreadMessages } from "../../hooks/useMessages";
import { extractTaggedMentions } from "../../lib/mentions";
import { handleSlashCommand, resolveLeadSlug } from "../../lib/slashCommands";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";
import {
  Autocomplete,
  type AutocompleteItem,
  applyAutocomplete,
} from "./Autocomplete";
import { MessageBubble } from "./MessageBubble";

interface MessagesQueryData {
  messages?: Message[];
}

function latestCachedMessageId(
  queryClient: QueryClient,
  channel: string,
): string | null {
  const entries = queryClient.getQueriesData<MessagesQueryData>({
    queryKey: ["messages", channel],
  });
  for (let i = entries.length - 1; i >= 0; i--) {
    const [, data] = entries[i];
    const messages = data?.messages;
    if (!messages) continue;
    for (let j = messages.length - 1; j >= 0; j--) {
      const id = messages[j]?.id?.trim();
      if (id) return id;
    }
  }
  return null;
}

function emptyMessagesQueryData(
  data: MessagesQueryData | undefined,
): MessagesQueryData | undefined {
  if (!data?.messages) return data;
  return { ...data, messages: [] };
}

// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Composer wiring fans out into mutation, autocomplete, and slash-command branches that share state.
export function ThreadPanel() {
  const activeThread = useAppStore((s) => s.activeThread);
  const setActiveThread = useAppStore((s) => s.setActiveThread);
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const setChannelClearMarker = useAppStore((s) => s.setChannelClearMarker);
  // Channel is captured at thread-open time and stored on activeThread so
  // replies posted while the user has navigated away from the originating
  // channel still land in the right place. Reading useChannelSlug() here
  // would silently route replies to the URL's current channel (or
  // "general") whenever the panel outlived the originating route.
  const activeThreadId = activeThread?.id ?? null;
  const currentChannel = activeThread?.channelSlug ?? "general";
  const [text, setText] = useState("");
  const [caret, setCaret] = useState(0);
  const [acItems, setAcItems] = useState<AutocompleteItem[]>([]);
  const [acIdx, setAcIdx] = useState(0);
  const [quoting, setQuoting] = useState<Message | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const messagesRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const { data: cfg } = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 60_000,
  });
  const { data: members = [] } = useOfficeMembers();
  const knownSlugs = useMemo(() => members.map((m) => m.slug), [members]);
  const leadSlug = useMemo(
    () => resolveLeadSlug(cfg?.team_lead_slug, members),
    [cfg?.team_lead_slug, members],
  );
  // Broker-backed slash-command registry; same source the channel composer
  // reads. Falls back to a hardcoded list if the broker is unreachable.
  const commands = useCommands();

  const { data: messages = [] } = useThreadMessages(
    currentChannel,
    activeThreadId,
  );

  // Split the thread query response into parent + replies so we can render
  // the parent prominently at the top (like Slack's thread pane). The broker
  // returns both in the same list because thread_id matches either id or
  // reply_to.
  const { parent, replies } = useMemo(() => {
    let threadParent: Message | null = null;
    const threadReplies: Message[] = [];
    for (const m of messages) {
      if (m.id === activeThreadId) threadParent = m;
      else if (m.reply_to) threadReplies.push(m);
    }
    return { parent: threadParent, replies: threadReplies };
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
  // fires against the wrong thread on the next send. activeThreadId is
  // referenced via `void` so biome's useExhaustiveDependencies accepts
  // the dep as in-body — the dep IS the trigger for this reset, dropping
  // it would silently leak drafts across threads.
  useEffect(() => {
    void activeThreadId;
    setQuoting(null);
    setText("");
    setCaret(0);
    setAcItems([]);
    setAcIdx(0);
  }, [activeThreadId]);

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
        setActiveThread(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [activeThreadId, setActiveThread]);

  // The thread target is the quoted reply if the user clicked "quote" on a
  // specific reply; otherwise it's the parent. Broker thread semantics
  // don't distinguish depth — any reply with reply_to in the chain shows up
  // under this thread_id — so quoting-a-reply just tags the new message
  // against that reply's id for display, while still appearing in the same
  // thread panel.
  const replyTarget = quoting?.id ?? activeThreadId ?? undefined;

  const sendReply = useMutation({
    mutationFn: ({
      content,
      tagged,
      target,
    }: {
      content: string;
      tagged: string[];
      target: string | undefined;
    }) => postMessage(content, currentChannel, target, tagged),
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

  const sendThreadMessage = useCallback(
    (content: string) => {
      sendReply.mutate({
        content,
        tagged: extractTaggedMentions(content, knownSlugs),
        target: replyTarget,
      });
    },
    [sendReply, knownSlugs, replyTarget],
  );

  const clearParentChannelMessages = useCallback(() => {
    const markerId = latestCachedMessageId(queryClient, currentChannel);
    setLastMessageId(null);
    setChannelClearMarker(currentChannel, markerId);
    queryClient.setQueriesData<MessagesQueryData>(
      { queryKey: ["messages", currentChannel] },
      emptyMessagesQueryData,
    );
    queryClient.invalidateQueries({ queryKey: ["messages", currentChannel] });
    showNotice("Messages cleared", "info");
  }, [currentChannel, queryClient, setChannelClearMarker, setLastMessageId]);

  const handleSend = useCallback(() => {
    const trimmed = text.trim();
    if (!trimmed || sendReply.isPending) return;

    if (trimmed.startsWith("/")) {
      const consumed = handleSlashCommand(trimmed, {
        leadSlug,
        sendAsMessage: sendThreadMessage,
        clearMessages: clearParentChannelMessages,
        channel: currentChannel,
      });
      if (consumed) {
        setText("");
        setQuoting(null);
        return;
      }
    }

    sendThreadMessage(trimmed);
  }, [
    text,
    sendReply.isPending,
    leadSlug,
    sendThreadMessage,
    clearParentChannelMessages,
    currentChannel,
  ]);

  const pickAutocomplete = useCallback(
    (item: AutocompleteItem) => {
      const next = applyAutocomplete(text, caret, item);
      setText(next.text);
      requestAnimationFrame(() => {
        const el = textareaRef.current;
        if (!el) return;
        el.focus();
        el.setSelectionRange(next.caret, next.caret);
        setCaret(next.caret);
      });
    },
    [text, caret],
  );

  const handleAcItems = useCallback((items: AutocompleteItem[]) => {
    setAcItems(items);
    setAcIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)));
  }, []);

  const syncCaret = useCallback(() => {
    const el = textareaRef.current;
    if (el) setCaret(el.selectionStart ?? 0);
  }, []);

  const handleAutocompleteKey = useCallback(
    (e: React.KeyboardEvent): boolean => {
      if (acItems.length === 0) return false;
      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          setAcIdx((i) => (i + 1) % acItems.length);
          return true;
        case "ArrowUp":
          e.preventDefault();
          setAcIdx((i) => (i - 1 + acItems.length) % acItems.length);
          return true;
        case "Enter":
        case "Tab": {
          e.preventDefault();
          const pick = acItems[acIdx] ?? acItems[0];
          if (pick) pickAutocomplete(pick);
          return true;
        }
        case "Escape":
          e.preventDefault();
          setAcItems([]);
          return true;
        default:
          return false;
      }
    },
    [acItems, acIdx, pickAutocomplete],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (handleAutocompleteKey(e)) return;
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
        return;
      }
      if (e.key === "Escape" && quoting) {
        e.preventDefault();
        setQuoting(null);
      }
    },
    [handleAutocompleteKey, handleSend, quoting],
  );

  if (!activeThreadId) return null;

  return (
    <aside className="thread-panel open" aria-label="Thread">
      <div className="thread-panel-header">
        <div className="thread-panel-title-group">
          <span className="thread-panel-title">Thread</span>
          <span className="thread-panel-channel">#{currentChannel}</span>
        </div>
        <button
          type="button"
          className="thread-panel-close"
          onClick={() => setActiveThread(null)}
          aria-label="Close thread"
          title="Close (Esc)"
        >
          <svg
            aria-hidden="true"
            focusable="false"
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
        {parent ? (
          <div className="thread-panel-parent">
            <MessageBubble message={parent} />
          </div>
        ) : null}
        {replies.length > 0 ? (
          <div className="thread-panel-replies-count">
            {replies.length} {replies.length === 1 ? "reply" : "replies"}
          </div>
        ) : null}
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
        {quoting ? (
          <div className="thread-quote-chip">
            <svg
              aria-hidden="true"
              focusable="false"
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
              type="button"
              className="thread-quote-dismiss"
              onClick={() => setQuoting(null)}
              aria-label="Cancel quote"
              title="Cancel quote"
            >
              <svg
                aria-hidden="true"
                focusable="false"
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
        ) : null}
        <Autocomplete
          value={text}
          caret={caret}
          selectedIdx={acIdx}
          onItems={handleAcItems}
          onPick={pickAutocomplete}
          commands={commands}
        />
        <div className="composer-inner">
          <textarea
            ref={textareaRef}
            className="composer-input"
            placeholder={
              quoting ? `Reply to @${quoting.from}…` : "Reply to thread…"
            }
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setCaret(e.target.selectionStart ?? 0);
            }}
            onKeyDown={handleKeyDown}
            onKeyUp={syncCaret}
            onClick={syncCaret}
            rows={1}
          />
          <button
            type="button"
            className="composer-send"
            disabled={!text.trim() || sendReply.isPending}
            onClick={handleSend}
            aria-label="Send reply"
          >
            <svg
              aria-hidden="true"
              focusable="false"
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
    </aside>
  );
}

function truncate(s: string, n: number): string {
  const oneLine = s.replace(/\s+/g, " ").trim();
  return oneLine.length > n ? `${oneLine.slice(0, n - 1)}…` : oneLine;
}

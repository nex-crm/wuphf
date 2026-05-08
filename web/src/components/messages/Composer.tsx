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
import {
  extractTaggedMentions,
  parseMentions,
  renderMentionTokens,
} from "../../lib/mentions";
import {
  askPrefix,
  handleSlashCommand,
  resolveLeadSlug,
  unknownSlashCommandMessage,
} from "../../lib/slashCommands";
import { useChannelSlug } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { showNotice } from "../ui/Toast";
import {
  Autocomplete,
  type AutocompleteItem,
  applyAutocomplete,
} from "./Autocomplete";

/** How many sent messages to keep in per-channel history. */
const COMPOSER_HISTORY_LIMIT = 20;

/** sessionStorage key shape: `wuphf:composer-history:<channel>`. */
function historyKey(channel: string): string {
  return `wuphf:composer-history:${channel || "general"}`;
}

function readHistory(channel: string): string[] {
  try {
    const raw = sessionStorage.getItem(historyKey(channel));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (v): v is string => typeof v === "string" && v.length > 0,
    );
  } catch {
    return [];
  }
}

function writeHistory(channel: string, entries: string[]): void {
  try {
    sessionStorage.setItem(historyKey(channel), JSON.stringify(entries));
  } catch {
    // sessionStorage disabled / quota exceeded — silently drop history rather
    // than blowing up the send flow. The user still sees their message land.
  }
}

/**
 * Append a sent message to the per-channel history, trimming to the most
 * recent COMPOSER_HISTORY_LIMIT entries. Skips duplicates of the latest
 * entry so rapid resends do not pollute recall.
 */
function pushHistory(channel: string, message: string): void {
  const trimmed = message.trim();
  if (!trimmed) return;
  const current = readHistory(channel);
  if (current.length > 0 && current[current.length - 1] === trimmed) return;
  const next = [...current, trimmed].slice(-COMPOSER_HISTORY_LIMIT);
  writeHistory(channel, next);
}

interface MessagesQueryData {
  messages?: Message[];
}

function latestMessageIdFromQueryData(
  data: MessagesQueryData | undefined,
): string | null {
  if (!data?.messages) return null;
  for (let i = data.messages.length - 1; i >= 0; i--) {
    const id = data.messages[i]?.id?.trim();
    if (id) return id;
  }
  return null;
}

function latestCachedMessageId(
  queryClient: QueryClient,
  channel: string,
): string | null {
  const entries = queryClient.getQueriesData<MessagesQueryData>({
    queryKey: ["messages", channel],
  });
  for (let i = entries.length - 1; i >= 0; i--) {
    const id = latestMessageIdFromQueryData(entries[i][1]);
    if (id) return id;
  }
  return null;
}

function emptyMessagesQueryData(
  data: MessagesQueryData | undefined,
): MessagesQueryData | undefined {
  if (!data?.messages) return data;
  return { ...data, messages: [] };
}

interface OutboundMessage {
  content: string;
  tagged: string[];
}

/**
 * History recall state. `draftStash` holds whatever the operator had typed
 * before the first Ctrl+P so we can restore it when they walk forward past
 * the end of history.
 */
interface HistoryState {
  /** -1 when live, else index into the cached history array. */
  index: number;
  /** Draft text to restore when stepping past the end. */
  draftStash: string | null;
  /** Snapshot taken at recall start; kept so mid-recall writes don't churn it. */
  entries: string[];
}

function emptyHistoryState(): HistoryState {
  return { index: -1, draftStash: null, entries: [] };
}

// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function Composer() {
  const currentChannel = useChannelSlug() ?? "general";
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const setChannelClearMarker = useAppStore((s) => s.setChannelClearMarker);
  const [text, setText] = useState("");
  const [caret, setCaret] = useState(0);
  const [acItems, setAcItems] = useState<AutocompleteItem[]>([]);
  const [acIdx, setAcIdx] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const mirrorRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const { data: cfg } = useQuery({
    queryKey: ["config"],
    queryFn: getConfig,
    staleTime: 60_000,
  });
  const { data: members = [] } = useOfficeMembers();
  const leadSlug = useMemo(
    () => resolveLeadSlug(cfg?.team_lead_slug, members),
    [cfg?.team_lead_slug, members],
  );
  // Slugs the mirror-overlay recognises as mention chips. Memoed against
  // the member list reference so the token parse downstream doesn't
  // re-allocate on every Composer render.
  const knownSlugs = useMemo(() => members.map((m) => m.slug), [members]);
  const mentionTokens = useMemo(
    () => parseMentions(text, knownSlugs),
    [text, knownSlugs],
  );
  // Broker-backed slash-command registry. Falls back to the hardcoded
  // list if the broker is unreachable so the composer is never worse
  // than before this plumbing landed.
  const commands = useCommands();

  const historyRef = useRef<HistoryState>(emptyHistoryState());

  // Reset recall when switching channels so Ctrl+P replays *this* channel.
  useEffect(() => {
    historyRef.current = emptyHistoryState();
  }, []);

  const resetRecall = useCallback(() => {
    historyRef.current = emptyHistoryState();
  }, []);

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

  const sendMutation = useMutation({
    mutationFn: ({ content, tagged }: OutboundMessage) =>
      postMessage(content, currentChannel, undefined, tagged),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["messages", currentChannel] });
    },
    onError: (err: unknown) => {
      const message =
        err instanceof Error ? err.message : "Failed to send message";
      // The broker blocks chat with 409 + "request pending; answer required"
      // for approval-style requests. The request UI above the composer lets
      // the user answer or dismiss/cancel it without leaving the textbox.
      if (/request pending|answer required/i.test(message)) {
        showNotice(
          "Answer or dismiss the request above to send messages.",
          "info",
        );
        return;
      }
      showNotice(message, "error");
    },
  });

  /**
   * Clear the composer, shrink the textarea, and cancel any pending recall.
   * Called after every successful send or consumed command.
   */
  const resetComposer = useCallback(() => {
    setText("");
    resetRecall();
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [resetRecall]);

  const clearCurrentChannelMessages = useCallback(() => {
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
    if (!trimmed || sendMutation.isPending) return;

    // Handle slash commands
    if (trimmed.startsWith("/")) {
      const consumed = handleSlashCommand(trimmed, {
        leadSlug,
        sendAsMessage: (rewritten) => {
          sendMutation.mutate({
            content: rewritten,
            tagged: extractTaggedMentions(rewritten, knownSlugs),
          });
        },
        clearMessages: clearCurrentChannelMessages,
        channel: currentChannel,
      });
      if (consumed) {
        // Persist the *raw* command to history so Ctrl+P replays `/ask foo`,
        // not the rewritten `@ceo foo`. Matches user expectation.
        pushHistory(currentChannel, trimmed);
        resetComposer();
        return;
      }
    }

    pushHistory(currentChannel, trimmed);
    sendMutation.mutate({
      content: trimmed,
      tagged: extractTaggedMentions(trimmed, knownSlugs),
    });
    resetComposer();
  }, [
    text,
    sendMutation,
    leadSlug,
    currentChannel,
    resetComposer,
    knownSlugs,
    clearCurrentChannelMessages,
  ]);

  /**
   * Walk backward through history. On first invocation, snapshot the live
   * draft so Ctrl+N can restore it. Returns true if recall succeeded.
   */
  const recallPrevious = useCallback((): boolean => {
    const state = historyRef.current;
    if (state.index === -1) {
      const entries = readHistory(currentChannel);
      if (entries.length === 0) return false;
      state.entries = entries;
      state.draftStash = text;
      state.index = entries.length;
    }
    if (state.index <= 0) return false;
    state.index -= 1;
    setText(state.entries[state.index]);
    return true;
  }, [currentChannel, text]);

  /**
   * Walk forward through history. When we run off the end, restore the
   * original draft and clear recall state.
   */
  const recallNext = useCallback((): boolean => {
    const state = historyRef.current;
    if (state.index === -1) return false;
    if (state.index < state.entries.length - 1) {
      state.index += 1;
      setText(state.entries[state.index]);
      return true;
    }
    setText(state.draftStash ?? "");
    historyRef.current = emptyHistoryState();
    return true;
  }, []);

  const moveCaretToEnd = useCallback(() => {
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      const end = el.value.length;
      el.setSelectionRange(end, end);
      setCaret(end);
    });
  }, []);

  const handleKeyDown = useCallback(
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
    (e: React.KeyboardEvent) => {
      // Autocomplete navigation runs first
      if (acItems.length > 0) {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          setAcIdx((i) => (i + 1) % acItems.length);
          return;
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          setAcIdx((i) => (i - 1 + acItems.length) % acItems.length);
          return;
        }
        if (e.key === "Enter" || e.key === "Tab") {
          e.preventDefault();
          const pick = acItems[acIdx] ?? acItems[0];
          if (pick) pickAutocomplete(pick);
          return;
        }
        if (e.key === "Escape") {
          e.preventDefault();
          setAcItems([]);
          return;
        }
      }

      // History recall — Ctrl+P / Ctrl+N (TUI parity: internal/tui/interaction.go:56-58)
      if (e.ctrlKey && !e.metaKey && !e.altKey) {
        if ((e.key === "p" || e.key === "P") && recallPrevious()) {
          e.preventDefault();
          moveCaretToEnd();
          return;
        }
        if ((e.key === "n" || e.key === "N") && recallNext()) {
          e.preventDefault();
          moveCaretToEnd();
          return;
        }
      }

      // Slack-style: empty-draft ArrowUp recalls the last message.
      if (
        e.key === "ArrowUp" &&
        !e.shiftKey &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        text === "" &&
        recallPrevious()
      ) {
        e.preventDefault();
        moveCaretToEnd();
        return;
      }

      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [
      handleSend,
      acItems,
      acIdx,
      pickAutocomplete,
      recallPrevious,
      recallNext,
      text,
      moveCaretToEnd,
    ],
  );

  const handleAcItems = useCallback((items: AutocompleteItem[]) => {
    setAcItems(items);
    setAcIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)));
  }, []);

  const syncCaret = useCallback(() => {
    const el = textareaRef.current;
    if (el) setCaret(el.selectionStart ?? 0);
  }, []);

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
    }
  }, []);

  // Keep the mirror overlay scroll-locked to the textarea. Once content
  // overflows the 120px cap, the textarea scrolls internally; the mirror
  // has no scroll constraint of its own, so without this the chips would
  // drift out of alignment with the visible text rows.
  const syncScroll = useCallback(() => {
    const src = textareaRef.current;
    const dst = mirrorRef.current;
    if (src && dst) dst.scrollTop = src.scrollTop;
  }, []);

  return (
    <div className="composer">
      <Autocomplete
        value={text}
        caret={caret}
        selectedIdx={acIdx}
        onItems={handleAcItems}
        onPick={pickAutocomplete}
        commands={commands}
      />
      <div className="composer-inner">
        <div className="composer-field">
          {/* Mirror overlay: renders the same text as the textarea but with
              mention chips. The textarea sits on top with transparent text
              and a visible caret so the user still sees and edits the raw
              string — only the chips are styled. aria-hidden because the
              textarea is the interactive source of truth. */}
          <div ref={mirrorRef} className="composer-mirror" aria-hidden="true">
            {renderMentionTokens(mentionTokens)}
            {/* Trailing newline so the mirror height matches a textarea
                that ends on a blank line (otherwise the chip layout
                truncates by one row). */}
            {"\n"}
          </div>
          <textarea
            ref={textareaRef}
            className="composer-input"
            placeholder={`Message #${currentChannel}`}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setCaret(e.target.selectionStart ?? 0);
              handleInput();
              syncScroll();
              // Any manual edit cancels history recall.
              if (historyRef.current.index !== -1) {
                resetRecall();
              }
            }}
            onKeyDown={handleKeyDown}
            onKeyUp={syncCaret}
            onClick={syncCaret}
            onScroll={syncScroll}
            rows={1}
          />
        </div>
        <button
          type="button"
          className="composer-send"
          disabled={!text.trim() || sendMutation.isPending}
          onClick={handleSend}
          aria-label="Send message"
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
  );
}

// Re-export helpers for testing.
export const __test__ = {
  historyKey,
  readHistory,
  writeHistory,
  pushHistory,
  unknownSlashCommandMessage,
  handleSlashCommand,
  resolveLeadSlug,
  askPrefix,
  latestMessageIdFromQueryData,
  emptyMessagesQueryData,
  COMPOSER_HISTORY_LIMIT,
};

import { useMemo } from "react";
import ReactMarkdown from "react-markdown";

import type { Message } from "../../api/client";
import { toggleReaction } from "../../api/client";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useOfficeMembers } from "../../hooks/useMembers";
import { formatTime, formatTokens } from "../../lib/format";
import { resolveHarness } from "../../lib/harness";
import { renderMentions } from "../../lib/mentions";
import {
  messageMarkdownComponents,
  messageRemarkPlugins,
} from "../../lib/messageMarkdown";
import { useChannelSlug } from "../../routes/useCurrentRoute";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { RedactedBadge } from "../ui/RedactedBadge";
import { showNotice } from "../ui/Toast";

interface MessageBubbleProps {
  message: Message;
  grouped?: boolean;
  /** Direct reply to a top-level channel message — renders indented under the parent. */
  isReply?: boolean;
  /** Count of direct replies to this message. Shows an "N replies" affordance. */
  replyCount?: number;
  /** Open the thread panel for this message. Shown as a hover action when provided. */
  onOpenThread?: (id: string) => void;
  /** Reply-to-this-reply inside the thread panel. Shown as a hover action when provided. */
  onQuoteReply?: (message: Message) => void;
  /** Copy a permalink to this message. Shown as a hover action when provided. */
  onCopyLink?: (id: string) => void;
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function MessageBubble({
  message,
  grouped = false,
  isReply = false,
  replyCount = 0,
  onOpenThread,
  onQuoteReply,
  onCopyLink,
}: MessageBubbleProps) {
  const currentChannel = useChannelSlug() ?? "general";
  const { data: members = [] } = useOfficeMembers();
  const isHuman =
    message.from === "you" ||
    message.from === "human" ||
    message.from.startsWith("human:");
  const isLocalUser = message.from === "you";
  const teamMemberDisplayName =
    isHuman && !isLocalUser
      ? message.from.startsWith("human:")
        ? message.from.slice("human:".length).replace(/-/g, " ") ||
          "team member"
        : "Human"
      : null;
  const agent = members.find((m) => m.slug === message.from);
  const defaultHarness = useDefaultHarness();
  const harness = !isHuman
    ? resolveHarness(agent?.provider, defaultHarness)
    : null;

  const usageTotal = message.usage
    ? (message.usage.total_tokens ??
      (message.usage.input_tokens ?? 0) +
        (message.usage.output_tokens ?? 0) +
        (message.usage.cache_read_tokens ?? 0) +
        (message.usage.cache_creation_tokens ?? 0))
    : 0;

  const reactions = message.reactions
    ? Array.isArray(message.reactions)
      ? (message.reactions as Array<{ emoji: string; count?: number }>)
      : Object.entries(message.reactions).map(([emoji, users]) => ({
          emoji,
          count: Array.isArray(users) ? users.length : 1,
        }))
    : [];

  // SECURITY: agent messages render through ReactMarkdown with a remark and
  // components pipeline (../../lib/messageMarkdown). ReactMarkdown's default
  // urlTransform strips javascript:/vbscript:/data: URIs; the anchor renderer
  // adds a second-layer scheme allowlist. The legacy regex-based formatMarkdown
  // path that used the React unsafe-HTML prop has been removed (it had been
  // independently hardened on main via isSafeUrl(), but the lib swap is more
  // durable: react-markdown is a battle-tested mdast pipeline, and the
  // dedicated XSS test file web/src/lib/messageMarkdown.test.tsx (23 tests)
  // covers javascript:/data:/vbscript:, image src, GFM autolinks, and raw
  // HTML. Local-LLM agent content (mlx-lm, ollama, exo) flows through the
  // same path so the XSS posture applies uniformly. Human input takes the
  // safe ReactNode path via renderMentions.

  // Turn human text like "@pm when are you free?" into mention chips for
  // registered agent slugs. Non-agent @-references stay plain text. The
  // memo keys on content + the slug list so rapid renders don't re-parse.
  const knownSlugs = useMemo(() => members.map((m) => m.slug), [members]);
  const humanRendered = useMemo(
    () => (isHuman ? renderMentions(message.content || "", knownSlugs) : null),
    [isHuman, message.content, knownSlugs],
  );

  // Status messages — compact
  if (message.content?.startsWith("[STATUS]")) {
    const statusText = message.content.replace(/^\[STATUS\]\s*/, "");
    return <div className="message-status animate-fade">{statusText}</div>;
  }

  return (
    <div
      className={`message animate-fade${grouped ? " message-grouped" : ""}${isReply ? " message-reply" : ""}`}
      data-msg-id={message.id}
      // Precise author selectors so e2e specs can filter without parsing
      // textContent. `data-author-kind` is "human" | "agent"; `data-author-slug`
      // carries the raw `from` (e.g. "you", "human", or an agent slug like "planner").
      data-author-kind={isHuman ? "human" : "agent"}
      data-author-slug={message.from}
    >
      {/* Avatar */}
      <div
        className={`message-avatar${isHuman ? "" : " avatar-with-harness"}`}
        style={
          isHuman
            ? {
                background: "var(--bg-warm)",
                color: "var(--text-secondary)",
                fontSize: 12,
                fontWeight: 600,
              }
            : undefined
        }
      >
        {isLocalUser ? (
          "You"
        ) : teamMemberDisplayName ? (
          teamMemberDisplayName.slice(0, 1).toUpperCase()
        ) : (
          <>
            <PixelAvatar slug={message.from} size={24} />
            {harness ? (
              <HarnessBadge
                kind={harness}
                size={14}
                className="harness-badge-on-avatar"
              />
            ) : null}
          </>
        )}
      </div>

      {/* Content */}
      <div className="message-content">
        {/* Header */}
        <div className="message-header">
          <span className="message-author">
            {isLocalUser
              ? "You"
              : teamMemberDisplayName || agent?.name || message.from}
          </span>
          {isHuman ? (
            <span className="badge badge-neutral">human</span>
          ) : agent?.role ? (
            <span className="badge badge-green">{agent.role}</span>
          ) : null}
          <span className="message-time" title={message.timestamp}>
            {formatTime(message.timestamp)}
          </span>
          {usageTotal > 0 && (
            <span className="message-token-badge">
              {formatTokens(usageTotal)} tok
            </span>
          )}
          {Boolean(message.redacted) && (
            <RedactedBadge reasons={message.redaction_reasons} />
          )}
        </div>

        {/* Text — humans render mention chips via safe ReactNode children;
            agent messages render through ReactMarkdown (no raw HTML). */}
        {isHuman ? (
          <div className="message-text">{humanRendered}</div>
        ) : (
          <div className="message-text">
            <ReactMarkdown
              remarkPlugins={messageRemarkPlugins}
              components={messageMarkdownComponents}
              skipHtml={true}
            >
              {message.content || ""}
            </ReactMarkdown>
          </div>
        )}

        {/* Reactions */}
        {reactions.length > 0 && (
          <div className="message-reactions">
            {reactions.map((r) => (
              <button
                type="button"
                key={r.emoji}
                className="reaction-pill"
                onClick={() => {
                  toggleReaction(message.id, r.emoji, currentChannel).catch(
                    (e: Error) =>
                      showNotice(`Reaction failed: ${e.message}`, "error"),
                  );
                }}
              >
                <span>{r.emoji}</span>
                <span className="reaction-pill-count">{r.count ?? 1}</span>
              </button>
            ))}
          </div>
        )}

        {/* Thread summary — shown under a parent that has replies. Clicking
            opens the thread panel where the full chain is browsable. */}
        {replyCount > 0 && onOpenThread && (
          <button
            type="button"
            className="inline-thread-toggle"
            onClick={() => onOpenThread(message.id)}
            title="Open thread"
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
              <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
            </svg>
            {replyCount} {replyCount === 1 ? "reply" : "replies"}
          </button>
        )}
      </div>

      {/* Hover actions — reply in thread, quote, copy link. Absolutely
          positioned so they don't change the bubble's flow layout. */}
      {onOpenThread || onQuoteReply || onCopyLink ? (
        <div
          className="message-hover-actions"
          role="toolbar"
          aria-label="Message actions"
        >
          {onOpenThread ? (
            <button
              type="button"
              className="message-hover-btn"
              onClick={() => onOpenThread(message.id)}
              title="Reply in thread"
              aria-label="Reply in thread"
            >
              <svg
                aria-hidden="true"
                focusable="false"
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
              </svg>
            </button>
          ) : null}
          {onQuoteReply ? (
            <button
              type="button"
              className="message-hover-btn"
              onClick={() => onQuoteReply(message)}
              title="Quote-reply"
              aria-label="Quote-reply"
            >
              <svg
                aria-hidden="true"
                focusable="false"
                width="14"
                height="14"
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
            </button>
          ) : null}
          {onCopyLink ? (
            <button
              type="button"
              className="message-hover-btn"
              onClick={() => onCopyLink(message.id)}
              title="Copy link"
              aria-label="Copy link"
            >
              <svg
                aria-hidden="true"
                focusable="false"
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.72-1.71" />
              </svg>
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

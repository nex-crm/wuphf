import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts, slack, agentEmojis } from "../theme";
import { PixelAvatar } from "./PixelAvatar";

interface ChatMessageProps {
  name: string;
  color: string;
  text: string;
  enterFrame: number;
  isStreaming?: boolean;
  timestamp?: string;
  isReply?: boolean;
  /** First reply in a stack — adds breathing room above it. */
  firstOfStack?: boolean;
  mentions?: { name: string; color: string }[];
}

// Parse text and colorize @mentions
const renderTextWithMentions = (
  text: string,
  mentions?: { name: string; color: string }[]
): React.ReactNode[] => {
  if (!mentions || mentions.length === 0) return [text];

  const parts: React.ReactNode[] = [];
  let remaining = text;
  let key = 0;

  for (const m of mentions) {
    const tag = `@${m.name.toLowerCase().replace(/ /g, "")}`;
    // Try common patterns
    const patterns = [tag, `@${m.name.split(" ")[0].toLowerCase()}`, `@${m.name.toLowerCase()}`];
    for (const pat of patterns) {
      const idx = remaining.toLowerCase().indexOf(pat.toLowerCase());
      if (idx !== -1) {
        if (idx > 0) parts.push(remaining.slice(0, idx));
        parts.push(
          <span key={key++} style={{
            color: colors.mentionText,
            backgroundColor: colors.mentionBg,
            fontWeight: 600,
            fontSize: "0.9em",
            borderRadius: 3,
            padding: "0 5px",
          }}>
            {remaining.slice(idx, idx + pat.length)}
          </span>
        );
        remaining = remaining.slice(idx + pat.length);
        break;
      }
    }
  }
  if (remaining) parts.push(remaining);
  return parts;
};

export const ChatMessage: React.FC<ChatMessageProps> = ({
  name,
  color,
  text,
  enterFrame,
  isStreaming = false,
  timestamp = "just now",
  isReply = false,
  firstOfStack = false,
  mentions,
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - enterFrame;

  const opacity = interpolate(elapsed, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.25, 0.46, 0.45, 0.94),
  });

  const translateY = interpolate(elapsed, [0, 12], [10, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.25, 0.46, 0.45, 0.94),
  });

  const visibleChars = isStreaming
    ? Math.min(text.length, Math.floor(Math.max(0, elapsed - 4) * 1.5))
    : elapsed >= 0 ? text.length : 0;

  if (elapsed < 0) return null;

  // Derive avatar slug from agent name
  const avatarSlug = name === "You" || name === "human"
    ? "generic"
    : Object.keys(agentEmojis).find(k => name.toLowerCase().includes(k)) ?? name.toLowerCase().replace(/ /g, "").slice(0, 3);

  const visibleText = text.slice(0, visibleChars);
  const renderedText = renderTextWithMentions(visibleText, mentions);

  // Real MessageBubble + .message-reply proportions, scaled ~1.25× for 1080p.
  // Reply has smaller avatar, tighter header, slightly smaller text, and
  // sits under a continuous thread rail aligned with the parent avatar's
  // right edge.
  const avatarBox = isReply ? 32 : 44;
  const avatarPad = isReply ? 5 : 8;
  const innerAvatar = avatarBox - avatarPad * 2;

  return (
    <div
      style={{
        opacity,
        transform: `translateY(${translateY}px)`,
        display: "flex",
        gap: isReply ? 10 : 14,
        paddingLeft: isReply ? 58 : 0,
        paddingTop: isReply ? 8 : 0,
        paddingBottom: isReply ? 8 : 0,
        // replies cancel container gap (padding handles spacing); first reply of
        // a stack keeps 8px of it as breathing room from the parent message
        marginTop: isReply ? (firstOfStack ? -2 : -10) : 0,
        position: "relative",
      }}
    >
      {/* Thread rail — continuous line behind stacked replies
          (mirrors .thread-replies::before in messages.css) */}
      {isReply && (
        <div style={{
          position: "absolute",
          left: 44,
          top: 0,
          bottom: 0,
          width: 2,
          borderRadius: 1,
          background: "#cfd1d2",   // neutral-200 / border-dark
          opacity: 0.7,
        }} />
      )}

      {/* Avatar — padded tile with pixel art inside (matches .message-avatar) */}
      <div style={{
        width: avatarBox,
        height: avatarBox,
        padding: avatarPad,
        boxSizing: "border-box",
        borderRadius: isReply ? 7 : 8,
        flexShrink: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: slack.bgWarm,
      }}>
        <PixelAvatar slug={avatarSlug} color={color} size={innerAvatar} />
      </div>

      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{
          display: "flex",
          alignItems: "baseline",
          gap: isReply ? 8 : 10,
          marginBottom: isReply ? 2 : 4,
        }}>
          <span style={{
            fontFamily: fonts.sans,
            fontSize: isReply ? 14 : 16,
            fontWeight: 600,
            color: slack.text,
          }}>
            {name}
          </span>
          <span style={{
            fontFamily: fonts.sans,
            fontSize: isReply ? 11.5 : 13,
            color: slack.textTertiary,
          }}>
            {timestamp}
          </span>
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: isReply ? 15 : 16,
          lineHeight: isReply ? 1.5 : 1.6,
          color: slack.textSecondary,
        }}>
          {renderedText}
          {isStreaming && visibleChars < text.length && (
            <span style={{ opacity: Math.floor(frame / 8) % 2 === 0 ? 1 : 0.3, color: slack.accent }}>|</span>
          )}
        </div>
      </div>
    </div>
  );
};

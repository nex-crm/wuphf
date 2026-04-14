import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";
import { fonts, slack, agentEmojis } from "../theme";
import { PixelAvatar } from "./PixelAvatar";

interface ChatMessageProps {
  name: string;
  color: string;
  text: string;
  enterFrame: number;
  isStreaming?: boolean;
  timestamp?: string;
  isReply?: boolean;
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
          <span key={key++} style={{ color: m.color, fontWeight: 700, backgroundColor: `${m.color}15`, borderRadius: 3, padding: "1px 3px" }}>
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
  mentions,
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - enterFrame;

  const opacity = interpolate(elapsed, [0, 6], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  const translateY = interpolate(elapsed, [0, 6], [8, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
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

  return (
    <div
      style={{
        opacity,
        transform: `translateY(${translateY}px)`,
        display: "flex",
        gap: 14,
        padding: isReply ? "4px 20px 4px 56px" : "6px 24px",
      }}
    >
      {/* Reply indent line */}
      {isReply && (
        <div style={{
          position: "absolute",
          left: 36,
          top: 0,
          bottom: 0,
          width: 2,
          backgroundColor: `${color}30`,
          borderRadius: 1,
        }} />
      )}

      {/* Avatar — pixel art from platform's sprite data */}
      <PixelAvatar slug={avatarSlug} color={color} size={42} />

      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "baseline", gap: 12, marginBottom: 6 }}>
          <span style={{ fontFamily: fonts.sans, fontSize: 22, fontWeight: 700, color: slack.text }}>
            {name}
          </span>
          <span style={{ fontFamily: fonts.sans, fontSize: 16, color: slack.textTertiary }}>
            {timestamp}
          </span>
        </div>
        <div style={{ fontFamily: fonts.sans, fontSize: 22, lineHeight: 1.55, color: slack.text }}>
          {renderedText}
          {isStreaming && visibleChars < text.length && (
            <span style={{ opacity: Math.floor(frame / 8) % 2 === 0 ? 1 : 0.3, color: slack.accent }}>|</span>
          )}
        </div>
      </div>
    </div>
  );
};

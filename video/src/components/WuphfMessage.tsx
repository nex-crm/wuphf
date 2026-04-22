import React from "react";
import { colors, fonts, starterAgents } from "../theme";
import { PixelAvatar } from "./PixelAvatar";
import { TypingDots } from "./TypingDots";

interface WuphfMessageProps {
  /** Agent slug ("ceo", "gtm", "eng", "pm", "designer") or "human" / "you". */
  from: string;
  /** Message body. Plain text for human, styled ReactNode for agents. */
  children?: React.ReactNode;
  /** Token count (displayed as a small pill next to the time). */
  tokens?: number;
  /** Agent role ("engineering" | "product" | …) — rendered as a green badge. */
  role?: string;
  /** Timestamp label, e.g. "10:24 AM". */
  time?: string;
  /** If true, render an empty body with a `TypingDots` in its place. */
  typing?: boolean;
  /** Extra style override for the wrapper. */
  style?: React.CSSProperties;
}

function resolveAgentColor(slug: string): string {
  const match = starterAgents.find((a) => a.slug === slug);
  return match?.color ?? colors.accent;
}

function resolveAgentName(slug: string): string {
  if (slug === "human" || slug === "you") return "You";
  const match = starterAgents.find((a) => a.slug === slug);
  return match?.name ?? slug;
}

export const WuphfMessage: React.FC<WuphfMessageProps> = ({
  from,
  children,
  tokens,
  role,
  time = "10:24 AM",
  typing,
  style,
}) => {
  const isHuman = from === "human" || from === "you";
  const color = resolveAgentColor(from);
  const name = resolveAgentName(from);

  return (
    <div
      style={{
        display: "flex",
        gap: 14,
        padding: "10px 0",
        fontFamily: fonts.sans,
        ...style,
      }}
    >
      {/* Avatar */}
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          background: colors.msgAvatarBg,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
          fontSize: 13,
          fontWeight: 600,
          color: colors.textSecondary,
          overflow: "hidden",
        }}
      >
        {isHuman ? "You" : <PixelAvatar slug={from} color={color} size={34} />}
      </div>

      {/* Body */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div
          style={{
            display: "flex",
            alignItems: "baseline",
            gap: 8,
            marginBottom: 4,
          }}
        >
          <span style={{ fontSize: 14, fontWeight: 600, color: colors.text }}>{name}</span>
          {isHuman ? (
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                height: 18,
                padding: "0 6px",
                borderRadius: 3,
                background: colors.bgWarm,
                color: colors.textSecondary,
                fontSize: 11,
                fontWeight: 500,
              }}
            >
              human
            </span>
          ) : role ? (
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                height: 18,
                padding: "0 6px",
                borderRadius: 3,
                background: colors.greenBg,
                color: colors.green,
                fontSize: 11,
                fontWeight: 500,
              }}
            >
              {role}
            </span>
          ) : null}
          <span style={{ fontSize: 11, color: colors.textTertiary }}>{time}</span>
          {tokens !== undefined && (
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                padding: "1px 6px",
                borderRadius: 999,
                background: colors.msgTokenBg,
                border: `1px solid ${colors.msgTokenBorder}`,
                color: colors.msgTokenText,
                fontFamily: fonts.mono,
                fontSize: 11,
                fontWeight: 600,
              }}
            >
              {formatTokens(tokens)} tok
            </span>
          )}
        </div>
        <div
          style={{
            fontSize: 15,
            lineHeight: 1.6,
            color: isHuman ? colors.text : colors.textSecondary,
          }}
        >
          {typing ? <TypingDots color={colors.textTertiary} /> : children}
        </div>
      </div>
    </div>
  );
};

export const Mention: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <span
    style={{
      display: "inline-block",
      background: colors.mentionBg,
      color: colors.mentionText,
      padding: "0 5px",
      borderRadius: 3,
      fontWeight: 600,
      fontSize: "0.95em",
      lineHeight: 1.4,
      verticalAlign: "baseline",
    }}
  >
    {children}
  </span>
);

export const InlineCode: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <code
    style={{
      background: colors.bgWarm,
      borderRadius: 4,
      padding: "1px 5px",
      fontFamily: fonts.mono,
      fontSize: 13,
      color: colors.text,
    }}
  >
    {children}
  </code>
);

function formatTokens(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

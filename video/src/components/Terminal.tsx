import React from "react";
import { colors, fonts } from "../theme";

interface TerminalProps {
  children: React.ReactNode;
  title?: string;
  style?: React.CSSProperties;
}

export const Terminal: React.FC<TerminalProps> = ({
  children,
  title = "Terminal",
  style,
}) => {
  return (
    <div
      style={{
        backgroundColor: colors.bgTerminal,
        borderRadius: 12,
        overflow: "hidden",
        boxShadow: "0 25px 50px rgba(0,0,0,0.5)",
        border: "1px solid #30363D",
        ...style,
      }}
    >
      {/* Title bar */}
      <div
        style={{
          backgroundColor: "#161B22",
          padding: "12px 16px",
          display: "flex",
          alignItems: "center",
          gap: 8,
          borderBottom: "1px solid #30363D",
        }}
      >
        <div style={{ width: 12, height: 12, borderRadius: "50%", backgroundColor: "#FF5F57" }} />
        <div style={{ width: 12, height: 12, borderRadius: "50%", backgroundColor: "#FEBC2E" }} />
        <div style={{ width: 12, height: 12, borderRadius: "50%", backgroundColor: "#28C840" }} />
        <span
          style={{
            marginLeft: 8,
            color: colors.textMuted,
            fontSize: 13,
            fontFamily: fonts.mono,
          }}
        >
          {title}
        </span>
      </div>
      {/* Content */}
      <div
        style={{
          padding: "20px 24px",
          fontFamily: fonts.mono,
          fontSize: 18,
          lineHeight: 1.6,
          color: colors.text,
        }}
      >
        {children}
      </div>
    </div>
  );
};

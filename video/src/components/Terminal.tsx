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
        borderRadius: 24,
        overflow: "hidden",
        boxShadow: "0 0 0 2px rgba(255,255,255,0.1), 0 25px 60px rgba(0,0,0,0.35)",
        ...style,
      }}
    >
      {/* Title bar — unified with content bg, no separator */}
      <div
        style={{
          padding: "20px 24px",
          display: "flex",
          alignItems: "center",
          gap: 10,
        }}
      >
        <div style={{ width: 16, height: 16, borderRadius: "50%", backgroundColor: "#FF5F57" }} />
        <div style={{ width: 16, height: 16, borderRadius: "50%", backgroundColor: "#FEBC2E" }} />
        <div style={{ width: 16, height: 16, borderRadius: "50%", backgroundColor: "#28C840" }} />
        <span
          style={{
            marginLeft: 14,
            color: "#85898b",
            fontSize: 16,
            fontFamily: fonts.mono,
          }}
        >
          {title}
        </span>
      </div>
      {/* Content */}
      <div
        style={{
          padding: "8px 28px 28px",
          fontFamily: fonts.mono,
          fontSize: 26,
          lineHeight: 1.7,
          color: "#85898b",
        }}
      >
        {children}
      </div>
    </div>
  );
};

import React from "react";
import { fonts } from "../theme";

// Floating yellow label callout used above WUPHF composition windows.
// Usage: drop as a child of the scene's AbsoluteFill (which should have
// position: relative so the absolute positioning anchors there).
//
//   <AbsoluteFill style={{ ..., position: "relative" }}>
//     <WuphfLabel>Tasks</WuphfLabel>
//     ...
//   </AbsoluteFill>

interface WuphfLabelProps {
  children: React.ReactNode;
  /** Distance from the top of the canvas in px (default 48). */
  top?: number;
  /** Swap the fill color if you need a different accent. */
  background?: string;
  color?: string;
}

export const WuphfLabel: React.FC<WuphfLabelProps> = ({
  children,
  top = 48,
  background = "#D4DB18",
  color = "#000000",
}) => (
  <div
    style={{
      position: "absolute",
      top,
      left: "50%",
      transform: "translateX(-50%)",
      background,
      color,
      padding: "8px 20px",
      borderRadius: 16,
      fontFamily: fonts.sans,
      fontSize: 64,
      fontWeight: 600,
      letterSpacing: -1.2,
      lineHeight: 1,
      zIndex: 10,
    }}
  >
    {children}
  </div>
);

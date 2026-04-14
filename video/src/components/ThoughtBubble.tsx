import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";
import { fonts } from "../theme";

interface ThoughtBubbleProps {
  text: string;
  enterFrame: number;
  color: string;
  side?: "right" | "left";
  x: number;
  y: number;
}

export const ThoughtBubble: React.FC<ThoughtBubbleProps> = ({
  text,
  enterFrame,
  color,
  side = "right",
  x,
  y,
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - enterFrame;

  const opacity = interpolate(elapsed, [0, 8, 50, 58], [0, 1, 1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  const scale = interpolate(elapsed, [0, 10], [0.7, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.back(1.5)),
  });

  const float = Math.sin(elapsed * 0.08) * 3;

  if (elapsed < 0) return null;

  const offsetX = side === "right" ? 30 : -200;

  return (
    <div
      style={{
        position: "absolute",
        left: x + offsetX,
        top: y - 20 + float,
        opacity,
        transform: `scale(${scale})`,
        zIndex: 100,
        pointerEvents: "none" as const,
      }}
    >
      {/* Bubble */}
      <div
        style={{
          backgroundColor: "#2A2D32",
          border: `1px solid ${color}40`,
          borderRadius: 14,
          padding: "8px 14px",
          maxWidth: 180,
          fontFamily: fonts.sans,
          fontSize: 12,
          fontStyle: "italic",
          color: "#CCC",
          lineHeight: 1.4,
          boxShadow: "0 4px 16px rgba(0,0,0,0.3)",
        }}
      >
        {text}
      </div>
      {/* Bubble tail dots */}
      <div
        style={{
          display: "flex",
          gap: 4,
          marginTop: 4,
          marginLeft: side === "right" ? -12 : 160,
        }}
      >
        <div style={{ width: 8, height: 8, borderRadius: "50%", backgroundColor: "#2A2D32", border: `1px solid ${color}30` }} />
        <div style={{ width: 5, height: 5, borderRadius: "50%", backgroundColor: "#2A2D32", border: `1px solid ${color}30`, marginTop: 4 }} />
      </div>
    </div>
  );
};

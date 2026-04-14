import React from "react";
import { useCurrentFrame, interpolate } from "remotion";

interface TypingDotsProps {
  color: string;
  size?: number;
}

// Slack-style typing indicator — three bouncing dots
export const TypingDots: React.FC<TypingDotsProps> = ({ color, size = 6 }) => {
  const frame = useCurrentFrame();

  const dot = (delay: number) => {
    const phase = (frame - delay) * 0.3;
    const bounce = Math.max(0, Math.sin(phase)) * 4;
    return bounce;
  };

  return (
    <span style={{ display: "inline-flex", gap: 4, alignItems: "center", marginLeft: 2 }}>
      {[0, 4, 8].map((d, i) => (
        <span
          key={i}
          style={{
            width: size,
            height: size,
            borderRadius: "50%",
            backgroundColor: color,
            display: "inline-block",
            transform: `translateY(${-dot(d)}px)`,
          }}
        />
      ))}
    </span>
  );
};

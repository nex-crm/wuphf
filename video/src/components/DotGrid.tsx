import React from "react";
import { useCurrentFrame } from "remotion";

interface DotGridProps {
  color?: string;
  opacity?: number;
  size?: number;
  spacing?: number;
  drift?: boolean;
}

// Subtle dot-grid background — adds depth without being busy
export const DotGrid: React.FC<DotGridProps> = ({
  color = "#FFFFFF",
  opacity = 0.06,
  size = 1.5,
  spacing = 32,
  drift = true,
}) => {
  const frame = useCurrentFrame();
  const offset = drift ? (frame * 0.2) % spacing : 0;

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        backgroundImage: `radial-gradient(circle, ${color} ${size}px, transparent ${size}px)`,
        backgroundSize: `${spacing}px ${spacing}px`,
        backgroundPosition: `${offset}px ${offset}px`,
        opacity,
        pointerEvents: "none",
      }}
    />
  );
};

// Radial glow — adds atmosphere, not decoration
export const RadialGlow: React.FC<{ color: string; x?: string; y?: string; size?: number; opacity?: number }> = ({
  color,
  x = "50%",
  y = "50%",
  size = 800,
  opacity = 0.15,
}) => {
  return (
    <div
      style={{
        position: "absolute",
        left: `calc(${x} - ${size / 2}px)`,
        top: `calc(${y} - ${size / 2}px)`,
        width: size,
        height: size,
        background: `radial-gradient(circle, ${color} 0%, transparent 60%)`,
        opacity,
        pointerEvents: "none",
        filter: "blur(40px)",
      }}
    />
  );
};

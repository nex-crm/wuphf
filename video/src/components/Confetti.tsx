import React from "react";
import { useCurrentFrame, interpolate } from "remotion";

interface ConfettiProps {
  startFrame: number;
  colors?: string[];
  count?: number;
  originX?: number;
  originY?: number;
}

// Burst of confetti particles
export const Confetti: React.FC<ConfettiProps> = ({
  startFrame,
  colors = ["#22C55E", "#EAB308", "#3B82F6", "#F97316", "#EC4899"],
  count = 40,
  originX = 0.5,
  originY = 0.5,
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - startFrame;

  if (elapsed < 0 || elapsed > 80) return null;

  // Generate stable particle positions based on index
  const particles = Array.from({ length: count }, (_, i) => {
    const angle = (i / count) * Math.PI * 2 + (i * 0.137);
    const velocity = 200 + (i % 7) * 40;
    const gravity = 0.15;
    const t = elapsed;

    const dx = Math.cos(angle) * velocity * (t / 30);
    const dy = Math.sin(angle) * velocity * (t / 30) + gravity * t * t;

    const rotation = i * 23 + t * (i % 5) * 10;
    const scale = interpolate(t, [0, 10, 70, 80], [0, 1, 1, 0], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    });
    const opacity = interpolate(t, [0, 5, 60, 80], [0, 1, 1, 0], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    });

    return {
      x: dx,
      y: dy,
      rotation,
      scale,
      opacity,
      color: colors[i % colors.length],
      shape: i % 3, // 0 = square, 1 = circle, 2 = rect
    };
  });

  return (
    <div style={{ position: "absolute", inset: 0, pointerEvents: "none" }}>
      {particles.map((p, i) => {
        const isCircle = p.shape === 1;
        const isRect = p.shape === 2;
        const size = isRect ? 14 : 10;
        return (
          <div
            key={i}
            style={{
              position: "absolute",
              left: `${originX * 100}%`,
              top: `${originY * 100}%`,
              width: size,
              height: isRect ? 6 : size,
              backgroundColor: p.color,
              borderRadius: isCircle ? "50%" : 2,
              transform: `translate(${p.x}px, ${p.y}px) rotate(${p.rotation}deg) scale(${p.scale})`,
              opacity: p.opacity,
              willChange: "transform",
            }}
          />
        );
      })}
    </div>
  );
};

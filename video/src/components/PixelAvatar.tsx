import React from "react";
import { resolveSprite } from "./spriteData";

interface PixelAvatarProps {
  slug: string;   // agent slug: "ceo", "eng", "gtm", "pm", "fe", etc.
  color: string;  // hex accent color, e.g. "#E8A838"
  size: number;   // rendered pixel width (height auto-scales 1:1)
}

function hexToRgb(hex: string) {
  return {
    r: parseInt(hex.slice(1, 3), 16),
    g: parseInt(hex.slice(3, 5), 16),
    b: parseInt(hex.slice(5, 7), 16),
  };
}

function buildPalette(accentHex: string): Record<number, string> {
  const { r, g, b } = hexToRgb(accentHex);
  const hr = Math.max(0, r - 60);
  const hg = Math.max(0, g - 60);
  const hb = Math.max(0, b - 60);
  return {
    1: "#242020",                              // outline
    2: "#EBD7BE",                              // skin
    3: accentHex,                              // accent (shirt / body)
    4: `rgb(${hr},${hg},${hb})`,              // hair (darkened accent)
    5: "#B4AA9B",                              // prop / accessory
    6: "#FFFFFF",                              // highlight
  };
}

export const PixelAvatar: React.FC<PixelAvatarProps> = ({ slug, color, size }) => {
  const sprite = resolveSprite(slug);
  const palette = buildPalette(color);
  const rows = sprite.length;
  const cols = sprite[0]?.length ?? 14;

  return (
    <svg
      width={size}
      height={Math.round(size * rows / cols)}
      viewBox={`0 0 ${cols} ${rows}`}
      style={{ imageRendering: "pixelated", display: "block", flexShrink: 0 }}
    >
      {sprite.flatMap((row, r) =>
        row.map((px, c) =>
          px > 0 ? (
            <rect
              key={`${r}-${c}`}
              x={c}
              y={r}
              width={1}
              height={1}
              fill={palette[px] ?? "#888888"}
            />
          ) : null
        )
      )}
    </svg>
  );
};

import React from "react";
import { resolveSpriteData } from "./avatarSprites.generated";

interface PixelAvatarProps {
  slug: string;
  color: string;  // Kept for API compatibility with existing scene code.
  size: number;
}

export const PixelAvatar: React.FC<PixelAvatarProps> = ({ slug, color: _color, size }) => {
  const sprite = resolveSpriteData(slug);
  const rows = sprite.portrait.length;
  const cols = sprite.portrait[0]?.length ?? 16;

  return (
    <svg
      width={size}
      height={Math.round((size * rows) / cols)}
      viewBox={`0 0 ${cols} ${rows}`}
      style={{ imageRendering: "pixelated", display: "block", flexShrink: 0 }}
    >
      {sprite.portrait.flatMap((row, r) =>
        row.map((px, c) =>
          px > 0 ? (
            <rect
              key={`${r}-${c}`}
              x={c}
              y={r}
              width={1}
              height={1}
              fill={sprite.palette[px - 1] ?? "#888888"}
            />
          ) : null
        )
      )}
    </svg>
  );
};

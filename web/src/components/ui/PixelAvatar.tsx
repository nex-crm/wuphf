import { useEffect, useRef } from "react";

import { drawPixelAvatar } from "../../lib/pixelAvatar";

interface PixelAvatarProps {
  slug: string;
  size: number;
  className?: string;
}

/**
 * Renders a pixel-art agent portrait on a <canvas>.
 * Pass a className like `pixel-avatar-sidebar` or `pixel-avatar-panel`
 * to apply theme-level sizing/treatment around the canvas.
 */
export function PixelAvatar({ slug, size, className }: PixelAvatarProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    drawPixelAvatar(canvas, slug, size);
  }, [slug, size]);

  const composedClassName = ["pixel-avatar", className]
    .filter(Boolean)
    .join(" ");

  return <canvas ref={canvasRef} className={composedClassName} />;
}

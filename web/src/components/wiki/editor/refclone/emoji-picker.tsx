import { useEffect, useRef } from "react";
import data from "@emoji-mart/data";
import { Picker } from "emoji-mart";

interface Props {
  anchor: { top: number; left?: number; right?: number };
  onSelect: (native: string) => void;
  onClose: () => void;
}

/**
 * Resolve the active light/dark theme for emoji-mart.
 *
 * The reference app reads this from a `useTheme()` provider that WUPHF does
 * not have. WUPHF themes drive a `data-theme` attribute + a `.dark` class on
 * the document element (see RootRoute), so we detect dark mode from the live
 * DOM instead of dragging in a theme context the host doesn't expose.
 */
function resolveDarkMode(): boolean {
  if (typeof document === "undefined") return false;
  const root = document.documentElement;
  if (root.classList.contains("dark")) return true;
  const theme = root.getAttribute("data-theme")?.toLowerCase() ?? "";
  if (theme.includes("dark") || theme.includes("noir")) return true;
  if (typeof window !== "undefined" && window.matchMedia) {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  return false;
}

export function EmojiPicker({ anchor, onSelect, onClose }: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    // emoji-mart Picker renders into the element when `new Picker({...})`
    const picker = new Picker({
      data,
      theme: resolveDarkMode() ? "dark" : "light",
      autoFocus: true,
      previewPosition: "none",
      skinTonePosition: "search",
      maxFrequentRows: 2,
      perLine: 8,
      onEmojiSelect: (emoji: { native: string }) => onSelect(emoji.native),
    });
    container.appendChild(picker as unknown as Node);
    return () => {
      if ((picker as unknown as Node).parentNode) {
        (picker as unknown as Node).parentNode!.removeChild(
          picker as unknown as Node,
        );
      }
    };
  }, [onSelect]);

  useEffect(() => {
    const handle = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        onClose();
      }
    };
    // Defer to avoid catching the opening click
    const t = window.setTimeout(
      () => window.addEventListener("mousedown", handle),
      10,
    );
    return () => {
      window.clearTimeout(t);
      window.removeEventListener("mousedown", handle);
    };
  }, [onClose]);

  return (
    <div
      ref={containerRef}
      className="absolute z-50 shadow-xl rounded-lg overflow-hidden"
      style={{ top: anchor.top, left: anchor.left, right: anchor.right }}
      onMouseDown={(e) => e.stopPropagation()}
    />
  );
}

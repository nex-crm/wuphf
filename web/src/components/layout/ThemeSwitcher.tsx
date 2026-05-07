import { useEffect, useRef, useState } from "react";

import type { Theme } from "../../stores/app";
import { useAppStore } from "../../stores/app";

interface ThemeOption {
  id: Theme;
  name: string;
  desc: string;
  swatch: { primary: string; accent: string; surface: string };
}

const THEME_OPTIONS: ThemeOption[] = [
  {
    id: "nex",
    name: "Nex Light",
    desc: "Clean light. Cyan accents.",
    swatch: { primary: "#612a92", accent: "#9f4dbf", surface: "#ffffff" },
  },
  {
    id: "nex-dark",
    name: "Nex Dark",
    desc: "Low-glare dark.",
    swatch: { primary: "#0f0f12", accent: "#9f4dbf", surface: "#1a1a1f" },
  },
  {
    id: "noir-gold",
    name: "Noir Gold",
    desc: "Black, gold leaf.",
    swatch: { primary: "#0a0a0a", accent: "#d4af37", surface: "#161616" },
  },
];

interface ThemeSwitcherProps {
  className?: string;
}

export function ThemeSwitcher({ className }: ThemeSwitcherProps) {
  const theme = useAppStore((s) => s.theme);
  const setTheme = useAppStore((s) => s.setTheme);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(e: PointerEvent) {
      const node = wrapRef.current;
      if (node && e.target instanceof Node && !node.contains(e.target)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function pick(t: Theme) {
    setTheme(t);
    setOpen(false);
  }

  const current = THEME_OPTIONS.find((o) => o.id === theme) ?? THEME_OPTIONS[0];

  return (
    <div
      ref={wrapRef}
      className={`theme-switcher${className ? ` ${className}` : ""}`}
    >
      <button
        type="button"
        className="sidebar-btn theme-switcher-trigger"
        title={`Theme: ${current.name}`}
        aria-label={`Theme: ${current.name}. Open theme switcher.`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span
          className="theme-switcher-swatch"
          aria-hidden="true"
          style={{
            background: `linear-gradient(135deg, ${current.swatch.primary} 0%, ${current.swatch.primary} 50%, ${current.swatch.accent} 50%, ${current.swatch.accent} 100%)`,
          }}
        />
        <svg
          aria-hidden="true"
          focusable="false"
          width="10"
          height="10"
          viewBox="0 0 10 10"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M2 4l3 3 3-3" />
        </svg>
      </button>

      {open ? (
        <div
          className="theme-switcher-menu"
          role="menu"
          aria-label="Theme switcher"
        >
          <div className="theme-switcher-title">Theme</div>
          <div className="theme-switcher-options">
            {THEME_OPTIONS.map((opt) => {
              const isActive = opt.id === theme;
              return (
                <button
                  key={opt.id}
                  type="button"
                  role="menuitemradio"
                  aria-checked={isActive}
                  className={`theme-switcher-option${isActive ? " is-active" : ""}`}
                  onClick={() => pick(opt.id)}
                >
                  <span
                    className="theme-switcher-option-swatch"
                    aria-hidden="true"
                    style={{
                      background: `linear-gradient(135deg, ${opt.swatch.primary} 0%, ${opt.swatch.primary} 50%, ${opt.swatch.accent} 50%, ${opt.swatch.accent} 100%)`,
                      borderColor: opt.swatch.surface,
                    }}
                  />
                  <span className="theme-switcher-option-text">
                    <span className="theme-switcher-option-name">
                      {opt.name}
                    </span>
                    <span className="theme-switcher-option-desc">
                      {opt.desc}
                    </span>
                  </span>
                  {isActive ? (
                    <span
                      className="theme-switcher-option-check"
                      aria-hidden="true"
                    >
                      {"✓"}
                    </span>
                  ) : null}
                </button>
              );
            })}
          </div>
        </div>
      ) : null}
    </div>
  );
}

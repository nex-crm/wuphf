import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from "react";

import { type Theme, THEMES } from "../../lib/themes";
import { useAppStore } from "../../stores/app";

interface ThemeSwitcherProps {
  className?: string;
}

export function ThemeSwitcher({ className }: ThemeSwitcherProps) {
  const theme = useAppStore((s) => s.theme);
  const setTheme = useAppStore((s) => s.setTheme);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(e: PointerEvent) {
      const node = wrapRef.current;
      if (node && e.target instanceof Node && !node.contains(e.target)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // APG menu pattern: when the menu opens, focus moves to the active
  // menuitemradio (or the first item) so arrow-key navigation has a
  // starting point.
  useEffect(() => {
    if (!open) return;
    const menu = menuRef.current;
    if (!menu) return;
    const items = menu.querySelectorAll<HTMLButtonElement>(
      '[role="menuitemradio"]',
    );
    const active = menu.querySelector<HTMLButtonElement>(
      '[role="menuitemradio"][aria-checked="true"]',
    );
    (active ?? items[0])?.focus();
  }, [open]);

  function pick(t: Theme) {
    setTheme(t);
    setOpen(false);
    triggerRef.current?.focus();
  }

  function onMenuKeyDown(e: ReactKeyboardEvent<HTMLDivElement>) {
    const menu = menuRef.current;
    if (!menu) return;
    const items = Array.from(
      menu.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]'),
    );
    if (items.length === 0) return;
    const idx = items.indexOf(document.activeElement as HTMLButtonElement);

    if (e.key === "ArrowDown") {
      e.preventDefault();
      const next = idx < 0 ? 0 : (idx + 1) % items.length;
      items[next]?.focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      const prev = idx <= 0 ? items.length - 1 : idx - 1;
      items[prev]?.focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      items[0]?.focus();
    } else if (e.key === "End") {
      e.preventDefault();
      items[items.length - 1]?.focus();
    } else if (e.key === "Tab") {
      // Per APG menu-button pattern, Tab closes the menu and lets the
      // browser advance focus naturally to the next document element.
      setOpen(false);
    }
  }

  const current = THEMES.find((o) => o.id === theme) ?? THEMES[0];

  return (
    <div
      ref={wrapRef}
      className={`theme-switcher${className ? ` ${className}` : ""}`}
    >
      <button
        ref={triggerRef}
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
          ref={menuRef}
          className="theme-switcher-menu"
          role="menu"
          aria-label="Theme switcher"
          onKeyDown={onMenuKeyDown}
        >
          <div className="theme-switcher-title">Theme</div>
          <div className="theme-switcher-options">
            {THEMES.map((opt) => {
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

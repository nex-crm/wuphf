import type { ReactNode } from "react";
import { useEffect, useRef } from "react";

interface SidePanelProps {
  /** Whether the panel is currently open. */
  open: boolean;
  /** Called when the user dismisses (Esc, backdrop, or X). */
  onClose: () => void;
  /** Heading rendered in the panel header. */
  title: string;
  /** Optional subline shown under the title (e.g. a kebab-case skill name). */
  subtitle?: string;
  /** Body content. */
  children: ReactNode;
  /** ARIA label override for the close button. Defaults to "Close panel". */
  closeLabel?: string;
}

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "textarea:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

/**
 * Generic side panel primitive. 480px desktop / 60vw tablet / fullscreen
 * mobile. Esc/backdrop/X dismiss, focus trap, body scroll-lock. Re-usable
 * for any "view full" surface (Skills, Threads, Activity, …).
 */
export function SidePanel({
  open,
  onClose,
  title,
  subtitle,
  children,
  closeLabel = "Close panel",
}: SidePanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  // Esc to close.
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose]);

  // Body scroll-lock while open.
  useEffect(() => {
    if (!open) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [open]);

  // Save and restore focus + trap focus inside the panel.
  useEffect(() => {
    if (!open) return;
    previousFocusRef.current = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel) {
      const focusables =
        panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
      const [first] = focusables;
      if (first) {
        first.focus();
      } else {
        panel.focus();
      }
    }

    const trap = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || !panel) return;
      const focusables = Array.from(
        panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      );
      if (focusables.length === 0) {
        e.preventDefault();
        return;
      }
      const [first] = focusables;
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", trap);
    return () => {
      document.removeEventListener("keydown", trap);
      previousFocusRef.current?.focus();
    };
  }, [open]);

  if (!open) return null;

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 250,
        display: "flex",
        justifyContent: "flex-end",
      }}
    >
      <div
        onClick={onClose}
        aria-hidden="true"
        style={{
          position: "absolute",
          inset: 0,
          background: "rgba(20, 22, 24, 0.36)",
        }}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        style={{
          position: "relative",
          width: "min(480px, 100vw)",
          maxWidth: "100vw",
          height: "100vh",
          background: "var(--bg-card, #fff)",
          borderLeft: "1px solid var(--border)",
          boxShadow: "-12px 0 32px rgba(0,0,0,0.10)",
          display: "flex",
          flexDirection: "column",
          outline: "none",
        }}
        className="side-panel"
      >
        <header
          style={{
            display: "flex",
            alignItems: "flex-start",
            justifyContent: "space-between",
            gap: 12,
            padding: "16px 20px",
            borderBottom: "1px solid var(--border)",
          }}
        >
          <div style={{ minWidth: 0, flex: 1 }}>
            <h2
              style={{
                fontSize: 15,
                fontWeight: 600,
                color: "var(--text)",
                margin: 0,
                lineHeight: 1.3,
                wordBreak: "break-word",
              }}
            >
              {title}
            </h2>
            {subtitle ? (
              <div
                style={{
                  fontSize: 12,
                  color: "var(--text-tertiary)",
                  fontFamily: "var(--font-mono)",
                  marginTop: 2,
                }}
              >
                {subtitle}
              </div>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={closeLabel}
            style={{
              background: "transparent",
              border: "none",
              padding: 4,
              cursor: "pointer",
              color: "var(--text-tertiary)",
              fontSize: 20,
              lineHeight: 1,
              minWidth: 32,
              minHeight: 32,
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              borderRadius: 4,
            }}
          >
            ×
          </button>
        </header>
        <div
          style={{
            flex: 1,
            overflow: "auto",
            padding: "16px 20px",
          }}
        >
          {children}
        </div>
      </div>
    </div>
  );
}

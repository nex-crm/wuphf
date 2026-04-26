import { type CSSProperties, type ReactNode, useState } from "react";
import { PlaySolid } from "iconoir-react";

import { WipeModal, type WipeSeverity } from "./WipeModal";

export type InlineCommandTone = "warning" | "neutral";

export interface DestructiveConfirm {
  title: string;
  intro: ReactNode;
  confirmLabel: string;
  severity?: WipeSeverity;
}

export interface InlineCommandProps {
  command: string;
  onRun: () => void | Promise<void>;
  destructive?: DestructiveConfirm;
  tone?: InlineCommandTone;
  ariaLabel?: string;
  style?: CSSProperties;
}

const PALETTES: Record<
  InlineCommandTone,
  { bg: string; bgHover: string; fg: string; ring: string }
> = {
  warning: {
    bg: "var(--warning-200)",
    bgHover: "var(--warning-300, #f3cf90)",
    fg: "var(--warning-500)",
    ring: "rgba(153, 66, 0, 0.35)",
  },
  neutral: {
    bg: "var(--bg-warm)",
    bgHover: "var(--border, #d8d4cc)",
    fg: "var(--text)",
    ring: "rgba(0, 0, 0, 0.18)",
  },
};

// InlineCommand renders a clickable inline chip styled like a code snippet.
// For destructive actions, click opens a WipeModal that gates execution behind
// the confirm phrase; otherwise click runs onRun immediately.
export function InlineCommand({
  command,
  onRun,
  destructive,
  tone = "warning",
  ariaLabel,
  style,
}: InlineCommandProps) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hover, setHover] = useState(false);
  const palette = PALETTES[tone];

  const run = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await onRun();
      setOpen(false);
    } finally {
      setBusy(false);
    }
  };

  const handleClick = () => {
    if (busy) return;
    if (destructive) {
      setOpen(true);
      return;
    }
    void run();
  };

  return (
    <>
      <button
        type="button"
        onClick={handleClick}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
        disabled={busy}
        title={ariaLabel || `Run ${command}`}
        aria-label={ariaLabel || `Run ${command}`}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 5,
          fontFamily: "var(--font-mono)",
          fontSize: "inherit",
          padding: "1px 8px",
          background: hover && !busy ? palette.bgHover : palette.bg,
          color: palette.fg,
          border: "none",
          borderRadius: 4,
          cursor: busy ? "progress" : "pointer",
          boxShadow: hover && !busy ? `0 0 0 2px ${palette.ring}` : "none",
          transition: "background 0.12s, box-shadow 0.12s",
          verticalAlign: "baseline",
          lineHeight: 1.45,
          ...style,
        }}
      >
        <PlaySolid
          width={9}
          height={9}
          style={{ flexShrink: 0, opacity: 0.85 }}
        />
        <span>{command}</span>
      </button>
      {open && destructive && (
        <WipeModal
          title={destructive.title}
          severity={destructive.severity || "critical"}
          intro={destructive.intro}
          confirmLabel={destructive.confirmLabel}
          busy={busy}
          onConfirm={() => void run()}
          onCancel={() => {
            if (!busy) setOpen(false);
          }}
        />
      )}
    </>
  );
}

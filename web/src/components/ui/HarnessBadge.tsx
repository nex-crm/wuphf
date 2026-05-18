import type { ReactNode } from "react";

import type { HarnessKind } from "../../lib/harness";
import { harnessLabel } from "../../lib/harness";

interface HarnessBadgeProps {
  kind: HarnessKind;
  size?: number;
  className?: string;
}

// Per-kind rendering record. Each entry describes the badge's background
// color, a viewBox for the embedded SVG (so each brand's native coordinate
// system survives), and an SVG body. We use the projects' OWN SVG sources
// verbatim where they exist:
//   - openclaw — http://127.0.0.1:18789/favicon.svg (gradient lobster)
//   - hermes-agent — ~/.hermes/hermes-agent/acp_registry/icon.svg (caduceus)
// The other three (claude-code, codex, opencode) have no permissively-shippable
// SVG inside this repo; they keep minimal monogram-style glyphs in brand colors.
type GlyphDef = {
  bg: string;
  viewBox: string;
  body: ReactNode;
};

const GLYPHS: Record<HarnessKind, GlyphDef> = {
  "claude-code": {
    bg: "#D97757",
    viewBox: "0 0 24 24",
    body: (
      <path
        d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6L5.6 18.4"
        stroke="#FFFFFF"
        strokeWidth="2.4"
        strokeLinecap="round"
      />
    ),
  },
  codex: {
    bg: "#10A37F",
    viewBox: "0 0 24 24",
    body: (
      <path
        d="M6 8l5 4-5 4M13 16h6"
        stroke="#FFFFFF"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    ),
  },
  opencode: {
    bg: "#2563EB",
    viewBox: "0 0 24 24",
    body: (
      <path
        d="M9 8l-4 4 4 4M15 8l4 4-4 4"
        stroke="#FFFFFF"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    ),
  },
  // OpenClaw lobster — exact copy of the project's favicon.svg served from
  // their gateway at /favicon.svg. White badge background lets the native
  // red gradient + dark eyes read clearly even at 12px.
  openclaw: {
    bg: "#FFFFFF",
    viewBox: "0 0 120 120",
    body: (
      <>
        <defs>
          <linearGradient
            id="harness-openclaw-gradient"
            x1="0%"
            y1="0%"
            x2="100%"
            y2="100%"
          >
            <stop offset="0%" stopColor="#ff4d4d" />
            <stop offset="100%" stopColor="#991b1b" />
          </linearGradient>
        </defs>
        {/* body */}
        <path
          d="M60 10 C30 10 15 35 15 55 C15 75 30 95 45 100 L45 110 L55 110 L55 100 C55 100 60 102 65 100 L65 110 L75 110 L75 100 C90 95 105 75 105 55 C105 35 90 10 60 10Z"
          fill="url(#harness-openclaw-gradient)"
        />
        {/* left claw */}
        <path
          d="M20 45 C5 40 0 50 5 60 C10 70 20 65 25 55 C28 48 25 45 20 45Z"
          fill="url(#harness-openclaw-gradient)"
        />
        {/* right claw */}
        <path
          d="M100 45 C115 40 120 50 115 60 C110 70 100 65 95 55 C92 48 95 45 100 45Z"
          fill="url(#harness-openclaw-gradient)"
        />
        {/* antennae */}
        <path
          d="M45 15 Q35 5 30 8"
          stroke="#ff4d4d"
          strokeWidth="3"
          strokeLinecap="round"
          fill="none"
        />
        <path
          d="M75 15 Q85 5 90 8"
          stroke="#ff4d4d"
          strokeWidth="3"
          strokeLinecap="round"
          fill="none"
        />
        {/* eyes */}
        <circle cx="45" cy="35" r="6" fill="#050810" />
        <circle cx="75" cy="35" r="6" fill="#050810" />
        <circle cx="46" cy="34" r="2.5" fill="#00e5cc" />
        <circle cx="76" cy="34" r="2.5" fill="#00e5cc" />
      </>
    ),
  },
  // Hermes caduceus — exact copy of ~/.hermes/hermes-agent/acp_registry/icon.svg.
  // Original uses `currentColor`, so we set `color` on the wrapper and the paths
  // inherit it. White-on-indigo for visibility on the indigo Hermes badge.
  "hermes-agent": {
    bg: "#3730A3",
    viewBox: "0 0 16 16",
    body: (
      <>
        <path
          d="M8 1.5v13"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
        />
        <path
          d="M8 3.25c-2.35-1.4-4.7-.95-6.25.35 1.85-.2 3.8.2 5.55 1.55"
          stroke="currentColor"
          strokeWidth="1.1"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M8 3.25c2.35-1.4 4.7-.95 6.25.35-1.85-.2-3.8.2-5.55 1.55"
          stroke="currentColor"
          strokeWidth="1.1"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M8 13.25c-2.3-1-3.05-2.65-1.35-4.15-2 .8-2.35 2.95-.35 4"
          stroke="currentColor"
          strokeWidth="1.1"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M8 13.25c2.3-1 3.05-2.65 1.35-4.15 2 .8 2.35 2.95.35 4"
          stroke="currentColor"
          strokeWidth="1.1"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx="8" cy="1.8" r="1.1" fill="currentColor" />
      </>
    ),
  },
};

export function HarnessBadge({
  kind,
  size = 12,
  className,
}: HarnessBadgeProps) {
  const glyph = GLYPHS[kind];
  const classes = ["harness-badge", className].filter(Boolean).join(" ");
  return (
    <span
      className={classes}
      role="img"
      aria-label={`${harnessLabel(kind)} harness`}
      title={harnessLabel(kind)}
      style={{
        width: size,
        height: size,
        background: glyph.bg,
        color: "#FFFFFF",
      }}
    >
      <svg
        viewBox={glyph.viewBox}
        width={size}
        height={size}
        fill="none"
        aria-hidden="true"
      >
        {glyph.body}
      </svg>
    </span>
  );
}

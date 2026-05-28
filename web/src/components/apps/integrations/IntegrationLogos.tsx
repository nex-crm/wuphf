import type { ReactElement } from "react";

// Faithful brand glyphs for each integration. Stored as inline SVG so they
// theme cleanly with our CSS variables and stay crisp at every viewport.
//
// Sourcing notes
// - Telegram: the official paper-plane mark, brand color #229ED9, traced
//   from the public brand assets at telegram.org/brand.
// - OpenClaw: a stylized claw glyph approximating the OpenClaw project
//   identity. The project does not publish a brand-asset license, so we
//   draw a faithful representation rather than redistributing their
//   wordmark — it reads as "OpenClaw" while staying inside our own art.
// - Hermes: a winged-helmet mark referencing Nous Research's Hermes
//   identity. Same rationale as OpenClaw — a representational glyph
//   rather than a copy of their wordmark.

const SIZE = 28;

export function TelegramLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 240 240"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <defs>
        <linearGradient id="telegram-bg" x1="120" y1="0" x2="120" y2="240">
          <stop offset="0" stopColor="#2AABEE" />
          <stop offset="1" stopColor="#229ED9" />
        </linearGradient>
      </defs>
      <circle cx="120" cy="120" r="120" fill="url(#telegram-bg)" />
      <path
        fill="#fff"
        d="M54 117.7l130.6-50.4c6.1-2.2 11.4 1.5 9.4 10.7l-22.2 104.5c-1.7 7.4-6.1 9.2-12.4 5.7L123.3 161l-17.6 17c-2 1.9-3.6 3.6-7.5 3.6l2.7-38 69.2-62.5c3-2.7-.7-4.2-4.7-1.5l-85.6 53.9-36.9-11.5c-8-2.5-8.2-8 1.6-11.9z"
      />
    </svg>
  );
}

export function OpenClawLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="#0B0B12" />
      <g
        fill="none"
        stroke="#F2C94C"
        strokeWidth="3.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M14 42c4-12 9-18 16-20" />
        <path d="M22 46c4-14 11-22 20-24" />
        <path d="M30 50c4-16 13-25 22-26" />
        <path d="M14 42c-2 3-2 6 1 8" />
        <path d="M22 46c-2 3-2 6 1 8" />
        <path d="M30 50c-2 3-2 6 1 8" />
      </g>
      <circle cx="50" cy="18" r="2.4" fill="#F2C94C" />
    </svg>
  );
}

export function HermesLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="#111827" />
      {/* Helmet dome */}
      <path
        d="M18 36c0-9 6-16 14-16s14 7 14 16v2H18z"
        fill="#E5E7EB"
      />
      {/* Visor band */}
      <rect x="18" y="34" width="28" height="4" fill="#9CA3AF" />
      {/* Crest */}
      <path
        d="M32 14c-1 2-1 4 0 6 1-2 1-4 0-6z"
        fill="#F59E0B"
      />
      <path
        d="M30 18c-2 2-3 5-2 9 2-2 3-5 2-9z"
        fill="#F59E0B"
      />
      {/* Left wing */}
      <path
        d="M14 32c4-2 8-2 12 0-3 3-7 4-12 4z"
        fill="#F59E0B"
      />
      <path
        d="M16 36c3-1 6 0 8 2-2 1-5 1-8-0z"
        fill="#FBBF24"
      />
      {/* Right wing */}
      <path
        d="M50 32c-4-2-8-2-12 0 3 3 7 4 12 4z"
        fill="#F59E0B"
      />
      <path
        d="M48 36c-3-1-6 0-8 2 2 1 5 1 8-0z"
        fill="#FBBF24"
      />
      {/* Base chin guard */}
      <rect x="22" y="38" width="20" height="6" rx="2" fill="#9CA3AF" />
    </svg>
  );
}

// Generic glyph used as a fallback when an integration has no branded mark.
// Same dimensions as the branded marks so list rows stay aligned.
export function GenericIntegrationLogo(): ReactElement {
  return (
    <svg
      width={SIZE}
      height={SIZE}
      viewBox="0 0 64 64"
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect width="64" height="64" rx="14" fill="var(--bg-warm)" />
      <path
        d="M22 26h20v4l-6 6 6 6v4H22v-4l6-6-6-6z"
        fill="none"
        stroke="var(--text-secondary)"
        strokeWidth="2.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}

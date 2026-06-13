// Theme-aware line-art illustrations for the Slack-onboarding wizard. Each
// draws on the live design tokens (--wk-accent for the hero stroke/fill,
// --wk-text-muted for line work, --wk-border for structure) so they re-skin
// cleanly across Nex Light, Nex Dark, and Noir Gold without per-theme assets.
// All are decorative (aria-hidden); the step copy carries the meaning.

import type { ReactElement } from "react";

interface IlloProps {
  className?: string;
}

const ACCENT = "var(--wk-accent)";
const MUTED = "var(--wk-text-muted)";
const LINE = "var(--wk-border)";
const PAPER = "var(--wk-paper)";

function frame(children: ReactElement, className?: string): ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 240 168"
      fill="none"
      aria-hidden="true"
      role="presentation"
    >
      {children}
    </svg>
  );
}

/** Intro: the office and Slack, joined by a live bridge. */
export function SlackBridgeIllo({ className }: IlloProps): ReactElement {
  return frame(
    <>
      <g className="sc-illo-float">
        {/* office card */}
        <rect
          x="20"
          y="44"
          width="78"
          height="80"
          rx="10"
          fill={PAPER}
          stroke={LINE}
          strokeWidth="2"
        />
        <circle cx="40" cy="66" r="6" fill={ACCENT} />
        <rect
          x="54"
          y="62"
          width="34"
          height="6"
          rx="3"
          fill={MUTED}
          opacity="0.6"
        />
        <rect
          x="32"
          y="84"
          width="54"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
        <rect
          x="32"
          y="96"
          width="42"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
        <rect
          x="32"
          y="108"
          width="48"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
      </g>
      <g className="sc-illo-float sc-illo-float-delay">
        {/* slack card */}
        <rect
          x="142"
          y="44"
          width="78"
          height="80"
          rx="10"
          fill={PAPER}
          stroke={LINE}
          strokeWidth="2"
        />
        <text
          x="156"
          y="72"
          fontSize="20"
          fontWeight="700"
          fill={ACCENT}
          fontFamily="var(--wk-mono)"
        >
          #
        </text>
        <rect
          x="172"
          y="62"
          width="34"
          height="6"
          rx="3"
          fill={MUTED}
          opacity="0.6"
        />
        <rect
          x="154"
          y="84"
          width="54"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
        <rect
          x="154"
          y="96"
          width="42"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
        <rect
          x="154"
          y="108"
          width="48"
          height="5"
          rx="2.5"
          fill={MUTED}
          opacity="0.35"
        />
      </g>
      {/* bridge */}
      <path
        d="M98 84 H142"
        stroke={ACCENT}
        strokeWidth="3"
        strokeLinecap="round"
        strokeDasharray="2 9"
        className="sc-illo-dash"
      />
      <circle cx="120" cy="84" r="13" fill={ACCENT} />
      <path
        d="M114 84 l4 4 l8 -9"
        stroke={PAPER}
        strokeWidth="2.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </>,
    className,
  );
}

/** Create app: a manifest document that pre-fills the app. */
export function SlackManifestIllo({ className }: IlloProps): ReactElement {
  return frame(
    <>
      <rect
        x="64"
        y="30"
        width="112"
        height="108"
        rx="10"
        fill={PAPER}
        stroke={LINE}
        strokeWidth="2"
      />
      <rect
        x="64"
        y="30"
        width="112"
        height="26"
        rx="10"
        fill={ACCENT}
        opacity="0.12"
      />
      <circle cx="80" cy="43" r="3.5" fill={ACCENT} />
      <rect
        x="90"
        y="40"
        width="58"
        height="6"
        rx="3"
        fill={ACCENT}
        opacity="0.7"
      />
      {[68, 82, 96, 110].map((y, i) => (
        <g key={y}>
          <rect
            x="78"
            y={y}
            width="10"
            height="5"
            rx="2.5"
            fill={ACCENT}
            opacity="0.5"
          />
          <rect
            x="94"
            y={y}
            width={[64, 50, 58, 40][i]}
            height="5"
            rx="2.5"
            fill={MUTED}
            opacity="0.4"
          />
        </g>
      ))}
      {/* paste spark */}
      <g className="sc-illo-pop">
        <circle cx="176" cy="44" r="16" fill={ACCENT} />
        <path
          d="M176 36 v16 M168 44 h16"
          stroke={PAPER}
          strokeWidth="2.6"
          strokeLinecap="round"
        />
      </g>
    </>,
    className,
  );
}

/** Tokens: two keys, bot and app. */
export function SlackTokensIllo({ className }: IlloProps): ReactElement {
  const key = (x: number, accent: boolean): ReactElement => (
    <g
      transform={`translate(${x} 0)`}
      className="sc-illo-float sc-illo-float-delay"
    >
      <circle
        cx="0"
        cy="0"
        r="16"
        fill="none"
        stroke={accent ? ACCENT : MUTED}
        strokeWidth="4"
      />
      <circle cx="0" cy="0" r="5" fill={accent ? ACCENT : MUTED} />
      <path
        d="M14 0 H54 M44 0 v12 M54 0 v9"
        stroke={accent ? ACCENT : MUTED}
        strokeWidth="4"
        strokeLinecap="round"
      />
    </g>
  );
  return frame(
    <>
      <g transform="translate(58 58)">{key(0, true)}</g>
      <g transform="translate(58 104)">{key(0, false)}</g>
      <rect
        x="150"
        y="50"
        width="14"
        height="9"
        rx="2"
        fill={ACCENT}
        opacity="0.7"
      />
      <text
        x="168"
        y="59"
        fontSize="11"
        fill={MUTED}
        fontFamily="var(--wk-mono)"
      >
        xoxb-…
      </text>
      <rect
        x="150"
        y="96"
        width="14"
        height="9"
        rx="2"
        fill={MUTED}
        opacity="0.7"
      />
      <text
        x="168"
        y="105"
        fontSize="11"
        fill={MUTED}
        fontFamily="var(--wk-mono)"
      >
        xapp-…
      </text>
    </>,
    className,
  );
}

/** Channel: pick the room the office lives in. */
export function SlackChannelIllo({ className }: IlloProps): ReactElement {
  return frame(
    <>
      <rect
        x="48"
        y="34"
        width="144"
        height="100"
        rx="10"
        fill={PAPER}
        stroke={LINE}
        strokeWidth="2"
      />
      {[
        { y: 52, on: false },
        { y: 74, on: true },
        { y: 96, on: false },
        { y: 118, on: false },
      ].map(({ y, on }) => (
        <g key={y} className={on ? "sc-illo-pop" : undefined}>
          {on && (
            <rect
              x="56"
              y={y - 9}
              width="128"
              height="26"
              rx="7"
              fill={ACCENT}
              opacity="0.14"
            />
          )}
          <text
            x="66"
            y={y + 5}
            fontSize="15"
            fontWeight="700"
            fontFamily="var(--wk-mono)"
            fill={on ? ACCENT : MUTED}
            opacity={on ? 1 : 0.55}
          >
            #
          </text>
          <rect
            x="82"
            y={y - 3}
            width={on ? 96 : [60, 0, 72, 54][[52, 74, 96, 118].indexOf(y)]}
            height="6"
            rx="3"
            fill={on ? ACCENT : MUTED}
            opacity={on ? 0.7 : 0.4}
          />
        </g>
      ))}
    </>,
    className,
  );
}

/** Activating: the bridge coming alive. CSS drives the pulse. */
export function SlackActivatingIllo({ className }: IlloProps): ReactElement {
  return frame(
    <>
      <circle
        cx="120"
        cy="84"
        r="46"
        fill={ACCENT}
        opacity="0.08"
        className="sc-illo-pulse"
      />
      <circle
        cx="120"
        cy="84"
        r="32"
        fill={ACCENT}
        opacity="0.14"
        className="sc-illo-pulse sc-illo-float-delay"
      />
      <circle cx="120" cy="84" r="22" fill={ACCENT} />
      <g className="sc-illo-spin" style={{ transformOrigin: "120px 84px" }}>
        <path
          d="M120 84 m0 -34 a34 34 0 0 1 30 17"
          stroke={ACCENT}
          strokeWidth="3"
          strokeLinecap="round"
          fill="none"
          opacity="0.6"
        />
      </g>
      <path
        d="M111 84 l6 6 l13 -15"
        stroke={PAPER}
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </>,
    className,
  );
}

/** Live: you're in Slack. The payoff. */
export function SlackLiveIllo({ className }: IlloProps): ReactElement {
  return frame(
    <>
      {[
        { x: 40, y: 38, c: ACCENT },
        { x: 196, y: 44, c: MUTED },
        { x: 56, y: 124, c: MUTED },
        { x: 190, y: 120, c: ACCENT },
      ].map((s, i) => (
        <g
          key={i}
          className="sc-illo-pop"
          style={{ animationDelay: `${0.1 + i * 0.08}s` }}
        >
          <path
            d={`M${s.x} ${s.y - 6} v12 M${s.x - 6} ${s.y} h12`}
            stroke={s.c}
            strokeWidth="2.5"
            strokeLinecap="round"
          />
        </g>
      ))}
      <circle cx="120" cy="84" r="40" fill={ACCENT} />
      <circle
        cx="120"
        cy="84"
        r="40"
        fill="none"
        stroke={ACCENT}
        strokeWidth="2"
        opacity="0.4"
        className="sc-illo-pulse"
      />
      <path
        d="M104 85 l11 12 l24 -27"
        stroke={PAPER}
        strokeWidth="4"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="sc-illo-check"
      />
    </>,
    className,
  );
}

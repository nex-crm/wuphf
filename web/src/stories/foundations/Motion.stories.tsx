import type { Meta, StoryObj } from "@storybook/react-vite";

import { Section, TokenRow } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Motion",
  parameters: { layout: "fullscreen" },
};

export default meta;

const DURATIONS = [
  { token: "--duration-fast", value: "0.12s", note: "color/hover swaps" },
  { token: "--duration-base", value: "0.15s", note: "default transition" },
  { token: "--duration-slow", value: "0.2s", note: "modals, drawers" },
];

function Pulse({ duration }: { duration: string }) {
  return (
    <div
      style={{
        width: 48,
        height: 24,
        background: "var(--accent)",
        borderRadius: 999,
        animation: `pulse ${duration} ease-in-out infinite alternate`,
        opacity: 0.7,
      }}
    />
  );
}

export const Durations: StoryObj = {
  render: () => (
    <Section
      title="Duration tokens"
      description="Three durations cover the interaction stack — pair with `ease`, `ease-out`, or `cubic-bezier(0.2, 0, 0, 1)` for sheets."
    >
      <style>{`@keyframes pulse { from { transform: scaleX(1); } to { transform: scaleX(1.6); } }`}</style>
      <div style={{ padding: "0 16px 16px", maxWidth: 920 }}>
        {DURATIONS.map(({ token, value, note }) => (
          <TokenRow
            key={token}
            token={token}
            preview={<Pulse duration={`var(${token})`} />}
            note={`${value} — ${note}`}
          />
        ))}
      </div>
    </Section>
  ),
};

export const PrefersReducedMotion: StoryObj = {
  name: "Reduced motion",
  render: () => (
    <Section
      title="Honoring prefers-reduced-motion"
      description="Critical animations (halos, sliding panels, attention pulses) must short-circuit when the user opts out. Pattern shown below."
    >
      <pre
        style={{
          margin: "0 16px",
          padding: 12,
          background: "var(--bg-warm)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-sm)",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          color: "var(--text)",
          overflowX: "auto",
        }}
      >{`@media (prefers-reduced-motion: reduce) {
  .my-component {
    animation: none;
    transition: none;
  }
}`}</pre>
    </Section>
  ),
};

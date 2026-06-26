import type { Meta, StoryObj } from "@storybook/react-vite";

import { Section, TokenRow } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Typography",
  parameters: { layout: "fullscreen" },
};

export default meta;

const SIZES = [
  { token: "--text-xs", size: 11 },
  { token: "--text-sm", size: 12 },
  { token: "--text-base", size: 13 },
  { token: "--text-md", size: 14 },
  { token: "--text-lg", size: 15 },
  { token: "--text-xl", size: 17 },
  { token: "--text-2xl", size: 20 },
];

const FAMILIES = [
  {
    token: "--font-sans",
    label: "Sans (system)",
    note: "Default UI body — system stack",
  },
  {
    token: "--font-serif",
    label: "Serif",
    note: "Editorial — system serif fallback",
  },
  {
    token: "--font-mono",
    label: "Mono",
    note: "Code, tokens, kbd — SFMono / Menlo",
  },
];

const DISPLAY_FONTS = [
  {
    family: "Newsreader",
    sample: "The shared brain remembers everything.",
    note: "Display + editorial — self-hosted, replaces Fraunces",
  },
  {
    family: "Geist Sans",
    sample: "An office of agents working in the open.",
    note: "Body sans — self-hosted, replaces Source Serif 4 / IBM Plex Serif / Press Start 2P",
  },
  {
    family: "Geist Mono",
    sample: "wuphf reset --force",
    note: "Code mono — self-hosted",
  },
  {
    family: "Geist Pixel",
    sample: "Terminal stamp — 09:42",
    note: "Pixel/retro accent — square style, alias for Silkscreen, replaces VT323 + Press Start 2P",
  },
];

export const Scale: StoryObj = {
  render: () => (
    <Section
      title="Size scale"
      description="The full type scale. Body default is `--text-base` (13px). Headers step up; captions step down."
    >
      <div
        style={{
          padding: "0 16px 16px",
          maxWidth: 920,
          color: "var(--text)",
        }}
      >
        {SIZES.map(({ token, size }) => (
          <TokenRow
            key={token}
            token={token}
            preview={
              <span style={{ fontSize: `var(${token})` }}>
                The quick brown fox jumps over the lazy dog
              </span>
            }
            note={`${size}px`}
          />
        ))}
      </div>
    </Section>
  ),
};

export const Families: StoryObj = {
  render: () => (
    <Section
      title="Token families"
      description="Three token families cover most surfaces. Reach for `--font-mono` for kbd, command output, and tokens."
    >
      <div
        style={{
          padding: "0 16px 16px",
          maxWidth: 920,
          color: "var(--text)",
        }}
      >
        {FAMILIES.map(({ token, label, note }) => (
          <TokenRow
            key={token}
            token={token}
            preview={
              <span style={{ fontFamily: `var(${token})`, fontSize: 16 }}>
                {label} — Aa Bb Cc 0123 → ⌘K
              </span>
            }
            note={note}
          />
        ))}
      </div>
    </Section>
  ),
};

export const DisplayFonts: StoryObj = {
  name: "Display fonts",
  render: () => (
    <Section
      title="Loaded display faces"
      description="Self-hosted via @fontsource — no Google Fonts CDN at runtime. Apply by name where you need editorial or retro presence."
    >
      <div
        style={{
          padding: "0 16px 16px",
          maxWidth: 920,
          color: "var(--text)",
        }}
      >
        {DISPLAY_FONTS.map(({ family, sample, note }) => (
          <TokenRow
            key={family}
            token={family}
            preview={
              <span style={{ fontFamily: family, fontSize: 22 }}>{sample}</span>
            }
            note={note}
          />
        ))}
      </div>
    </Section>
  ),
};

export const Weights: StoryObj = {
  render: () => (
    <Section
      title="Weights"
      description="Use 400 for body, 500 for emphasis, 600 for section headings. Reserve 700 for display."
    >
      <div style={{ padding: "0 16px 16px", maxWidth: 920 }}>
        {[400, 500, 600, 700].map((weight) => (
          <TokenRow
            key={weight}
            token={`font-weight: ${weight}`}
            preview={
              <span style={{ fontWeight: weight, fontSize: 17 }}>
                The shared brain remembers everything
              </span>
            }
            note={
              weight === 400
                ? "body"
                : weight === 500
                  ? "emphasis"
                  : weight === 600
                    ? "heading"
                    : "display"
            }
          />
        ))}
      </div>
    </Section>
  ),
};

import type { Meta, StoryObj } from "@storybook/react-vite";

import { Grid, Section, Swatch } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Color",
  parameters: { layout: "fullscreen" },
};

export default meta;

const NEUTRALS = [
  "--neutral-950",
  "--neutral-900",
  "--neutral-800",
  "--neutral-700",
  "--neutral-600",
  "--neutral-500",
  "--neutral-400",
  "--neutral-300",
  "--neutral-200",
  "--neutral-100",
  "--neutral-50",
  "--neutral-10",
];

const ramp = (name: string) =>
  [500, 400, 300, 200, 100].map((step) => `--${name}-${step}`);

const SEMANTIC_RAMPS: Array<{ name: string; tokens: string[] }> = [
  { name: "tertiary (brand purple)", tokens: ramp("tertiary") },
  { name: "cyan", tokens: ramp("cyan") },
  { name: "olive", tokens: ramp("olive") },
  { name: "success", tokens: ramp("success") },
  { name: "error", tokens: ramp("error") },
  { name: "warning", tokens: ramp("warning") },
];

const SURFACES: Array<{ token: string; alias?: string }> = [
  { token: "--bg" },
  { token: "--bg-warm", alias: "→ --neutral-50" },
  { token: "--bg-subtle", alias: "→ --neutral-10" },
  { token: "--bg-card" },
];

const TEXTS: Array<{ token: string; alias?: string }> = [
  { token: "--text", alias: "→ --neutral-900" },
  { token: "--text-secondary", alias: "→ --neutral-500" },
  { token: "--text-tertiary", alias: "→ --neutral-400" },
  { token: "--text-disabled", alias: "→ --neutral-300" },
];

const BORDERS: Array<{ token: string; alias?: string }> = [
  { token: "--border-light", alias: "→ --neutral-50" },
  { token: "--border", alias: "→ --neutral-100" },
  { token: "--border-dark", alias: "→ --neutral-200" },
  { token: "--border-strong", alias: "→ --neutral-300" },
];

const ACCENT: Array<{ token: string; alias?: string }> = [
  { token: "--accent", alias: "→ --tertiary-400" },
  { token: "--accent-warm", alias: "→ --tertiary-500" },
  { token: "--accent-bg", alias: "→ --tertiary-100" },
  { token: "--accent-bg-strong" },
];

const SEMANTIC_ALIASES: Array<{ token: string; alias?: string }> = [
  { token: "--green", alias: "→ --success-400" },
  { token: "--green-bg", alias: "→ --success-100" },
  { token: "--red", alias: "→ --error-400" },
  { token: "--red-bg", alias: "→ --error-100" },
  { token: "--yellow", alias: "→ --warning-400" },
  { token: "--yellow-bg", alias: "→ --warning-100" },
  { token: "--blue", alias: "→ --cyan-500" },
  { token: "--blue-bg", alias: "→ --cyan-100" },
];

const BUBBLE_STATES = [
  "--bubble-shipping",
  "--bubble-plotting",
  "--bubble-talking",
  "--bubble-stuck",
  "--bubble-idle",
];

export const Surfaces: StoryObj = {
  render: () => (
    <div>
      <Section
        title="Surfaces"
        description="Page, card, and warm-secondary fills. Pull from these — never reach for raw neutrals — so themes can swap them coherently."
      >
        <Grid cols={4}>
          {SURFACES.map(({ token, alias }) => (
            <Swatch key={token} token={token} alias={alias} height={64} />
          ))}
        </Grid>
      </Section>
      <Section
        title="Text"
        description="Foreground roles. Pair `--text` on `--bg`; `--text-secondary` is your default muted; `--text-tertiary` is for captions and metadata."
      >
        <Grid cols={4}>
          {TEXTS.map(({ token, alias }) => (
            <Swatch key={token} token={token} alias={alias} height={64} />
          ))}
        </Grid>
      </Section>
      <Section
        title="Borders"
        description="Hairlines for cards, inputs, dividers. `--border` is the everyday line; `--border-light` for nested separators."
      >
        <Grid cols={4}>
          {BORDERS.map(({ token, alias }) => (
            <Swatch key={token} token={token} alias={alias} height={64} />
          ))}
        </Grid>
      </Section>
    </div>
  ),
};

export const Accent: StoryObj = {
  render: () => (
    <Section
      title="Accent / brand"
      description="Primary action and selection chrome. The brand purple ramp sits in `--tertiary-*`; `--accent` is the default chip color."
    >
      <Grid cols={4}>
        {ACCENT.map(({ token, alias }) => (
          <Swatch key={token} token={token} alias={alias} />
        ))}
      </Grid>
    </Section>
  ),
};

export const Semantic: StoryObj = {
  render: () => (
    <div>
      <Section
        title="Semantic ramps"
        description="500 = strongest, 100 = softest fill. Use 400/500 for foregrounds, 100/200 for backgrounds."
      >
        {SEMANTIC_RAMPS.map(({ name, tokens }) => (
          <div key={name} style={{ marginBottom: 16 }}>
            <h4
              style={{
                margin: "0 16px 8px",
                fontSize: 11,
                fontFamily: "var(--font-mono)",
                color: "var(--text-secondary)",
              }}
            >
              {name}
            </h4>
            <Grid cols={5}>
              {tokens.map((token) => (
                <Swatch key={token} token={token} />
              ))}
            </Grid>
          </div>
        ))}
      </Section>
      <Section
        title="Semantic aliases"
        description="Convenience aliases over the ramps — use these when you want stable hooks for status meaning."
      >
        <Grid cols={4}>
          {SEMANTIC_ALIASES.map(({ token, alias }) => (
            <Swatch key={token} token={token} alias={alias} />
          ))}
        </Grid>
      </Section>
    </div>
  ),
};

export const Neutrals: StoryObj = {
  render: () => (
    <Section
      title="Neutral ramp"
      description="Source of truth for greys. Compose semantic surface and text tokens out of these instead of using them directly."
    >
      <Grid cols={6}>
        {NEUTRALS.map((token) => (
          <Swatch key={token} token={token} />
        ))}
      </Grid>
    </Section>
  ),
};

export const AgentBubbleStates: StoryObj = {
  name: "Agent bubble states",
  render: () => (
    <Section
      title="Agent event bubble states"
      description="Per-state colors that drive `.sidebar-agent-pill`. Used by AgentEventPill — see Sidebar / AgentEventPill stories."
    >
      <Grid cols={5}>
        {BUBBLE_STATES.map((token) => (
          <Swatch key={token} token={token} />
        ))}
      </Grid>
    </Section>
  ),
};

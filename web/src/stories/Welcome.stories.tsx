import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "About/Introduction",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Intro: StoryObj = {
  render: () => (
    <div
      style={{
        maxWidth: 760,
        margin: "32px auto",
        padding: 32,
        color: "var(--text)",
        fontFamily: "var(--font-sans)",
        lineHeight: 1.6,
      }}
    >
      <h1
        style={{
          fontFamily: "Newsreader, serif",
          fontSize: 40,
          fontWeight: 600,
          margin: 0,
          marginBottom: 8,
          letterSpacing: "-0.02em",
        }}
      >
        Wuphf Design System
      </h1>
      <p
        style={{
          color: "var(--text-secondary)",
          fontSize: 16,
          margin: 0,
          marginBottom: 28,
        }}
      >
        Tokens, atoms, molecules, organisms, and patterns for the Wuphf web
        app — a context graph platform for AI agents.
      </p>

      <Section title="Foundations">
        <Item href="?path=/story/design-system-foundations-color--surfaces" label="Color" desc="Surfaces, text, borders, accent, semantic ramps, neutrals" />
        <Item href="?path=/story/design-system-foundations-typography--scale" label="Typography" desc="Size scale, font families, display faces, weights" />
        <Item href="?path=/story/design-system-foundations-spacing--scale" label="Spacing" desc="Six-step linear ramp from 4–24px" />
        <Item href="?path=/story/design-system-foundations-radii--all" label="Radii" desc="Four radius steps + full circle" />
        <Item href="?path=/story/design-system-foundations-shadows--all" label="Shadows" desc="Popover token + inlined elevation patterns" />
        <Item href="?path=/story/design-system-foundations-motion--durations" label="Motion" desc="Durations + reduced-motion guidance" />
        <Item href="?path=/story/design-system-foundations-iconography--iconoir-set" label="Iconography" desc="Iconoir set + custom marks (Lightning, Harness, PixelAvatar)" />
      </Section>

      <Section title="Atoms">
        <Item href="?path=/story/design-system-atoms-button--default" label="Button" desc="Primary action — 4 variants × 3 sizes" />
        <Item href="?path=/story/design-system-atoms-badge--variants" label="Badge" desc="Semantic inline labels — 5 variants" />
        <Item href="?path=/story/design-system-atoms-input--default" label="Input" desc="Text inputs and textarea" />
        <Item href="?path=/story/design-system-atoms-kbd--default" label="Kbd" desc="Keyboard key glyph + sequences" />
        <Item href="?path=/story/design-system-atoms-status-dot--variants" label="Status dot" desc="6px presence + agent activity states" />
        <Item href="?path=/story/design-system-atoms-harnessbadge--default" label="HarnessBadge" desc="Agent provider identity glyph" />
        <Item href="?path=/story/design-system-atoms-redactedbadge--default" label="RedactedBadge" desc="Privacy/redaction marker" />
        <Item href="?path=/story/design-system-atoms-lightningicon--default" label="LightningIcon" desc="Inline lightning bolt — replaces ⚡" />
        <Item href="?path=/story/design-system-atoms-pixelavatar--default" label="PixelAvatar" desc="Procedural agent portrait" />
      </Section>

      <Section title="Molecules">
        <Item href="?path=/story/design-system-molecules-breadcrumb--two-level" label="Breadcrumb" desc="Object navigation path" />
        <Item href="?path=/story/design-system-molecules-card--default" label="Card" desc="Surface for grouped content" />
        <Item href="?path=/story/design-system-molecules-empty-state--minimal" label="Empty state" desc="Zero-data surfaces" />
        <Item href="?path=/story/design-system-molecules-collapsiblesection--default" label="CollapsibleSection" desc="Accordion section with header + meta" />
        <Item href="?path=/story/design-system-molecules-commandrow--default" label="CommandRow" desc="Inline shell command with copy" />
        <Item href="?path=/story/design-system-molecules-inlinecommand--warning" label="InlineCommand" desc="Click-to-run command chip" />
        <Item href="?path=/story/design-system-molecules-toast--playground" label="Toast" desc="Notifications + undo affordance" />
      </Section>

      <Section title="Organisms">
        <Item href="?path=/story/design-system-organisms-confirmdialog--playground" label="ConfirmDialog" desc="Imperative confirm modal" />
        <Item href="?path=/story/design-system-organisms-wipemodal--critical" label="WipeModal" desc="Type-the-phrase destructive gate" />
        <Item href="?path=/story/design-system-organisms-sidepanel--default" label="SidePanel" desc="Right-aligned detail panel" />
        <Item href="?path=/story/design-system-organisms-helpmodal--default" label="HelpModal" desc="Keyboard shortcut reference" />
        <Item href="?path=/story/design-system-organisms-themeswitcher--default" label="ThemeSwitcher" desc="Theme menu trigger" />
      </Section>

      <Section title="Patterns">
        <Item href="?path=/story/patterns-focus-ring--examples" label="Focus ring" desc="System focus ring tokens + behavior" />
        <Item href="?path=/story/patterns-shred-warning--modal-copy" label="Shred warning" desc="Canonical destructive copy module" />
      </Section>

      <Section title="Features">
        <Item href="?path=/story/features-agents-agenteventpill--halo" label="Agents / AgentEventPill" desc="Activity pill with halo / holding / idle / stuck" />
        <Item href="?path=/story/features-notebook-draftstamp--default" label="Notebook / DraftStamp" desc="Rotated DRAFT marker on entries" />
        <Item href="?path=/story/features-notebook-bylinestrip--draft" label="Notebook / ByLineStrip" desc="Author + status + last-edited byline" />
      </Section>

      <Section title="Authoring rules">
        <Note>
          Sidebar order is pinned in <code>.storybook/preview.tsx</code>:
          About → Design System (Foundations → Atoms → Molecules → Organisms →
          Templates) → Patterns → Features → Pages.
        </Note>
        <Note>
          Aim for <strong>at most ~5 stories per component</strong>:
          <code>Default</code> (controls playground),
          <code> Variants</code> (matrix), <code> Sizes</code>,
          <code> States</code>, <code> InContext</code>. Disable controls on
          matrix stories so the args panel doesn't lie about what's rendered.
          See <code>Design System / Atoms / Button</code> for the reference.
        </Note>
        <Note>
          Style with design tokens — <code>--text</code>, <code>--bg-card</code>,
          <code>--accent</code>, <code>--radius-md</code>, <code>--space-3</code>.
          Hardcoded colors break the theme switcher.
        </Note>
        <Note>
          Theme switcher in the toolbar swaps <code>data-theme</code> and loads
          <code>/themes/&lt;id&gt;.css</code> — verify your component looks
          right in all three themes (Nex Light, Nex Dark, Noir Gold).
        </Note>
        <Note>
          For store- or data-driven components, seed Zustand inside the story
          (see <code>Features / Agents / AgentEventPill</code>) rather than
          mounting the full app shell.
        </Note>
      </Section>
    </div>
  ),
};

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section style={{ marginBottom: 32 }}>
      <h2
        style={{
          margin: 0,
          marginBottom: 12,
          fontSize: 12,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.08em",
          color: "var(--text-tertiary)",
        }}
      >
        {title}
      </h2>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        {children}
      </div>
    </section>
  );
}

function Item({
  href,
  label,
  desc,
}: {
  href: string;
  label: string;
  desc: string;
}) {
  return (
    <a
      href={href}
      style={{
        display: "grid",
        gridTemplateColumns: "220px 1fr",
        gap: 16,
        padding: "10px 12px",
        borderRadius: "var(--radius-sm)",
        textDecoration: "none",
        color: "var(--text)",
        fontSize: 14,
        background: "transparent",
        transition: "background var(--duration-base)",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.background = "var(--bg-warm)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.background = "transparent";
      }}
    >
      <span style={{ fontWeight: 600 }}>{label}</span>
      <span style={{ color: "var(--text-secondary)", fontSize: 13 }}>
        {desc}
      </span>
    </a>
  );
}

function Note({ children }: { children: React.ReactNode }) {
  return (
    <p
      style={{
        margin: 0,
        marginBottom: 12,
        fontSize: 14,
        color: "var(--text-secondary)",
      }}
    >
      {children}
    </p>
  );
}

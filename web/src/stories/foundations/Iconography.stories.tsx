import {
  ChatBubble,
  CheckCircle,
  MailIn,
  NavArrowDown,
  NavArrowLeft,
  NavArrowRight,
  NavArrowUp,
  PlaySolid,
  Plus,
  Refresh,
  Settings,
  SidebarCollapse,
  Terminal,
  WarningTriangle,
  Xmark,
} from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { HarnessBadge } from "../../components/ui/HarnessBadge";
import { LightningIcon } from "../../components/ui/LightningIcon";
import { PixelAvatar } from "../../components/ui/PixelAvatar";

import { Grid, Section } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Iconography",
  parameters: { layout: "fullscreen" },
};

export default meta;

const ICONOIR_USED = [
  { Icon: ChatBubble, name: "ChatBubble" },
  { Icon: CheckCircle, name: "CheckCircle" },
  { Icon: MailIn, name: "MailIn" },
  { Icon: NavArrowDown, name: "NavArrowDown" },
  { Icon: NavArrowLeft, name: "NavArrowLeft" },
  { Icon: NavArrowRight, name: "NavArrowRight" },
  { Icon: NavArrowUp, name: "NavArrowUp" },
  { Icon: PlaySolid, name: "PlaySolid" },
  { Icon: Plus, name: "Plus" },
  { Icon: Refresh, name: "Refresh" },
  { Icon: Settings, name: "Settings" },
  { Icon: SidebarCollapse, name: "SidebarCollapse" },
  { Icon: Terminal, name: "Terminal" },
  { Icon: WarningTriangle, name: "WarningTriangle" },
  { Icon: Xmark, name: "Xmark" },
];

function IconCell({
  name,
  children,
}: {
  name: string;
  children: React.ReactNode;
}) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 6,
        padding: 12,
        border: "1px solid var(--border-light)",
        borderRadius: "var(--radius-sm)",
        background: "var(--bg-card)",
        fontSize: 11,
        color: "var(--text-secondary)",
      }}
    >
      <div style={{ color: "var(--text)" }}>{children}</div>
      <code style={{ fontFamily: "var(--font-mono)", fontSize: 10 }}>
        {name}
      </code>
    </div>
  );
}

export const Iconoir: StoryObj = {
  name: "Iconoir set",
  render: () => (
    <Section
      title="Iconoir (currently used)"
      description="Icon library is `iconoir-react` — pulled by name. Stroke-only, 1.5px width by default; pair with `currentColor` to inherit text color."
    >
      <Grid cols={6}>
        {ICONOIR_USED.map(({ Icon, name }) => (
          <IconCell key={name} name={name}>
            <Icon width={24} height={24} />
          </IconCell>
        ))}
      </Grid>
    </Section>
  ),
};

export const Custom: StoryObj = {
  name: "Custom marks",
  render: () => (
    <div>
      <Section
        title="LightningIcon"
        description="Inline monochrome lightning. Replaces the cross-OS-inconsistent ⚡ emoji."
      >
        <Grid cols={6}>
          {[12, 16, 20, 24, 32, 48].map((size) => (
            <IconCell key={size} name={`${size}px`}>
              <LightningIcon size={size} />
            </IconCell>
          ))}
        </Grid>
      </Section>
      <Section
        title="HarnessBadge"
        description="Identity glyph for each agent harness. Lobster + caduceus + monogram marks. See UI / HarnessBadge for variants."
      >
        <Grid cols={5}>
          {(
            [
              "claude-code",
              "codex",
              "opencode",
              "openclaw",
              "hermes-agent",
            ] as const
          ).map((kind) => (
            <IconCell key={kind} name={kind}>
              <HarnessBadge kind={kind} size={32} />
            </IconCell>
          ))}
        </Grid>
      </Section>
      <Section
        title="PixelAvatar"
        description="Procedural agent portrait, drawn on a canvas. Deterministic per slug — same slug ⇒ same face."
      >
        <Grid cols={6}>
          {["alex", "lina", "ops", "scout", "sage", "echo"].map((slug) => (
            <IconCell key={slug} name={slug}>
              <PixelAvatar slug={slug} size={48} />
            </IconCell>
          ))}
        </Grid>
      </Section>
    </div>
  ),
};

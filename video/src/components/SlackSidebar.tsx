import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";
import { colors, fonts, starterAgents } from "../theme";

interface SlackSidebarProps {
  enterFrame?: number;
  showCost?: boolean;
  costText?: string;
}

export const SlackSidebar: React.FC<SlackSidebarProps> = ({
  enterFrame = 0,
  showCost = false,
  costText = "$0.02 this session",
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - enterFrame;

  const sidebarOpacity = interpolate(elapsed, [0, 10], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <div
      style={{
        width: 280,
        height: "100%",
        backgroundColor: colors.bgSidebar,
        borderRight: "1px solid #35363A",
        opacity: sidebarOpacity,
        display: "flex",
        flexDirection: "column",
        fontFamily: fonts.sans,
      }}
    >
      {/* Workspace header */}
      <div style={{ padding: "20px 20px 16px", borderBottom: "1px solid #35363A" }}>
        <div style={{ fontSize: 22, fontWeight: 800, color: colors.textBright, letterSpacing: -0.5 }}>
          WUPHF
        </div>
        <div style={{ fontSize: 13, color: colors.green, marginTop: 2, display: "flex", alignItems: "center", gap: 6 }}>
          <div style={{ width: 8, height: 8, borderRadius: "50%", backgroundColor: colors.green }} />
          online
        </div>
      </div>

      {/* Channels */}
      <div style={{ padding: "16px 0" }}>
        <div style={{ padding: "6px 20px", fontSize: 13, fontWeight: 600, color: colors.textMuted, textTransform: "uppercase" as const, letterSpacing: 1 }}>
          Channels
        </div>
        <div
          style={{
            padding: "6px 20px 6px 24px",
            fontSize: 16,
            color: colors.textBright,
            backgroundColor: colors.accent,
            margin: "4px 12px",
            borderRadius: 8,
            fontWeight: 500,
          }}
        >
          # general
        </div>
      </div>

      {/* Team */}
      <div style={{ padding: "8px 0", flex: 1 }}>
        <div style={{ padding: "6px 20px", fontSize: 13, fontWeight: 600, color: colors.textMuted, textTransform: "uppercase" as const, letterSpacing: 1 }}>
          Team
        </div>
        {starterAgents.map((agent, i) => {
          const agentDelay = enterFrame + 15 + i * 6;
          const agentOpacity = interpolate(frame, [agentDelay, agentDelay + 8], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          });

          const statusLabels = ["talking", "shipping", "plotting"];
          const dotPulse = Math.sin((frame - agentDelay) * 0.15 + i) * 0.15 + 0.85;

          return (
            <div
              key={agent.slug}
              style={{
                opacity: agentOpacity,
                padding: "5px 20px 5px 24px",
                display: "flex",
                alignItems: "center",
                gap: 10,
                fontSize: 16,
                color: colors.text,
              }}
            >
              <div
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  backgroundColor: agent.color,
                  opacity: dotPulse,
                  flexShrink: 0,
                }}
              />
              <span style={{ fontWeight: 500 }}>{agent.name}</span>
              <span style={{ fontSize: 12, color: colors.textMuted, marginLeft: "auto" }}>
                {statusLabels[i]}
              </span>
            </div>
          );
        })}
      </div>

      {/* Cost display at bottom */}
      {showCost && (
        <div
          style={{
            padding: "16px 20px",
            borderTop: "1px solid #35363A",
            display: "flex",
            flexDirection: "column",
            gap: 4,
          }}
        >
          <div style={{ fontSize: 12, color: colors.textMuted, textTransform: "uppercase" as const, letterSpacing: 1 }}>
            Session Cost
          </div>
          <div style={{ fontSize: 24, fontWeight: 700, color: colors.green }}>
            {costText}
          </div>
        </div>
      )}
    </div>
  );
};

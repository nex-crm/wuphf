import { AbsoluteFill, useCurrentFrame, interpolate, Easing, spring, Sequence } from "remotion";
import { colors, fonts, sec, FPS, slack } from "../theme";
import { DotGrid, RadialGlow } from "../components/DotGrid";

// ═══════════════════════════════════════════════════════════════
// PANEL 1: 9x cheaper — hero number + animated chart
// ═══════════════════════════════════════════════════════════════
const Panel1Savings: React.FC = () => {
  const frame = useCurrentFrame();

  const headlineScale = spring({ frame, fps: FPS, config: { damping: 10, stiffness: 150 } });
  const headlineOp = interpolate(frame, [0, 10], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Chart progress
  const progress = interpolate(frame, [15, 60], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  const jokeOp = interpolate(frame, [50, 65], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  const chartW = 900;
  const chartH = 280;
  const pcY = 260 - 220 * progress;
  const wuphfY = 220;
  const pcX = chartW * progress;
  const wuphfX = chartW * progress;

  return (
    <AbsoluteFill style={{
      display: "flex",
      flexDirection: "column",
      alignItems: "flex-start",
      justifyContent: "center",
      padding: "0 140px",
      gap: 30,
    }}>
      <RadialGlow color={slack.presence} x="75%" y="30%" size={900} opacity={0.12} />

      {/* Eyebrow */}
      <div style={{
        opacity: headlineOp,
        fontFamily: fonts.mono, fontSize: 18, color: slack.textTertiary,
        textTransform: "uppercase" as const, letterSpacing: 4,
      }}>
        Efficiency
      </div>

      {/* Hero number */}
      <div style={{
        opacity: headlineOp,
        transform: `scale(${headlineScale})`,
        transformOrigin: "left center",
        fontFamily: fonts.sans,
        fontSize: 220,
        fontWeight: 900,
        lineHeight: 0.9,
        letterSpacing: -8,
        color: slack.presence,
      }}>
        9x
      </div>

      <div style={{
        opacity: headlineOp,
        fontFamily: fonts.sans,
        fontSize: 64,
        fontWeight: 800,
        color: "#FFF",
        lineHeight: 1,
        letterSpacing: -2,
        marginTop: -16,
      }}>
        less token burn. 🔥
      </div>

      {/* Animated chart */}
      <svg width={chartW} height={chartH + 40} viewBox={`0 0 ${chartW} ${chartH + 40}`} style={{ marginTop: 12 }}>
        {/* Paperclip line (ascending, red) */}
        <line x1="0" y1="260" x2={pcX} y2={pcY} stroke={slack.red} strokeWidth="5" strokeLinecap="round" />
        <circle cx={pcX} cy={pcY} r="10" fill={slack.red} />
        {progress > 0.5 && (
          <text x={pcX - 20} y={pcY - 20} fill={slack.red} fontFamily={fonts.mono} fontSize="20" fontWeight="700" textAnchor="end">
            Paperclip — 500K
          </text>
        )}

        {/* WUPHF line (flat, green) */}
        <line x1="0" y1={wuphfY} x2={wuphfX} y2={wuphfY} stroke={slack.presence} strokeWidth="5" strokeLinecap="round" />
        <circle cx={wuphfX} cy={wuphfY} r="10" fill={slack.presence} />
        {progress > 0.3 && (
          <text x={wuphfX - 20} y={wuphfY + 36} fill={slack.presence} fontFamily={fonts.mono} fontSize="20" fontWeight="700" textAnchor="end">
            WUPHF — 31K
          </text>
        )}
      </svg>

      {/* Joke */}
      <div style={{
        opacity: jokeOp,
        fontFamily: fonts.sans,
        fontSize: 30,
        color: slack.textSecondary,
        fontStyle: "italic",
        marginTop: 8,
      }}>
        Paperclip leaves scorch marks. We keep things cool.
      </div>
    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// PANEL 2: Context graph — your company, in a graph
// ═══════════════════════════════════════════════════════════════
const Panel2Graph: React.FC<{ freezeAfter?: number }> = ({ freezeAfter }) => {
  const raw = useCurrentFrame();
  const frame = freezeAfter !== undefined ? Math.min(raw, freezeAfter) : raw;

  const headlineOp = interpolate(frame, [0, 12], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const jokeOp = interpolate(frame, [50, 65], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Graph positioned on the right half, clear of the left-aligned headline
  const nodes = [
    { x: 1340, y: 280, label: "My Startup", emoji: "🏢", color: colors.ceo, size: 28, delay: 0 },
    { x: 1140, y: 420, label: "Sarah",      emoji: "👩",  color: colors.pm, size: 22, delay: 6 },
    { x: 1340, y: 480, label: "Acme Corp",  emoji: "🏭", color: colors.fe, size: 22, delay: 10 },
    { x: 1540, y: 420, label: "Q2 Launch",  emoji: "🚀", color: colors.gtm, size: 22, delay: 14 },
    { x: 1060, y: 620, label: "Roadmap",    emoji: "🗺️", color: colors.ai, size: 18, delay: 22 },
    { x: 1240, y: 680, label: "$40k deal",  emoji: "💰", color: colors.cro, size: 18, delay: 26 },
    { x: 1440, y: 680, label: "Contract",   emoji: "📄", color: colors.designer, size: 18, delay: 30 },
    { x: 1620, y: 620, label: "Pricing",    emoji: "💵", color: colors.be, size: 18, delay: 34 },
  ];

  const edges = [
    [0, 1], [0, 2], [0, 3],
    [1, 4], [2, 5], [2, 6], [3, 7], [2, 3],
  ];

  return (
    <AbsoluteFill>
      <RadialGlow color={colors.ai} x="70%" y="50%" size={1000} opacity={0.1} />

      {/* Left-aligned headline — vertically centered */}
      <div style={{
        position: "absolute",
        left: 140, top: 360,
        opacity: headlineOp,
      }}>
        <div style={{
          fontFamily: fonts.mono, fontSize: 20, color: slack.textTertiary,
          textTransform: "uppercase" as const, letterSpacing: 4, marginBottom: 20,
        }}>
          Memory · powered by Nex
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 96,
          fontWeight: 900,
          color: "#FFF",
          lineHeight: 0.95,
          letterSpacing: -3,
          maxWidth: 680,
        }}>
          Your company's<br/>
          <span style={{ color: colors.ai }}>context graph.</span>
        </div>
      </div>

      {/* Graph — right side, floating */}
      <svg
        width="1920" height="1080"
        style={{ position: "absolute", inset: 0 }}
      >
        {edges.map(([a, b], i) => {
          const startDelay = Math.max(nodes[a].delay, nodes[b].delay) + 4;
          const edgeProgress = interpolate(frame, [startDelay, startDelay + 12], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.out(Easing.cubic),
          });
          const na = nodes[a];
          const nb = nodes[b];
          const midX = na.x + (nb.x - na.x) * edgeProgress;
          const midY = na.y + (nb.y - na.y) * edgeProgress;
          return (
            <line
              key={i}
              x1={na.x} y1={na.y}
              x2={midX} y2={midY}
              stroke={slack.accent}
              strokeWidth="2.5"
              strokeOpacity={0.4}
            />
          );
        })}
        {nodes.map((n, i) => {
          const nodeScale = spring({
            frame: Math.max(0, frame - n.delay),
            fps: FPS,
            config: { damping: 10, stiffness: 150 },
          });
          const nodeOp = interpolate(frame, [n.delay, n.delay + 8], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          });
          const pulse = Math.sin(frame * 0.08 + i) * 4 + n.size + 12;
          return (
            <g key={i} opacity={nodeOp} transform={`translate(${n.x}, ${n.y}) scale(${nodeScale})`}>
              <circle r={pulse} fill={n.color} fillOpacity="0.18" />
              <circle r={n.size} fill={n.color} />
              <text textAnchor="middle" dy={n.size * 0.35} fontSize={n.size * 1.1}>{n.emoji}</text>
              <text y={n.size + 28} textAnchor="middle" fill="#FFF" fontFamily={fonts.sans} fontSize="18" fontWeight="700">{n.label}</text>
            </g>
          );
        })}
      </svg>

      {/* Joke */}
      <div style={{
        position: "absolute",
        left: 140, bottom: 160,
        opacity: jokeOp,
        fontFamily: fonts.sans,
        fontSize: 28,
        color: slack.textSecondary,
        fontStyle: "italic",
        maxWidth: 720,
      }}>
        Already better memory than your new hire. 🧠
      </div>
    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// PANEL 3: Integrations — tools fly in from edges
// ═══════════════════════════════════════════════════════════════
const Panel3Integrations: React.FC = () => {
  const frame = useCurrentFrame();

  const headlineOp = interpolate(frame, [0, 12], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const jokeOp = interpolate(frame, [50, 65], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });

  // Counter ticks up from 0 to 1000
  const counter = Math.floor(interpolate(frame, [20, 60], [0, 1000], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  }));

  // Tools scatter around the screen, each entering from a random direction
  const tools = [
    { emoji: "🐙", label: "GitHub" },
    { emoji: "📧", label: "Gmail" },
    { emoji: "💬", label: "Slack" },
    { emoji: "📊", label: "HubSpot" },
    { emoji: "💳", label: "Stripe" },
    { emoji: "📅", label: "Calendar" },
    { emoji: "🎯", label: "Linear" },
    { emoji: "🗃️", label: "Notion" },
    { emoji: "📞", label: "Twilio" },
    { emoji: "✉️", label: "Postmark" },
    { emoji: "🔍", label: "Apollo" },
    { emoji: "📨", label: "Intercom" },
  ];

  // Positions around the headline
  const positions = [
    { x: 1340, y: 180 }, { x: 1520, y: 280 }, { x: 1680, y: 200 },
    { x: 1420, y: 440 }, { x: 1640, y: 520 }, { x: 1300, y: 620 },
    { x: 1540, y: 700 }, { x: 1720, y: 640 }, { x: 1380, y: 840 },
    { x: 1620, y: 880 }, { x: 1260, y: 380 }, { x: 1780, y: 420 },
  ];

  return (
    <AbsoluteFill>
      <RadialGlow color={colors.yellow} x="75%" y="50%" size={900} opacity={0.08} />

      {/* Left-aligned headline */}
      <div style={{
        position: "absolute",
        left: 140, top: 260,
        opacity: headlineOp,
      }}>
        <div style={{
          fontFamily: fonts.mono, fontSize: 18, color: slack.textTertiary,
          textTransform: "uppercase" as const, letterSpacing: 4, marginBottom: 16,
        }}>
          Integrations
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 180,
          fontWeight: 900,
          color: colors.yellow,
          lineHeight: 0.9,
          letterSpacing: -6,
          fontVariantNumeric: "tabular-nums" as const,
        }}>
          {counter.toLocaleString()}+
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 56,
          fontWeight: 800,
          color: "#FFF",
          lineHeight: 1,
          letterSpacing: -1.5,
          marginTop: 8,
        }}>
          integrations.<br/>
          One click to connect.
        </div>
      </div>

      {/* Tool pills scattered in right area */}
      {tools.map((tool, i) => {
        const delay = 20 + i * 3;
        const opacity = interpolate(frame, [delay, delay + 10], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        });
        const scale = spring({
          frame: Math.max(0, frame - delay),
          fps: FPS,
          config: { damping: 12, stiffness: 180 },
        });
        const pos = positions[i];
        const drift = Math.sin((frame + i * 20) * 0.04) * 4;

        return (
          <div
            key={i}
            style={{
              position: "absolute",
              left: pos.x,
              top: pos.y + drift,
              opacity,
              transform: `scale(${scale})`,
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: "10px 18px",
              borderRadius: 100,
              backgroundColor: slack.bgWarm,
              border: `1px solid ${slack.border}`,
              fontFamily: fonts.sans,
              fontSize: 18,
              color: slack.text,
              fontWeight: 600,
              whiteSpace: "nowrap" as const,
              boxShadow: "0 4px 20px rgba(0,0,0,0.3)",
            }}
          >
            <span style={{ fontSize: 22 }}>{tool.emoji}</span>
            {tool.label}
          </div>
        );
      })}

      {/* Joke */}
      <div style={{
        position: "absolute",
        left: 140, bottom: 140,
        opacity: jokeOp,
        fontFamily: fonts.sans,
        fontSize: 28,
        color: slack.textSecondary,
        fontStyle: "italic",
      }}>
        Fewer clicks than raising an IT ticket.
      </div>
    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// PAUSE OVERLAY — fourth-wall break "paused video" effect
// ═══════════════════════════════════════════════════════════════
const PauseOverlay: React.FC<{ durationFrames: number }> = ({ durationFrames }) => {
  const frame = useCurrentFrame();
  // Quick fade in (3 frames), hold, click-press + fade out at end (last 5 frames)
  const fadeIn = interpolate(frame, [0, 3], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const fadeOut = interpolate(frame, [durationFrames - 5, durationFrames], [1, 0], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const opacity = Math.min(fadeIn, fadeOut);

  // Subtle pulse on play button during hold
  const pulse = 1 + Math.sin(frame * 0.08) * 0.03;
  // Click: in last 5 frames, scale down (pressed) then vanish
  const clickScale = interpolate(frame, [durationFrames - 5, durationFrames - 2], [1, 0.82], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const scale = frame < durationFrames - 5 ? pulse : clickScale;

  return (
    <AbsoluteFill style={{ opacity, pointerEvents: "none" }}>
      {/* Dim layer */}
      <AbsoluteFill style={{ backgroundColor: "rgba(0, 0, 0, 0.55)" }} />
      {/* Centered play button */}
      <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
        <div style={{
          width: 260, height: 260,
          borderRadius: "50%",
          backgroundColor: "rgba(255,255,255,0.12)",
          backdropFilter: "blur(4px)",
          border: "3px solid rgba(255,255,255,0.85)",
          display: "flex", alignItems: "center", justifyContent: "center",
          transform: `scale(${scale})`,
          boxShadow: "0 0 80px rgba(255,255,255,0.15)",
        }}>
          {/* Play triangle */}
          <svg width="100" height="110" viewBox="0 0 100 110">
            <polygon points="15,10 15,100 95,55" fill="#FFF" />
          </svg>
        </div>
      </AbsoluteFill>
      {/* Small "PAUSED" label below button */}
      <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center", marginTop: 220 }}>
        <div style={{
          fontFamily: fonts.mono,
          fontSize: 18,
          color: "rgba(255,255,255,0.75)",
          textTransform: "uppercase" as const,
          letterSpacing: 6,
        }}>
          Paused
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// MAIN SCENE: 3 sequential full-bleed panels + fourth-wall pause overlay
// ═══════════════════════════════════════════════════════════════
//
// Timing (local to Scene 6, which runs 26s):
//   0.0s-4.0s       Panel 1 (token burn)
//   4.0s-21.48s     Panel 2 (context graph) — freezes at local frame 99
//   7.3s-21.48s     PauseOverlay (14.18s = 425 frames) — fourth-wall break
//   21.48s-26.0s    Panel 3 (integrations)
//
// Narration sync (WuphfDemo.tsx places audio clips):
//   Scene 6a "nine times less token burn. a context graph of your entire company."  @ Scene 6 start+1s
//   Scene 6b "...wait. what the heck is a context graph?... if that's what the VCs want." — break window
//   Scene 6c "context graph. a thousand integrations, one click away." — resumes with play
export const Scene6MoneyShot: React.FC = () => {
  const PANEL2_FREEZE_FRAME = 99; // Panel-2-local frame where pause hits (~3.3s into Panel 2)
  const PAUSE_FROM = 219;         // Scene-6-local frame when pause overlay begins (7.3s)
  const PAUSE_DURATION = 361;     // 12.04s — matches trimmed break audio
  const PANEL3_FROM = 580;        // Scene-6-local frame when Panel 3 starts (19.33s)

  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10" }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />

      <Sequence from={0} durationInFrames={sec(4)}>
        <Panel1Savings />
      </Sequence>

      <Sequence from={sec(4)} durationInFrames={PANEL3_FROM - sec(4)}>
        <Panel2Graph freezeAfter={PANEL2_FREEZE_FRAME} />
      </Sequence>

      <Sequence from={PAUSE_FROM} durationInFrames={PAUSE_DURATION}>
        <PauseOverlay durationFrames={PAUSE_DURATION} />
      </Sequence>

      <Sequence from={PANEL3_FROM} durationInFrames={sec(24) - PANEL3_FROM}>
        <Panel3Integrations />
      </Sequence>
    </AbsoluteFill>
  );
};

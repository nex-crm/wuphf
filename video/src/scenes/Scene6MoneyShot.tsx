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
            Paperclip — 500k tokens
          </text>
        )}

        {/* WUPHF line (flat, green) */}
        <line x1="0" y1={wuphfY} x2={wuphfX} y2={wuphfY} stroke={slack.presence} strokeWidth="5" strokeLinecap="round" />
        <circle cx={wuphfX} cy={wuphfY} r="10" fill={slack.presence} />
        {progress > 0.3 && (
          <text x={wuphfX - 20} y={wuphfY + 36} fill={slack.presence} fontFamily={fonts.mono} fontSize="20" fontWeight="700" textAnchor="end">
            WUPHF — flat 31k
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
// PANEL 2: Knowledge graph — your company, in a graph
// ═══════════════════════════════════════════════════════════════
const Panel2Graph: React.FC = () => {
  const frame = useCurrentFrame();

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
          Context · powered by Nex
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 96,
          fontWeight: 900,
          color: "#FFF",
          lineHeight: 0.95,
          letterSpacing: -3,
          maxWidth: 640,
        }}>
          Your company,<br/>
          <span style={{ color: colors.ai }}>in a graph.</span>
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
// MAIN SCENE: 3 sequential full-bleed panels
// ═══════════════════════════════════════════════════════════════
export const Scene6MoneyShot: React.FC = () => {
  return (
    <AbsoluteFill style={{ backgroundColor: "#0B0D10" }}>
      <DotGrid color="#FFFFFF" opacity={0.04} spacing={40} size={1.2} />

      <Sequence from={0} durationInFrames={sec(4)}>
        <Panel1Savings />
      </Sequence>

      <Sequence from={sec(4)} durationInFrames={sec(4)}>
        <Panel2Graph />
      </Sequence>

      <Sequence from={sec(8)} durationInFrames={sec(4)}>
        <Panel3Integrations />
      </Sequence>
    </AbsoluteFill>
  );
};

import { AbsoluteFill, useCurrentFrame, interpolate, Easing, Sequence, spring, staticFile } from "remotion";
import { colors, fonts, sec, FPS, slack } from "../theme";
import { DotGrid } from "../components/DotGrid";

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

// ═══════════════════════════════════════════════════════════════
// PANEL 1: 9x cheaper — hero number + animated chart
// ═══════════════════════════════════════════════════════════════
const Panel1Savings: React.FC = () => {
  const frame = useCurrentFrame();

  const headlineScale = interpolate(frame, [0, 22], [0.94, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const headlineOp = interpolate(frame, [0, 18], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT });

  // Chart progress
  const progress = interpolate(frame, [15, 60], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  const jokeOp = interpolate(frame, [50, 70], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.inOut(Easing.cubic),
  });
  const jokeExpand = interpolate(frame, [50, 74], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.inOut(Easing.cubic),
  });

  // Bar chart — right-side, two big solid bars
  const PC_MAX = 560;             // paperclip at 500K
  const WUPHF_MAX = Math.round(PC_MAX / 9);  // 9× less → proportional
  const pcH = PC_MAX * progress;
  const wuphfH = WUPHF_MAX * progress;

  // Hero "9x" counts up from 1 → 9 in sync with the bar fill
  const heroCount = Math.max(
    1,
    Math.round(
      interpolate(frame, [15, 60], [1, 9], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
        easing: Easing.out(Easing.cubic),
      }),
    ),
  );

  // Bar value labels count up in sync with their fill
  const pcCount = Math.round(
    interpolate(frame, [15, 60], [0, 500], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.out(Easing.cubic),
    }),
  );
  const wuphfCount = Math.round(
    interpolate(frame, [15, 60], [0, 31], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.out(Easing.cubic),
    }),
  );

  return (
    <AbsoluteFill style={{
      display: "flex",
      flexDirection: "column",
      alignItems: "flex-start",
      justifyContent: "center",
      padding: "0 140px",
      gap: 30,
    }}>
      {/* Eyebrow */}
      <div style={{
        opacity: headlineOp,
        fontFamily: fonts.mono, fontSize: 20, color: "#9F4DBF",
        textTransform: "uppercase" as const, letterSpacing: 4,
        marginBottom: 20,
      }}>
        Efficiency
      </div>

      {/* Hero number — counts up 1 → 9 as bars grow */}
      <div style={{
        opacity: headlineOp,
        transform: `scale(${headlineScale})`,
        transformOrigin: "left center",
        fontFamily: fonts.sans,
        fontSize: 220,
        fontWeight: 900,
        lineHeight: 0.9,
        letterSpacing: -8,
        color: "#CF72D9",
        fontVariantNumeric: "tabular-nums" as const,
      }}>
        {heroCount}x
      </div>

      <div style={{
        opacity: headlineOp,
        fontFamily: fonts.sans,
        fontSize: 64,
        fontWeight: 700,
        color: "#FFEBFC",
        lineHeight: 1,
        letterSpacing: -2,
        marginTop: 20,
      }}>
        less token burn.
      </div>

      {/* Joke — grows in from 0 height, pushing the hero upward as it appears */}
      <div style={{
        overflow: "hidden",
        maxHeight: jokeExpand * 180,
        marginTop: jokeExpand * 24,
      }}>
        <div style={{
          opacity: jokeOp,
          transform: `translateY(${(1 - jokeOp) * 10}px)`,
          fontFamily: fonts.sans,
          fontSize: 42,
          color: "#FFFFFF",
          lineHeight: 1.35,
        }}>
          Paperclip leaves scorch marks.<br />
          We keep things cool.
        </div>
      </div>

      {/* Bar chart — big solid bars on the right */}
      <svg
        width={760}
        height={700}
        viewBox="0 0 760 700"
        style={{ position: "absolute", right: 140, top: "50%", transform: "translateY(-50%)" }}
      >
        {/* Paperclip bar */}
        <rect
          x="40"
          y={620 - pcH}
          width="320"
          height={pcH}
          rx="14"
          fill="#9F4DBF"
        />
        {progress > 0.05 && (
          <text
            x="200"
            y={620 - pcH - 28}
            fill="#9F4DBF"
            fontFamily={fonts.sans}
            fontSize="44"
            fontWeight="800"
            textAnchor="middle"
            style={{ fontVariantNumeric: "tabular-nums" }}
          >
            {pcCount}K
          </text>
        )}
        <text
          x="200"
          y="676"
          fill="#9F4DBF"
          fontFamily={fonts.mono}
          fontSize="28"
          fontWeight="700"
          textAnchor="middle"
        >
          Paperclip
        </text>

        {/* WUPHF bar — overlaps the Paperclip bar a bit */}
        <rect
          x="300"
          y={620 - wuphfH}
          width="320"
          height={wuphfH}
          rx="14"
          fill="#D4DB18"
        />
        {progress > 0.05 && (
          <text
            x="460"
            y={620 - wuphfH - 28}
            fill="#D4DB18"
            fontFamily={fonts.sans}
            fontSize="44"
            fontWeight="800"
            textAnchor="middle"
            style={{ fontVariantNumeric: "tabular-nums" }}
          >
            {wuphfCount}K
          </text>
        )}
        <text
          x="460"
          y="676"
          fill="#D4DB18"
          fontFamily={fonts.mono}
          fontSize="28"
          fontWeight="700"
          textAnchor="middle"
        >
          WUPHF
        </text>
      </svg>

    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// PANEL 2: Context graph — your company, in a graph
// ═══════════════════════════════════════════════════════════════
const Panel2Graph: React.FC<{ freezeAfter?: number }> = ({ freezeAfter }) => {
  const raw = useCurrentFrame();
  const frame = freezeAfter !== undefined ? Math.min(raw, freezeAfter) : raw;

  const headlineOp = interpolate(frame, [0, 18], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT });
  const jokeOp = interpolate(frame, [50, 68], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT });

  // Graph positioned on the right half, clear of the left-aligned headline.
  // All nodes use the pink/purple palette for visual cohesion.
  const nodes = [
    { x: 1340, y: 280, label: "My Startup", color: "#FFEBFC", size: 28, delay: 0 },
    { x: 1140, y: 420, label: "Sarah",      color: "#CF72D9", size: 22, delay: 6 },
    { x: 1340, y: 480, label: "Acme Corp",  color: "#CF72D9", size: 22, delay: 10 },
    { x: 1540, y: 420, label: "Q2 Launch",  color: "#CF72D9", size: 22, delay: 14 },
    { x: 1060, y: 620, label: "Roadmap",    color: "#9F4DBF", size: 18, delay: 22 },
    { x: 1240, y: 680, label: "$40k deal",  color: "#9F4DBF", size: 18, delay: 26 },
    { x: 1440, y: 680, label: "Contract",   color: "#9F4DBF", size: 18, delay: 30 },
    { x: 1620, y: 620, label: "Pricing",    color: "#9F4DBF", size: 18, delay: 34 },
  ];

  const edges = [
    [0, 1], [0, 2], [0, 3],
    [1, 4], [2, 5], [2, 6], [3, 7], [2, 3],
  ];

  return (
    <AbsoluteFill>
      {/* Left-aligned headline — vertically centered */}
      <div style={{
        position: "absolute",
        left: 140,
        top: "50%",
        transform: "translateY(-50%)",
        opacity: headlineOp,
      }}>
        <div style={{
          fontFamily: fonts.mono, fontSize: 20, color: "#9F4DBF",
          textTransform: "uppercase" as const, letterSpacing: 4, marginBottom: 20,
        }}>
          Memory · powered by Nex
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 96,
          fontWeight: 900,
          color: "#FFEBFC",
          lineHeight: 0.95,
          letterSpacing: -3,
          maxWidth: 680,
        }}>
          Your company's<br/>
          <span style={{ color: "#CF72D9" }}>context graph.</span>
        </div>
        {/* Joke — sits under the title */}
        <div style={{
          opacity: jokeOp,
          fontFamily: fonts.sans,
          fontSize: 32,
          color: "#FFFFFF",
          lineHeight: 1.35,
          maxWidth: 720,
          marginTop: 28,
        }}>
          Already better memory than your new hire.
        </div>
      </div>

      {/* Graph — right side, floating, vertically centered */}
      <svg
        width="1920" height="1080"
        style={{ position: "absolute", inset: 0 }}
      >
        <g transform="translate(0, 60)">
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
              stroke="#9F4DBF"
              strokeWidth="2.5"
              strokeOpacity={0.45}
            />
          );
        })}
        {nodes.map((n, i) => {
          const nodeScale = interpolate(frame, [n.delay, n.delay + 18], [0.6, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: ELEGANT,
          });
          const nodeOp = interpolate(frame, [n.delay, n.delay + 14], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: ELEGANT,
          });
          const pulse = Math.sin(frame * 0.08 + i) * 4 + n.size + 12;
          return (
            <g key={i} opacity={nodeOp} transform={`translate(${n.x}, ${n.y}) scale(${nodeScale})`}>
              <circle r={pulse} fill={n.color} fillOpacity="0.18" />
              <circle r={n.size} fill={n.color} />
              <text y={n.size + 28} textAnchor="middle" fill="#FFF" fontFamily={fonts.sans} fontSize="18" fontWeight="700">{n.label}</text>
            </g>
          );
        })}
        </g>
      </svg>

    </AbsoluteFill>
  );
};

// ═══════════════════════════════════════════════════════════════
// PANEL 3: Integrations — tools fly in from edges
// ═══════════════════════════════════════════════════════════════
const Panel3Integrations: React.FC = () => {
  const frame = useCurrentFrame();

  const headlineOp = interpolate(frame, [0, 18], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT });
  const jokeOp = interpolate(frame, [50, 68], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT });

  // Counter ticks up from 0 to 1000
  const counter = Math.floor(interpolate(frame, [20, 70], [0, 1000], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  }));

  // Real tag cloud — brand logos served from /public/logos (WorldVectorLogo)
  const tools: {
    label: string;
    logo: string;
    size: number;
    rotate: number;
    x: number;
    y: number;
    opacity: number;
  }[] = [
    { label: "Slack",    logo: "logos/slack.svg",    size: 36, rotate: -4,  x: 1340, y: 260, opacity: 1 },
    { label: "GitHub",   logo: "logos/github.svg",   size: 28, rotate: 6,   x: 1610, y: 220, opacity: 0.95 },
    { label: "Stripe",   logo: "logos/stripe.svg",   size: 42, rotate: -2,  x: 1450, y: 400, opacity: 1 },
    { label: "Gmail",    logo: "logos/gmail.svg",    size: 22, rotate: -9,  x: 1210, y: 320, opacity: 0.85 },
    { label: "Notion",   logo: "logos/notion.svg",   size: 30, rotate: 3,   x: 1700, y: 410, opacity: 0.9 },
    { label: "HubSpot",  logo: "logos/hubspot.svg",  size: 24, rotate: 10,  x: 1240, y: 500, opacity: 0.88 },
    { label: "Linear",   logo: "logos/linear.svg",   size: 34, rotate: -6,  x: 1390, y: 620, opacity: 1 },
    { label: "Apollo",   logo: "logos/apollo.svg",   size: 20, rotate: 8,   x: 1630, y: 560, opacity: 0.82 },
    { label: "Calendar", logo: "logos/calendar.svg", size: 26, rotate: -5,  x: 1560, y: 740, opacity: 0.9 },
    { label: "Intercom", logo: "logos/intercom.svg", size: 22, rotate: 4,   x: 1310, y: 760, opacity: 0.85 },
    { label: "Twilio",   logo: "logos/twilio.svg",   size: 18, rotate: -7,  x: 1720, y: 680, opacity: 0.78 },
  ];

  return (
    <AbsoluteFill>
      {/* Left-aligned headline — vertically centered */}
      <div style={{
        position: "absolute",
        left: 140,
        top: "50%",
        transform: "translateY(-50%)",
        opacity: headlineOp,
      }}>
        <div style={{
          fontFamily: fonts.mono, fontSize: 20, color: "#9F4DBF",
          textTransform: "uppercase" as const, letterSpacing: 4, marginBottom: 20,
        }}>
          Integrations
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 180,
          fontWeight: 900,
          color: "#CF72D9",
          lineHeight: 0.9,
          letterSpacing: -6,
          fontVariantNumeric: "tabular-nums" as const,
        }}>
          {counter.toLocaleString()}+
        </div>
        <div style={{
          fontFamily: fonts.sans,
          fontSize: 56,
          fontWeight: 700,
          color: "#FFEBFC",
          lineHeight: 1.1,
          letterSpacing: -1.5,
          marginTop: 56,
        }}>
          integrations.<br/>
          One click to connect.
        </div>
        {/* Joke — sits under the title */}
        <div style={{
          opacity: jokeOp,
          fontFamily: fonts.sans,
          fontSize: 32,
          color: "#FFFFFF",
          lineHeight: 1.35,
          marginTop: 28,
        }}>
          Fewer clicks than raising an IT ticket.
        </div>
      </div>

      {/* Tag cloud — vertically centered wrapper */}
      <div style={{
        position: "absolute",
        inset: 0,
        transform: "translateY(35px)",
      }}>
      {tools.map((tool, i) => {
        const delay = 20 + i * 3;
        const enter = interpolate(frame, [delay, delay + 14], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          easing: ELEGANT,
        });
        const scale = interpolate(frame, [delay, delay + 18], [0.85, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          easing: ELEGANT,
        });
        const drift = Math.sin((frame + i * 20) * 0.04) * 3;
        const iconSize = Math.round(tool.size * 0.9);
        const gap = Math.max(6, tool.size * 0.3);
        const padX = Math.max(12, tool.size * 0.6);
        const padY = Math.max(6, tool.size * 0.3);

        return (
          <div
            key={i}
            style={{
              position: "absolute",
              left: tool.x,
              top: tool.y + drift,
              opacity: enter * tool.opacity,
              transform: `translate(-50%, -50%) rotate(${tool.rotate}deg) scale(${scale})`,
              transformOrigin: "center",
              display: "flex",
              alignItems: "center",
              gap,
              padding: `${padY}px ${padX}px`,
              borderRadius: 999,
              backgroundColor: "#FFFFFF",
              fontFamily: fonts.sans,
              fontSize: tool.size,
              color: "#1d1c1d",
              fontWeight: 700,
              letterSpacing: -0.3,
              whiteSpace: "nowrap" as const,
              boxShadow: "0 2px 6px rgba(0,0,0,0.08), 0 10px 28px rgba(0,0,0,0.12)",
            }}
          >
            <img
              src={staticFile(tool.logo)}
              alt=""
              style={{
                width: iconSize,
                height: iconSize,
                flexShrink: 0,
                display: "block",
              }}
            />
            {tool.label}
          </div>
        );
      })}
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
    <AbsoluteFill style={{ backgroundColor: "#3B145D" }}>
      <DotGrid color="#FFFFFF" opacity={0.05} spacing={40} size={1.2} />

      <Sequence from={0} durationInFrames={sec(6)}>
        <Panel1Savings />
      </Sequence>

      <Sequence from={sec(6)} durationInFrames={PANEL3_FROM - sec(6)}>
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

// ═══════════════════════════════════════════════════════════════
// Brand marks — inline SVGs (simple-icons paths) rendered in pills
// ═══════════════════════════════════════════════════════════════

const brandSvg: React.CSSProperties = { width: "100%", height: "100%" };

const BrandGitHub: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
  </svg>
);

const BrandGmail: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M24 5.457v13.909c0 .904-.732 1.636-1.636 1.636h-3.819V11.73L12 16.64l-6.545-4.91v9.273H1.636A1.636 1.636 0 0 1 0 19.366V5.457c0-2.023 2.309-3.178 3.927-1.964L5.455 4.64 12 9.548l6.545-4.91 1.528-1.145C21.69 2.28 24 3.434 24 5.457z" />
  </svg>
);

const BrandSlack: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z" />
  </svg>
);

const BrandHubSpot: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M18.164 7.93V5.084a2.19 2.19 0 0 0 1.265-1.97v-.067A2.196 2.196 0 0 0 17.238.855h-.067a2.196 2.196 0 0 0-2.193 2.193v.067a2.19 2.19 0 0 0 1.265 1.97v2.85a6.22 6.22 0 0 0-2.963 1.302L5.52 3.24c.054-.2.085-.408.089-.62A2.53 2.53 0 1 0 3.077 5.15a2.53 2.53 0 0 0 1.22-.315l7.68 5.97a6.235 6.235 0 0 0 .094 7.033l-2.337 2.337a2.03 2.03 0 0 0-.58-.095 2.037 2.037 0 1 0 2.037 2.04 2.03 2.03 0 0 0-.095-.58l2.313-2.313a6.25 6.25 0 0 0 7.01-.37 6.25 6.25 0 0 0 1.48-8.63 6.257 6.257 0 0 0-3.733-2.297zm-1.004 9.387a3.21 3.21 0 1 1 0-6.42 3.21 3.21 0 0 1 0 6.42z" />
  </svg>
);

const BrandStripe: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M13.479 9.883c-1.626-.604-2.512-1.067-2.512-1.803 0-.622.511-.977 1.423-.977 1.667 0 3.379.642 4.558 1.22l.666-4.111c-.935-.446-2.847-1.177-5.49-1.177-1.87 0-3.425.489-4.536 1.401-1.155.957-1.757 2.334-1.757 4.003 0 3.023 1.847 4.312 4.847 5.403 1.936.688 2.579 1.18 2.579 1.938 0 .732-.629 1.155-1.766 1.155-1.403 0-3.716-.689-5.231-1.576l-.673 4.157c1.304.733 3.714 1.488 6.21 1.488 1.976 0 3.624-.467 4.738-1.355 1.24-.977 1.88-2.422 1.88-4.257 0-3.091-1.888-4.367-4.936-5.485z" />
  </svg>
);

const BrandCalendar: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M22.5 1.5h-21A1.5 1.5 0 0 0 0 3v18a1.5 1.5 0 0 0 1.5 1.5h21A1.5 1.5 0 0 0 24 21V3a1.5 1.5 0 0 0-1.5-1.5zM22.5 21h-21V7h21v14z" />
    <path d="M7 10h4v4H7zm6 0h4v4h-4zM7 16h4v4H7zm6 0h4v4h-4z" />
  </svg>
);

const BrandLinear: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M.403 13.796L10.204 23.597a12 12 0 0 1-9.8-9.8zM.004 9.434l14.562 14.562a12 12 0 0 0 2.403-.761L.765 7.031a12 12 0 0 0-.761 2.403zm1.79-5.26l18.032 18.032a12.074 12.074 0 0 0 1.953-1.602L3.396 2.22a12.074 12.074 0 0 0-1.602 1.953zM4.978 1.088C7.03-.206 9.427-.27 11.55.394l12.056 12.056c.663 2.122.6 4.52-.694 6.572L4.978 1.088z" />
  </svg>
);

const BrandNotion: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M4.459 4.208c.746.606 1.026.56 2.428.466l13.215-.793c.28 0 .047-.28-.046-.326L17.86 1.968c-.42-.326-.981-.7-2.055-.607L3.01 2.295c-.466.046-.56.28-.374.466zm.793 3.08v13.904c0 .747.373 1.027 1.214.98l14.523-.84c.841-.046.935-.56.935-1.167V6.354c0-.606-.233-.933-.748-.887l-15.177.887c-.56.047-.747.327-.747.933zm14.337.745c.093.42 0 .84-.42.888l-.7.14v10.264c-.608.327-1.168.513-1.635.513-.748 0-.935-.234-1.495-.933l-4.577-7.186v6.952L12.21 19s0 .84-1.168.84l-3.222.186c-.093-.186 0-.653.327-.746l.84-.233V9.854L7.822 9.76c-.094-.42.14-1.026.793-1.073l3.456-.233 4.764 7.279v-6.44l-1.215-.139c-.093-.514.28-.887.747-.933z" />
  </svg>
);

const BrandTwilio: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M12 0C5.388 0 0 5.388 0 12s5.388 12 12 12 12-5.388 12-12S18.612 0 12 0zm0 19.92A7.945 7.945 0 0 1 4.08 12 7.945 7.945 0 0 1 12 4.08 7.945 7.945 0 0 1 19.92 12 7.945 7.945 0 0 1 12 19.92zm4.584-11.4a2.1 2.1 0 1 1-4.2 0 2.1 2.1 0 0 1 4.2 0zm0 6.96a2.1 2.1 0 1 1-4.2 0 2.1 2.1 0 0 1 4.2 0zm-5.544-3.48a2.1 2.1 0 1 1-4.2 0 2.1 2.1 0 0 1 4.2 0zm5.544 0a2.1 2.1 0 1 1-4.2 0 2.1 2.1 0 0 1 4.2 0z" />
  </svg>
);

const BrandPostmark: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M12 0C5.383 0 0 5.383 0 12s5.383 12 12 12 12-5.383 12-12S18.617 0 12 0zm0 3.6a8.4 8.4 0 1 1 0 16.8 8.4 8.4 0 0 1 0-16.8zm-1.8 4.2v8.4h1.8v-3h1.2l2.1 3h2.1l-2.4-3.3c1.2-.3 1.8-1.2 1.8-2.4 0-1.65-1.05-2.7-2.7-2.7H10.2zm1.8 1.5h1.8c.75 0 1.2.45 1.2 1.2s-.45 1.2-1.2 1.2H12V9.3z" />
  </svg>
);

const BrandApollo: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm0 3c4.97 0 9 4.03 9 9s-4.03 9-9 9-9-4.03-9-9 4.03-9 9-9zm-1.05 3L6 18h2.25l1.05-2.85h5.4L15.75 18H18L13.05 6h-2.1zm1.05 2.4L14.1 13.2H9.9L12 8.4z" />
  </svg>
);

const BrandIntercom: React.FC = () => (
  <svg viewBox="0 0 24 24" style={brandSvg} fill="currentColor" aria-hidden="true">
    <path d="M21 0H3C1.35 0 0 1.35 0 3v18c0 1.65 1.35 3 3 3h18c1.65 0 3-1.35 3-3V3c0-1.65-1.35-3-3-3zm-5.4 4.2c0-.45.3-.75.75-.75s.75.3.75.75v9c0 .45-.3.75-.75.75s-.75-.3-.75-.75v-9zm-3.75-.3c0-.45.3-.75.75-.75s.75.3.75.75v10.2c0 .45-.3.75-.75.75s-.75-.3-.75-.75V3.9zm-3.75.3c0-.45.3-.75.75-.75s.75.3.75.75v9c0 .45-.3.75-.75.75s-.75-.3-.75-.75v-9zm-3.75 1.5c0-.45.3-.75.75-.75s.75.3.75.75v6c0 .45-.3.75-.75.75s-.75-.3-.75-.75v-6zm16.2 12.75c-.15.15-3.6 3.15-9.45 3.15s-9.3-3-9.45-3.15c-.3-.3-.3-.75-.15-1.05.3-.3.75-.3 1.05-.15.15.15 3.15 2.85 8.55 2.85s8.4-2.7 8.55-2.85c.3-.3.75-.15 1.05.15.15.3.15.75-.15 1.05zm-.9-6.75c0 .45-.3.75-.75.75s-.75-.3-.75-.75v-6c0-.45.3-.75.75-.75s.75.3.75.75v6z" />
  </svg>
);

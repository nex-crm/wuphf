import { AbsoluteFill, Easing, interpolate, useCurrentFrame } from "remotion";
import { colors, cyan, fonts } from "./theme";
import { NexSidebar } from "./components/NexSidebar";
import { PixelAvatar } from "./components/PixelAvatar";
import { WuphfLabel } from "./components/WuphfLabel";

// Channel view composition — window shell + sidebar + the Acme rollout
// conversation from the provided screenshots, in chronological order.

const NEX = {
  bg: "#FFFFFF",
  border: "#e9eaeb",
  borderLight: "#f2f2f3",
  text: "#28292a",
  textSecondary: "#686c6e",
  textTertiary: "#85898b",
};

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

// Agent palette
const AGENT_COLOR: Record<string, string> = {
  you: colors.human,
  ceo: colors.ceo,
  pm: colors.pm,
  designer: colors.designer,
  be: colors.be,
  cmo: colors.cmo,
  cro: colors.cro,
};
const AGENT_DISPLAY: Record<string, string> = {
  you: "You",
  ceo: "CEO",
  pm: "Product Manager",
  designer: "Designer",
  be: "Backend Engineer",
  cmo: "CMO",
  cro: "CRO",
};

// Role badge (colors.greenBg / colors.green) — matches real in-app style
const RoleBadge: React.FC<{ label: string; human?: boolean }> = ({ label, human }) => (
  <span
    style={{
      background: human ? "#e9eaeb" : colors.greenBg,
      color: human ? "#575a5c" : colors.green,
      padding: "1px 6px",
      borderRadius: 3,
      fontSize: 11,
      fontWeight: 500,
      fontFamily: fonts.sans,
    }}
  >
    {label}
  </span>
);

// @mention chip
const Mention: React.FC<{ slug: string }> = ({ slug }) => (
  <span
    style={{
      background: colors.mentionBg,
      color: colors.mentionText,
      padding: "0 5px",
      borderRadius: 3,
      fontWeight: 600,
      fontSize: "0.9em",
    }}
  >
    @{slug}
  </span>
);

// Split a body string, rendering @mentions inline as chips and paragraph breaks as blocks
const renderBody = (text: string) => {
  const lines = text.split(/\n\n+/);
  return lines.map((para, i) => (
    <div key={i} style={{ margin: i === 0 ? 0 : "8px 0 0" }}>
      {para.split(/(@[a-z]+)/).map((chunk, j) => {
        const m = /^@([a-z]+)$/.exec(chunk);
        if (m) return <Mention key={j} slug={m[1]} />;
        // Render **bold** segments
        return chunk.split(/(\*\*[^*]+\*\*)/).map((seg, k) => {
          const b = /^\*\*(.+)\*\*$/.exec(seg);
          if (b) return <strong key={k}>{b[1]}</strong>;
          return <span key={k}>{seg}</span>;
        });
      })}
    </div>
  ));
};

type Msg = {
  slug: string;
  ts: string;
  body: string;
  tokens?: string;
  replies?: number;
  reply?: boolean;
};

// Conversation in correct chronological order (from screenshots)
const MESSAGES: Msg[] = [
  {
    slug: "you",
    ts: "16:48",
    body:
      "Acme wants to start a WUPHF pilot next week. I need the onboarding plan, workspace defaults, approval rules, and follow-up sequence ready before their ops review.",
  },
  {
    slug: "ceo",
    ts: "16:50",
    body:
      "Got it. We need one coherent pilot package: @pm own onboarding scope, @be confirm broker and memory defaults, @designer tighten the first-run workspace, @cmo package the customer narrative.",
  },
  {
    slug: "pm",
    ts: "16:55",
    body:
      "Pilot scope is three workstreams: workspace setup, approval boundaries, and first-week operating cadence. I am turning each into owned tasks so nothing stays as a loose promise.",
  },
  {
    slug: "designer",
    ts: "16:59",
    body:
      "First-run polish pass: the customer should see channels, tasks, and skills as the operating system on day one. I am checking empty states, labels, and the handoff path from general to owned work.",
  },
  {
    slug: "be",
    ts: "17:08",
    body:
      "Runtime defaults are clear: Markdown memory for the pilot, Codex as the configured provider, no external actions until Acme approves CRM and email scopes.",
  },
  {
    slug: "cmo",
    ts: "17:12",
    body:
      "Customer-facing positioning: WUPHF gives Acme a visible AI team with receipts, ownership, and approval gates instead of a hidden prompt chain.",
  },
  {
    slug: "cro",
    ts: "17:16",
    body:
      "Sales note: Acme cares most about visibility and control. I am drafting the follow-up around auditability, weekly operating cadence, and the approval model.",
  },
  {
    slug: "ceo",
    ts: "17:22",
    body:
      "Good. Keep the handoff concrete: onboarding plan in review, CRM scope blocked until approval, customer follow-up done, and reusable Skills ready for the next pilot.",
  },
  {
    slug: "ceo",
    ts: "18:35",
    tokens: "130.9k",
    replies: 5,
    body:
      "#task-11 is approved and closed.\n\nFirst-run polish verdict for Acme:\n\nKeep this as the standard: Acme should see who owns what, what is waiting on approval, and what reusable motion WUPHF is running. Mystery is for magic shows and bad CRM exports.",
  },
  {
    slug: "designer",
    ts: "18:35",
    tokens: "133.7k",
    reply: true,
    body:
      "@ceo #task-11 complete. First-run workspace polish reads customer-ready for Acme: channel labels are understandable, task cards show ownership/status clearly, and Skill names feel reusable rather than internal.",
  },
  {
    slug: "cmo",
    ts: "18:35",
    tokens: "154.1k",
    reply: true,
    body:
      "Drafted the Acme pilot kickoff email. It frames WUPHF as visible AI teamwork with receipts: kickoff plan, first-week cadence, approval boundaries, and owner expectations are all covered in customer-facing language.",
  },
  {
    slug: "cro",
    ts: "18:36",
    tokens: "217.1k",
    reply: true,
    body:
      "The Acme CRM write-scope ask is ready commercially, but I could not send it because the live Gmail action provider is not configured. I blocked #task-13 and opened #task-25 to enable provider-backed Gmail send email. The customer-facing position remains: WUPHF operates read-only until Acme explicitly approves production CRM mutation scope.",
  },
  {
    slug: "be",
    ts: "18:37",
    tokens: "219.3k",
    reply: true,
    body:
      "The Gmail send path is blocked at provider configuration: the office action layer currently has no configured provider for Gmail actions. I opened the enablement task for One/Composio Gmail send provider and kept CRM production mutations read-only until Acme approves write scope.",
  },
  {
    slug: "ceo",
    ts: "18:37",
    tokens: "138.3k",
    reply: true,
    body:
      "@be task-34 is now the unblocker for task-25: discover and enable the smallest live Gmail send-email path through **One or Composio**.",
  },
];

export const WuphfChannel: React.FC = () => {
  const frame = useCurrentFrame();

  // Scroll timeline:
  //   0 → 60   : scroll down until the "5 replies" button is on screen
  //   60 → 108 : held (cursor moves in, clicks, thread expands)
  //   108 → 170: resume scroll revealing the replies
  const SCROLL_BEFORE = 620;
  const SCROLL_AFTER = 900;
  const scrollY = interpolate(
    frame,
    [5, 60, 108, 170],
    [0, SCROLL_BEFORE, SCROLL_BEFORE, SCROLL_AFTER],
    {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.inOut(Easing.cubic),
    },
  );

  // Thread expand
  const CLICK_FRAME = 80;
  const replyReveal = interpolate(frame, [CLICK_FRAME + 2, CLICK_FRAME + 26], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.inOut(Easing.cubic),
  });
  const threadOpen = replyReveal > 0.5;

  // Cursor — rendered INSIDE the CEO 18:35 message so it tracks the button
  // through scroll without needing to re-aim it. Path is a local offset from
  // the button's own coordinates.
  const cursorIn = interpolate(frame, [50, 70], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const cursorOut = interpolate(frame, [CLICK_FRAME + 10, CLICK_FRAME + 22], [1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const cursorOpacity = Math.min(cursorIn, cursorOut);
  // Start offset = far from the button, end = right on it
  const cursorOffsetX = 180 * (1 - cursorIn); // slide in from the right
  const cursorOffsetY = -60 * (1 - cursorIn); // slightly above, drops onto the button
  const clickPulse = interpolate(
    frame,
    [CLICK_FRAME - 2, CLICK_FRAME, CLICK_FRAME + 4, CLICK_FRAME + 8],
    [1, 0.86, 1, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );

  return (
    <AbsoluteFill
      style={{
        background: "#FFB3E6",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        position: "relative",
      }}
    >
      <WuphfLabel>Channels</WuphfLabel>
      <div
        style={{
          width: 1360,
          height: 1000,
          transform: "scale(0.8)",
          transformOrigin: "center",
          background: "#FFCFF1",
          borderRadius: 20,
          padding: 4,
          overflow: "hidden",
          boxShadow:
            "0 0 0 1px rgba(0,0,0,0.05), 0 40px 100px rgba(66, 26, 104, 0.35), 0 12px 32px rgba(0,0,0,0.12)",
          display: "flex",
          flexDirection: "column",
          willChange: "transform",
          backfaceVisibility: "hidden",
        }}
      >
        {/* Titlebar */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            height: 40,
            padding: "0 14px",
            background: "#FFCFF1",
            flexShrink: 0,
          }}
        >
          <div style={{ display: "flex", gap: 8 }}>
            <span style={trafficDot("#ff5f57")} />
            <span style={trafficDot("#febc2e")} />
            <span style={trafficDot("#28c840")} />
          </div>
          <span
            style={{
              flex: 1,
              textAlign: "center",
              fontFamily: fonts.sans,
              fontSize: 12,
              color: "#686c6e",
            }}
          >
            wuphf.app — # general
          </span>
          <span style={{ width: 54 }} />
        </div>

        {/* Rounded UI container */}
        <div
          style={{
            flex: 1,
            display: "flex",
            minHeight: 0,
            borderRadius: 16,
            overflow: "hidden",
          }}
        >
          <NexSidebar active={{ kind: "channel", slug: "general" }} />

          {/* Main channel area */}
          <div
            style={{
              flex: 1,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
              background: NEX.bg,
            }}
          >
            {/* Channel header */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                height: 56,
                padding: "0 24px",
                borderBottom: `1px solid ${NEX.border}`,
                background: "rgba(255,255,255,0.8)",
                flexShrink: 0,
                gap: 14,
              }}
            >
              <span style={{ fontSize: 16, fontWeight: 700, color: NEX.text, fontFamily: fonts.sans }}>
                # general
              </span>
              <span style={{ fontSize: 13, color: NEX.textSecondary, fontFamily: fonts.sans }}>
                Live command room for the WUPHF workspace rollout.
              </span>
            </div>

            {/* Runtime strip */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                height: 28,
                padding: "0 20px",
                borderBottom: `1px solid ${NEX.borderLight}`,
                flexShrink: 0,
                fontFamily: fonts.sans,
              }}
            >
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  padding: "2px 8px",
                  borderRadius: 999,
                  background: "#ffe4e0",
                  color: "#8c1727",
                  fontSize: 10,
                  fontWeight: 700,
                  textTransform: "uppercase" as const,
                  letterSpacing: "0.06em",
                }}
              >
                3 blocked
              </span>
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  padding: "2px 8px",
                  borderRadius: 999,
                  background: "#e9fbef",
                  color: "#0d5935",
                  fontSize: 10,
                  fontWeight: 700,
                  textTransform: "uppercase" as const,
                  letterSpacing: "0.06em",
                }}
              >
                3 active
              </span>
            </div>

            {/* Messages (scrolling) */}
            <div style={{ flex: 1, overflow: "hidden", position: "relative" }}>
              <div
                style={{
                  transform: `translateY(${-scrollY}px)`,
                  padding: "16px 24px 24px",
                  display: "flex",
                  flexDirection: "column",
                  gap: 14,
                  willChange: "transform",
                  backfaceVisibility: "hidden",
                }}
              >
                {/* "Today" separator */}
                <div style={{ display: "flex", justifyContent: "center", padding: "4px 0 10px" }}>
                  <span
                    style={{
                      background: "#f2f2f3",
                      color: NEX.textTertiary,
                      padding: "3px 12px",
                      borderRadius: 999,
                      fontSize: 11,
                      fontWeight: 600,
                      fontFamily: fonts.sans,
                    }}
                  >
                    Today
                  </span>
                </div>

                {MESSAGES.map((m, i) => {
                  const isHuman = m.slug === "you";
                  const color = AGENT_COLOR[m.slug] ?? colors.human;
                  const displayName = AGENT_DISPLAY[m.slug] ?? m.slug;
                  const roleLabel = isHuman
                    ? "human"
                    : m.slug === "ceo"
                    ? "Team Lead"
                    : m.slug === "pm"
                    ? "Product"
                    : m.slug === "designer"
                    ? "Design"
                    : m.slug === "be"
                    ? "Backend"
                    : m.slug === "cmo"
                    ? "Marketing"
                    : m.slug === "cro"
                    ? "Revenue"
                    : "";

                  // Reply rows use three nested wrappers so the rail can
                  // extend beyond the collapsible box without being clipped.
                  //   outer — position:relative (holds rail), marginTop handles gap
                  //   inner — maxHeight + overflow:hidden for the collapse animation
                  //   row   — flex layout of avatar + content
                  const outerStyle: React.CSSProperties = m.reply
                    ? { marginTop: (replyReveal - 1) * 14, position: "relative" }
                    : { position: "relative" };
                  const innerStyle: React.CSSProperties = m.reply
                    ? {
                        maxHeight: replyReveal * 800,
                        opacity: replyReveal,
                        overflow: "hidden",
                      }
                    : {};
                  return (
                    <div key={i} style={outerStyle}>
                      {m.reply && (
                        <div
                          style={{
                            position: "absolute",
                            left: 44,
                            top: -7,
                            bottom: -7,
                            width: 2,
                            borderRadius: 1,
                            background: "#cfd1d2",
                            opacity: 0.7 * replyReveal,
                            pointerEvents: "none",
                          }}
                        />
                      )}
                      <div style={innerStyle}>
                    <div
                      style={{
                        display: "flex",
                        gap: 12,
                        paddingLeft: m.reply ? 56 : 0,
                      }}
                    >
                      {/* Avatar tile */}
                      <div
                        style={{
                          width: m.reply ? 32 : 36,
                          height: m.reply ? 32 : 36,
                          padding: m.reply ? 5 : 6,
                          borderRadius: m.reply ? 7 : 8,
                          flexShrink: 0,
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "center",
                          background: "#f2f2f3",
                          boxSizing: "border-box",
                        }}
                      >
                        {isHuman ? (
                          <span style={{ fontSize: 12, fontWeight: 600, color: "#575a5c" }}>You</span>
                        ) : (
                          <PixelAvatar slug={m.slug} color={color} size={m.reply ? 22 : 24} />
                        )}
                      </div>

                      <div style={{ flex: 1, minWidth: 0, maxWidth: "70%" }}>
                        {/* Header row */}
                        <div style={{ display: "flex", alignItems: "baseline", gap: 8, marginBottom: 4 }}>
                          <span
                            style={{
                              fontFamily: fonts.sans,
                              fontSize: m.reply ? 13 : 14,
                              fontWeight: 700,
                              color: NEX.text,
                            }}
                          >
                            {displayName}
                          </span>
                          {roleLabel && <RoleBadge label={roleLabel} human={isHuman} />}
                          <span
                            style={{
                              fontFamily: fonts.sans,
                              fontSize: m.reply ? 11 : 12,
                              color: NEX.textTertiary,
                            }}
                          >
                            {m.ts}
                          </span>
                          {m.tokens && (
                            <span
                              style={{
                                fontFamily: fonts.mono,
                                fontSize: 11,
                                color: colors.accent,
                                background: "rgba(159,77,191,0.10)",
                                border: "1px solid rgba(159,77,191,0.18)",
                                padding: "1px 6px",
                                borderRadius: 999,
                                fontWeight: 600,
                              }}
                            >
                              {m.tokens} tok
                            </span>
                          )}
                        </div>
                        {/* Body */}
                        <div
                          style={{
                            fontFamily: fonts.sans,
                            fontSize: m.reply ? 14 : 15,
                            lineHeight: 1.55,
                            color: NEX.textSecondary,
                          }}
                        >
                          {renderBody(m.body)}
                        </div>
                        {/* Replies affordance — toggles to "Hide thread" after click */}
                        {m.replies && (
                          <div style={{ position: "relative", display: "inline-block", marginTop: 6 }}>
                          <div
                            style={{
                              display: "inline-flex",
                              alignItems: "center",
                              gap: 6,
                              padding: "3px 10px",
                              borderRadius: 999,
                              border: `1px solid ${NEX.border}`,
                              background: "#FFFFFF",
                              fontFamily: fonts.sans,
                              fontSize: 12,
                              color: colors.accent,
                              fontWeight: 600,
                              transform: `scale(${i === 8 ? clickPulse : 1})`,
                            }}
                          >
                            {threadOpen ? (
                              <>
                                <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ transform: "rotate(90deg)" }}>
                                  <path d="m9 18 6-6-6-6" />
                                </svg>
                                Hide thread
                              </>
                            ) : (
                              <>
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                                </svg>
                                {m.replies} replies
                              </>
                            )}
                          </div>
                          {/* Cursor — tracks the button through scroll */}
                          {i === 8 && cursorOpacity > 0 && (
                            <div
                              style={{
                                position: "absolute",
                                left: 30 + cursorOffsetX,
                                top: 12 + cursorOffsetY,
                                opacity: cursorOpacity,
                                transform: `scale(${clickPulse})`,
                                transformOrigin: "top left",
                                pointerEvents: "none",
                                filter: "drop-shadow(0 2px 6px rgba(0,0,0,0.25))",
                                zIndex: 60,
                              }}
                            >
                              <svg width="26" height="26" viewBox="0 0 24 24" fill="#1d1c1d" stroke="#FFFFFF" strokeWidth="1.6" strokeLinejoin="round">
                                <path d="M5 3 L5 19 L9 15 L11.6 21 L14.2 19.8 L11.6 14 L18 14 Z" />
                              </svg>
                            </div>
                          )}
                          </div>
                        )}
                      </div>
                    </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Composer */}
            <div
              style={{
                padding: "12px 24px 14px",
                borderTop: `1px solid ${NEX.border}`,
                background: NEX.bg,
                flexShrink: 0,
              }}
            >
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  background: NEX.bg,
                  border: `1px solid ${NEX.border}`,
                  borderRadius: 10,
                  padding: "10px 10px 10px 16px",
                }}
              >
                <span style={{ flex: 1, fontSize: 14, color: NEX.textTertiary, fontFamily: fonts.sans }}>
                  Message #general
                </span>
                <div
                  style={{
                    width: 32,
                    height: 32,
                    borderRadius: 8,
                    background: cyan[400],
                    color: "#0b3a44",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    flexShrink: 0,
                  }}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="m22 2-7 20-4-9-9-4Z" />
                    <path d="M22 2 11 13" />
                  </svg>
                </div>
              </div>
            </div>

            {/* Status bar */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 14,
                height: 36,
                padding: "0 20px",
                borderTop: `1px solid ${NEX.border}`,
                background: NEX.bg,
                fontFamily: fonts.mono,
                fontSize: 11,
                color: NEX.textTertiary,
                flexShrink: 0,
              }}
            >
              <span># general</span>
              <span>office</span>
              <span style={{ flex: 1 }} />
              <span>7 agents</span>
              <span>⚙ codex · gpt-5.4</span>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <span style={{ width: 7, height: 7, borderRadius: "50%", background: "#03a04c" }} />
                connected
              </span>
            </div>
          </div>
        </div>

      </div>
    </AbsoluteFill>
  );
};

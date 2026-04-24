import { AbsoluteFill, Easing, interpolate, useCurrentFrame } from "remotion";
import { colors, cyan, fonts, olive } from "./theme";
import { NexSidebar } from "./components/NexSidebar";
import { WikiFonts } from "./components/WikiFonts";
import { PixelAvatar } from "./components/PixelAvatar";
import { WuphfLabel } from "./components/WuphfLabel";

// New composition: scroll through a full Wiki article in the WUPHF web app.
// Content mirrors the "Finding Product-Market Fit in B2B" playbook article.

const NEX = {
  bg: "#FFFFFF",
  border: "#e9eaeb",
  borderLight: "#f2f2f3",
  text: "#28292a",
  textSecondary: "#686c6e",
  textTertiary: "#85898b",
  activeBg: cyan[400],
  activeFg: "#0b3a44",
};

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

const WIKI_NAV = [
  "Supply in a Marketplace",
  "Playbook: A PM's Guide to Influence",
  "Playbook: A Comprehensive Survey of Product Management",
  "Playbook: A Three-Step Framework for Solving Problems",
  "14 Habits of Highly Effective Product Managers",
  "25 Tactics to Accelerate AI Adoption at Your Company",
  "Case Study: Stripe's pre-Series-A PMF iteration",
  "Finding Product-Market Fit in B2B",       // ← active
  "A Founder's Guide to Community",
  "A Playbook for Fundraising",
  "60 Ideas to Boost Your Growth",
  "28 Ways to Grow Supply in a Marketplace",
  "A PM's Guide to Influence",
  "A Comprehensive Survey of Product Management",
];

const ACTIVE_PAGE = "Finding Product-Market Fit in B2B";

// Inline wikilink (dashed underline, olive)
const WikiLink: React.FC<{ children: React.ReactNode; broken?: boolean }> = ({
  children,
  broken,
}) => (
  <span
    style={{
      color: broken ? "#d1261a" : colors.wkWikilink,
      borderBottom: `1px dashed ${broken ? "#d1261a" : colors.wkWikilink}`,
      paddingBottom: 1,
    }}
  >
    {children}
  </span>
);

export const WuphfWikiScroll: React.FC = () => {
  const frame = useCurrentFrame();


  // Scroll: 6s total, ease-in-out, fills the whole composition
  const SCROLL_START = 5;
  const SCROLL_END = 175;
  const SCROLL_MAX = 2048;
  const scrollY = interpolate(
    frame,
    [SCROLL_START, SCROLL_END],
    [0, SCROLL_MAX],
    {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.inOut(Easing.cubic),
    },
  );

  const livePulse = 0.55 + 0.45 * Math.sin(frame * 0.18);

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
      <WikiFonts />
      <WuphfLabel>Wiki</WuphfLabel>
      {/* Window shell */}
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
            wuphf.app — Wiki
          </span>
          <span style={{ width: 54 }} />
        </div>

        {/* UI container with 16px rounding + clip */}
        <div
          style={{
            flex: 1,
            display: "flex",
            minHeight: 0,
            borderRadius: 16,
            overflow: "hidden",
          }}
        >
          <NexSidebar active={{ kind: "app", slug: "wiki" }} />

          {/* ═════ MAIN: Wiki app ═════ */}
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
              }}
            >
              <span style={{ fontSize: 16, fontWeight: 700, color: NEX.text, fontFamily: fonts.sans }}>
                Wiki
              </span>
            </div>

            {/* all quiet strip */}
            <div
              style={{
                height: 26,
                display: "flex",
                alignItems: "center",
                padding: "0 24px",
                fontFamily: fonts.sans,
                fontSize: 12,
                color: NEX.textTertiary,
                borderBottom: `1px solid ${NEX.borderLight}`,
                flexShrink: 0,
              }}
            >
              all quiet
            </div>

            {/* Top wiki tabs */}
            <nav
              style={{
                display: "flex",
                alignItems: "center",
                gap: 0,
                padding: "0 20px",
                height: 42,
                background: "#FFFFFF",
                borderBottom: `1px solid ${NEX.border}`,
                fontFamily: fonts.sans,
                flexShrink: 0,
              }}
            >
              {[
                { id: "wiki", label: "Wiki", active: true },
                { id: "notebooks", label: "Notebooks", active: false },
                { id: "reviews", label: "Reviews", active: false, badge: 3 },
              ].map((t) => (
                <div
                  key={t.id}
                  style={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 6,
                    height: "100%",
                    padding: "0 14px",
                    color: t.active ? NEX.text : NEX.textSecondary,
                    fontSize: 13,
                    fontWeight: 500,
                    borderBottom: `2px solid ${t.active ? colors.accent : "transparent"}`,
                  }}
                >
                  <span>{t.label}</span>
                  {t.badge && (
                    <span
                      style={{
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        minWidth: 18,
                        height: 18,
                        padding: "0 6px",
                        borderRadius: 9,
                        background: "#9F4DBF",
                        color: "#FFFFFF",
                        fontFamily: fonts.mono,
                        fontSize: 11,
                        fontWeight: 500,
                      }}
                    >
                      {t.badge}
                    </span>
                  )}
                </div>
              ))}
            </nav>

            {/* Wiki body: left nav + article + right rail */}
            <div style={{ flex: 1, display: "flex", minHeight: 0 }}>
              {/* Left wiki nav */}
              <div
                style={{
                  width: 240,
                  borderRight: `1px solid ${NEX.border}`,
                  padding: "16px 14px 0",
                  fontFamily: fonts.sans,
                  fontSize: 13,
                  color: NEX.textSecondary,
                  flexShrink: 0,
                  display: "flex",
                  flexDirection: "column",
                }}
              >
                <div
                  style={{
                    border: `1px solid ${NEX.border}`,
                    borderRadius: 8,
                    padding: "7px 10px",
                    fontSize: 12,
                    color: NEX.textTertiary,
                    marginBottom: 8,
                  }}
                >
                  Search wiki…
                </div>
                <div style={{ flex: 1, overflow: "hidden" }}>
                  {WIKI_NAV.map((item) => {
                    const active = item === ACTIVE_PAGE;
                    return (
                      <div
                        key={item}
                        style={{
                          padding: "6px 10px",
                          borderRadius: 5,
                          background: active ? olive[200] : "transparent",
                          color: active ? colors.text : colors.textSecondary,
                          fontWeight: active ? 500 : 400,
                          fontSize: 13,
                          lineHeight: 1.35,
                          marginBottom: 1,
                        }}
                      >
                        {item}
                      </div>
                    );
                  })}
                </div>
                {/* Live footer */}
                <div
                  style={{
                    borderTop: `1px solid ${NEX.border}`,
                    padding: "8px 6px",
                    display: "flex",
                    alignItems: "center",
                    gap: 8,
                    fontFamily: fonts.sans,
                    fontSize: 11,
                    color: NEX.textSecondary,
                  }}
                >
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: 5,
                      color: olive[500],
                      fontWeight: 700,
                      letterSpacing: "0.06em",
                      textTransform: "uppercase" as const,
                    }}
                  >
                    <span
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: olive[400],
                        opacity: livePulse,
                      }}
                    />
                    Live
                  </span>
                  <div style={{ width: 14, height: 14 }}>
                    <PixelAvatar slug="ceo" color={colors.ceo} size={14} />
                  </div>
                  <span style={{ color: colors.text, fontWeight: 600 }}>CEO</span>
                  <span>edited</span>
                  <span style={{ color: colors.text, fontWeight: 500 }}>Customer X</span>
                </div>
              </div>

              {/* Middle article column (scrolls) */}
              <div style={{ flex: 1, position: "relative", overflow: "hidden" }}>
                <div
                  style={{
                    position: "absolute",
                    inset: 0,
                    padding: "16px 0 0",
                    overflow: "hidden",
                  }}
                >
                  <div
                    style={{
                      transform: `translateY(${-scrollY}px)`,
                      padding: "0 40px 60px",
                      willChange: "transform",
                      backfaceVisibility: "hidden",
                    }}
                  >
                    {/* Compiled-skill banner */}
                    <div
                      style={{
                        background: "#f2f2f3",
                        borderLeft: "4px solid #aeb1b2",
                        borderRadius: 4,
                        padding: "10px 14px",
                        fontFamily: fonts.sans,
                        fontSize: 13,
                        color: NEX.textSecondary,
                        marginBottom: 18,
                      }}
                    >
                      <span style={{ fontWeight: 700, color: NEX.text }}>● Compiled skill:</span>{" "}
                      <span style={{ fontFamily: fonts.mono, color: NEX.text }}>
                        team/playbooks/.compiled/b2b-pmf/SKILL.md
                      </span>
                      <div style={{ marginTop: 2, fontSize: 12, color: NEX.textTertiary }}>
                        0 executions logged
                      </div>
                    </div>

                    {/* Action row: Article / Talk / Edit source / History / Raw markdown */}
                    <div
                      style={{
                        display: "flex",
                        gap: 20,
                        fontFamily: fonts.sans,
                        fontSize: 14,
                        color: NEX.textSecondary,
                        borderBottom: `1px solid ${NEX.border}`,
                        paddingBottom: 6,
                        marginBottom: 14,
                      }}
                    >
                      {[
                        { l: "Article", active: true },
                        { l: "Talk" },
                        { l: "Edit source" },
                        { l: "History" },
                        { l: "Raw markdown" },
                      ].map((b) => (
                        <span
                          key={b.l}
                          style={{
                            color: b.active ? colors.text : NEX.textSecondary,
                            fontWeight: b.active ? 600 : 400,
                            borderBottom: b.active ? `2px solid ${colors.accent}` : "none",
                            paddingBottom: 4,
                          }}
                        >
                          {b.l}
                        </span>
                      ))}
                      <span style={{ marginLeft: "auto", fontFamily: fonts.mono, fontSize: 12, color: NEX.textTertiary }}>
                        team
                      </span>
                    </div>

                    {/* Breadcrumb */}
                    <div
                      style={{
                        fontFamily: fonts.sans,
                        fontSize: 13,
                        color: NEX.textSecondary,
                        marginBottom: 12,
                        display: "flex",
                        gap: 6,
                      }}
                    >
                      <span>Team Wiki</span>
                      <span style={{ color: NEX.textTertiary }}>›</span>
                      <span>team</span>
                      <span style={{ color: NEX.textTertiary }}>›</span>
                      <span>playbooks</span>
                      <span style={{ color: NEX.textTertiary }}>›</span>
                      <span style={{ color: colors.text }}>Finding Product-Market Fit in B2B</span>
                    </div>

                    {/* Title */}
                    <div
                      style={{
                        fontFamily: fonts.display,
                        fontSize: 52,
                        fontWeight: 500,
                        lineHeight: 1.05,
                        letterSpacing: "-0.015em",
                        color: colors.text,
                        fontVariationSettings: '"opsz" 100',
                        margin: "0 0 4px",
                      }}
                    >
                      Finding Product-Market Fit in B2B
                    </div>
                    <div
                      style={{
                        fontFamily: fonts.serif,
                        fontStyle: "italic",
                        fontSize: 15,
                        color: NEX.textSecondary,
                        marginBottom: 14,
                      }}
                    >
                      From Team Wiki, your team's encyclopedia.
                    </div>
                    <div
                      style={{
                        borderTop: `1px solid ${colors.text}`,
                        borderBottom: `1px solid ${colors.text}`,
                        height: 3,
                        margin: "14px 0 24px",
                      }}
                    />

                    {/* Byline */}
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        fontFamily: fonts.sans,
                        fontSize: 13,
                        color: NEX.textSecondary,
                        marginBottom: 18,
                      }}
                    >
                      <div style={{ width: 22, height: 22 }}>
                        <PixelAvatar slug="archivist" color={colors.ai} size={22} />
                      </div>
                      <span>Last edited by</span>
                      <span style={{ color: colors.text, fontWeight: 600 }}>Archivist</span>
                      <span
                        style={{
                          background: olive[200],
                          color: colors.text,
                          padding: "2px 8px",
                          borderRadius: 4,
                          fontFamily: fonts.mono,
                          fontSize: 12,
                          display: "inline-flex",
                          alignItems: "center",
                          gap: 6,
                        }}
                      >
                        <span
                          style={{
                            width: 6,
                            height: 6,
                            borderRadius: "50%",
                            background: olive[400],
                            opacity: livePulse,
                          }}
                        />
                        1h ago
                      </span>
                      <span style={{ color: NEX.textTertiary }}>·</span>
                      <span>1 revisions</span>
                    </div>

                    {/* Hatnote */}
                    <div
                      style={{
                        fontFamily: fonts.serif,
                        fontStyle: "italic",
                        fontSize: 15,
                        color: NEX.textSecondary,
                        borderLeft: `2px solid ${NEX.border}`,
                        padding: "4px 12px",
                        marginBottom: 22,
                      }}
                    >
                      This article is auto-generated from team activity. See the commit history for the full trail.
                    </div>

                    {/* Lead paragraph */}
                    <div
                      style={{
                        fontFamily: fonts.serif,
                        fontSize: 18,
                        lineHeight: 1.72,
                        color: colors.text,
                        maxWidth: 820,
                        fontVariationSettings: '"opsz" 24',
                        marginBottom: 28,
                      }}
                    >
                      B2B PMF isn't a moment, it's a shape. A playbook for hunting it deliberately,
                      drawn from <WikiLink broken>Lenny's</WikiLink> guide and case studies across
                      our wiki.
                    </div>

                    <SectionHeading>Playbook</SectionHeading>

                    <BodyP>
                      <strong>Pick a narrow wedge.</strong> One job, for one buyer, in one industry —
                      before you try to generalize. <WikiLink broken>april-dunford</WikiLink> calls
                      this your 'best at what' dimension; most positioning fails because it chases
                      'best overall'.
                    </BodyP>
                    <BodyP>
                      <strong>Get to 10 paying customers who would be 'very disappointed' without
                      you.</strong>{" "}
                      This is <WikiLink broken>rahul-vohra</WikiLink>'s 40% PMF engine, refined at{" "}
                      <WikiLink broken>superhuman</WikiLink>. Below 40%, you are iterating blind. At
                      40%, you have a signal you can follow.
                    </BodyP>
                    <BodyP>
                      <strong>Only then look for the repeated buying pattern.</strong> Title, company
                      size, budget authority. <WikiLink broken>april-dunford</WikiLink> on B2B
                      positioning: if your customers can't explain what you do to their boss in one
                      sentence, your positioning is broken.
                    </BodyP>
                    <BodyP>
                      <strong>Write the ICP memo once you see the pattern.</strong> Who signs, who
                      uses, who benefits. <WikiLink broken>marty-cagan</WikiLink> argues most B2B
                      teams confuse the buyer, the user, and the champion — name all three
                      explicitly.
                    </BodyP>
                    <BodyP>
                      <strong>Expand horizontally before vertically.</strong> Same buyer, new
                      industry, before new buyer same industry. Horizontal compounds faster.
                    </BodyP>

                    <SectionHeading>When to use</SectionHeading>
                    <BodyP>
                      Seed-through-Series-A founders hunting for fit. Existing companies diagnosing
                      plateaued B2B revenue.
                    </BodyP>

                    <SectionHeading>Related</SectionHeading>
                    <ul style={{ padding: 0, margin: "0 0 18px 24px", fontFamily: fonts.serif, fontSize: 18, lineHeight: 1.72, color: colors.text }}>
                      {["rahul-vohra", "april-dunford", "marty-cagan", "superhuman", "stripe", "figma"].map(
                        (s) => (
                          <li key={s} style={{ marginBottom: 6 }}>
                            <WikiLink broken>{s}</WikiLink>
                          </li>
                        ),
                      )}
                    </ul>

                    <SectionHeading>Case Studies</SectionHeading>
                    <BodyP>
                      <WikiLink broken>b2b-pmf-stripe</WikiLink> — Stripe's pre-Series-A PMF
                      iteration — developers as the wedge.
                    </BodyP>
                    <BodyP>
                      <WikiLink broken>b2b-pmf-figma</WikiLink> — Figma's designer-to-team loop —
                      pending review.
                    </BodyP>

                    <SectionHeading>Source</SectionHeading>
                    <BodyP>
                      Derived from Lenny's newsletter:{" "}
                      <strong>a-guide-for-finding-product-market-fit-in-b2b.md</strong>. Ingested via
                      the team scanner and compiled by the archivist.
                    </BodyP>

                    {/* Execution log (collapsed) */}
                    <div
                      style={{
                        border: `1px solid ${NEX.border}`,
                        borderRadius: 6,
                        padding: "10px 14px",
                        marginTop: 16,
                        marginBottom: 28,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        fontFamily: fonts.sans,
                        fontSize: 12,
                        fontWeight: 700,
                        letterSpacing: "0.06em",
                        textTransform: "uppercase" as const,
                        color: NEX.textSecondary,
                      }}
                    >
                      <span>Execution log (0)</span>
                      <span>▸</span>
                    </div>

                    {/* Sources list */}
                    <SectionHeading>Sources</SectionHeading>
                    <ol
                      style={{
                        fontFamily: fonts.sans,
                        fontSize: 14,
                        color: NEX.textSecondary,
                        paddingLeft: 22,
                        margin: "8px 0 28px",
                        lineHeight: 1.7,
                      }}
                    >
                      <li style={{ display: "flex", alignItems: "center", gap: 8 }}>
                        <span>Edit 1 by pm</span>
                        <div style={{ width: 14, height: 14 }}>
                          <PixelAvatar slug="pm" color={colors.pm} size={14} />
                        </div>
                        <span style={{ color: colors.text, fontWeight: 500 }}>PM</span>
                        <span style={{ color: NEX.textTertiary }}>· mock0 · 2026-04-22</span>
                      </li>
                    </ol>

                    {/* Page footer */}
                    <div
                      style={{
                        borderTop: `1px solid ${NEX.border}`,
                        paddingTop: 14,
                        fontFamily: fonts.sans,
                        fontSize: 12,
                        color: NEX.textTertiary,
                        lineHeight: 1.6,
                      }}
                    >
                      This article was last edited on{" "}
                      <span style={{ color: colors.text, fontWeight: 500 }}>2026-04-22 at 13:18 UTC</span>{" "}
                      by <span style={{ color: colors.text, fontWeight: 500 }}>Archivist</span>. Text
                      is available under the terms of your local workspace, written by your agent
                      team.
                      <div style={{ display: "flex", gap: 18, marginTop: 10, flexWrap: "wrap", rowGap: 8 }}>
                        <span style={{ whiteSpace: "nowrap" }}>View git history</span>
                        <span style={{ whiteSpace: "nowrap" }}>Cite this page</span>
                        <span style={{ whiteSpace: "nowrap" }}>Download as markdown</span>
                        <span style={{ whiteSpace: "nowrap" }}>Export PDF</span>
                        <span style={{ whiteSpace: "nowrap" }}>Clone wiki locally</span>
                      </div>
                      <div
                        style={{
                          fontFamily: fonts.serif,
                          fontStyle: "italic",
                          fontSize: 12,
                          color: NEX.textTertiary,
                          marginTop: 12,
                        }}
                      >
                        Every edit is a real git commit authored by the named agent.{" "}
                        <code
                          style={{
                            fontFamily: fonts.mono,
                            background: "#f8f8f9",
                            border: `1px solid ${NEX.border}`,
                            padding: "1px 6px",
                            borderRadius: 3,
                            fontSize: 11,
                            color: colors.text,
                          }}
                        >
                          git log team/team/playbooks/b2b-pmf.md.md
                        </code>{" "}
                        shows the full trail.
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              {/* Right rail — fixed (doesn't scroll with article) */}
              <div
                style={{
                  width: 260,
                  padding: "16px 18px",
                  borderLeft: `1px solid ${NEX.border}`,
                  fontFamily: fonts.sans,
                  fontSize: 13,
                  color: NEX.textSecondary,
                  flexShrink: 0,
                }}
              >
                {/* Contents box */}
                <div
                  style={{
                    background: "#f8f8f9",
                    border: `1px solid ${NEX.border}`,
                    padding: 12,
                    borderRadius: 6,
                    marginBottom: 16,
                  }}
                >
                  <div
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      fontSize: 11,
                      fontWeight: 700,
                      textTransform: "uppercase" as const,
                      letterSpacing: "0.06em",
                      color: NEX.textTertiary,
                      marginBottom: 8,
                    }}
                  >
                    <span>Contents</span>
                    <span style={{ fontWeight: 400 }}>[hide]</span>
                  </div>
                  {[
                    { n: "1", label: "Playbook" },
                    { n: "2", label: "When to use" },
                    { n: "3", label: "Related" },
                    { n: "4", label: "Case Studies" },
                    { n: "5", label: "Source" },
                  ].map((t) => (
                    <div
                      key={t.n}
                      style={{
                        display: "flex",
                        gap: 8,
                        padding: "2px 0",
                        color: colors.text,
                        fontSize: 12.5,
                      }}
                    >
                      <span style={{ fontFamily: fonts.mono, color: NEX.textTertiary }}>
                        {t.n}
                      </span>
                      <span>{t.label}</span>
                    </div>
                  ))}
                </div>

                {/* Page stats */}
                <div
                  style={{
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: "uppercase" as const,
                    letterSpacing: "0.06em",
                    color: NEX.textTertiary,
                    marginBottom: 8,
                  }}
                >
                  Page stats
                </div>
                <div style={{ fontSize: 12, marginBottom: 16 }}>
                  {[
                    ["Revisions", "1"],
                    ["Contributors", "1 agents"],
                    ["Words", "275"],
                    ["Created", "2026-04-22"],
                    ["Last edit", "1h ago"],
                  ].map(([k, v]) => (
                    <div
                      key={k}
                      style={{
                        display: "flex",
                        justifyContent: "space-between",
                        padding: "2px 0",
                      }}
                    >
                      <span style={{ color: NEX.textTertiary }}>{k}</span>
                      <span style={{ color: colors.text, fontWeight: 500 }}>{v}</span>
                    </div>
                  ))}
                </div>

                {/* Cite this page */}
                <div
                  style={{
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: "uppercase" as const,
                    letterSpacing: "0.06em",
                    color: NEX.textTertiary,
                    marginBottom: 6,
                  }}
                >
                  Cite this page
                </div>
                <div
                  style={{
                    background: "#f8f8f9",
                    border: `1px solid ${NEX.border}`,
                    padding: "8px 10px",
                    borderRadius: 6,
                    fontFamily: fonts.mono,
                    fontSize: 11,
                    color: colors.text,
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                  }}
                >
                  <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    [[team/playbooks/b2b-pmf.md]]
                  </span>
                  <span
                    style={{
                      background: colors.text,
                      color: "#FFFFFF",
                      padding: "2px 8px",
                      borderRadius: 3,
                      fontSize: 10,
                      fontWeight: 500,
                    }}
                  >
                    copy
                  </span>
                </div>
                <div style={{ fontSize: 10, color: NEX.textTertiary, marginTop: 6, fontStyle: "italic" }}>
                  Paste this in any article to link here.
                </div>

                <div
                  style={{
                    marginTop: 18,
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: "uppercase" as const,
                    letterSpacing: "0.06em",
                    color: NEX.textTertiary,
                    display: "flex",
                    justifyContent: "space-between",
                  }}
                >
                  <span>Referenced by</span>
                  <span style={{ fontWeight: 500 }}>0</span>
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
              <span>wiki · office</span>
              <span style={{ flex: 1 }} />
              <span>5 agents</span>
              <span>⚙ claude-code</span>
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

const SectionHeading: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <h2
    style={{
      fontFamily: fonts.display,
      fontSize: 28,
      fontWeight: 500,
      lineHeight: 1.2,
      letterSpacing: "-0.01em",
      fontVariationSettings: '"opsz" 36',
      color: olive[500],
      borderBottom: `1px solid ${NEX.border}`,
      paddingBottom: 6,
      margin: "40px 0 14px",
    }}
  >
    {children}
  </h2>
);

const BodyP: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <p
    style={{
      fontFamily: fonts.serif,
      fontSize: 18,
      lineHeight: 1.72,
      color: colors.text,
      maxWidth: 820,
      fontVariationSettings: '"opsz" 24',
      margin: "0 0 18px",
    }}
  >
    {children}
  </p>
);

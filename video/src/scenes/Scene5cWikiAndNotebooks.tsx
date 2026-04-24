import { AbsoluteFill, Easing, interpolate, spring, useCurrentFrame } from "remotion";
import { FPS, colors, fonts, olive, sec } from "../theme";
import { AppShell } from "../components/AppShell";
import { PixelAvatar } from "../components/PixelAvatar";
import { WikiFonts } from "../components/WikiFonts";

const ELEGANT = Easing.bezier(0.25, 0.46, 0.45, 0.94);

const trafficDot = (color: string): React.CSSProperties => ({
  width: 13,
  height: 13,
  borderRadius: "50%",
  background: color,
  display: "inline-block",
});

// Scene 5c (new 2026-04-22): a cohesive wiki + notebooks beat.
// Flow: Notebooks tab (agent drafts) → tab switch → Wiki article with
// live edit log footer. Narration rides on this beat and pays off the
// "a team wiki your agents write" promise that /wiki delivers.

// Notebook tokens mirror web/src/styles/notebook.css (`--nb-*`)
const NB = {
  paper: "#FAFFE5",
  paperDark: "#E9DFC9",
  rule: "rgba(150, 150, 64, 0.07)",
  surface: "#FAF5E8",
  text: "#2A2721",
  textMuted: "#5B5547",
  textTertiary: "#8A8373",
  border: "#D9CEB5",
  amber: "#C78A1F",
  green: "#03a04c",
  display: "'Covered By Your Grace', 'Comic Sans MS', cursive",
  bodySerif: "'IBM Plex Serif', Georgia, serif",
  chrome: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
};

// One AgentShelf per agent, each with 3–5 mini cards (no snippets)
const NOTEBOOK_SHELVES = [
  {
    slug: "ceo",
    name: "CEO",
    color: colors.ceo,
    role: "Team lead",
    stats: "12 entries · 3 promoted · updated just now",
    entries: [
      { title: "Deal prep playbook", ts: "just now", status: "promoted" as const },
      { title: "Investor update notes", ts: "2h ago", status: "draft" as const },
      { title: "Q3 priorities", ts: "yesterday", status: "in-review" as const },
      { title: "Hiring loop debrief", ts: "2d ago", status: "promoted" as const },
    ],
  },
  {
    slug: "gtm",
    name: "GTM Lead",
    color: colors.gtm,
    role: "Go-to-market",
    stats: "8 entries · 2 promoted · updated 3m ago",
    entries: [
      { title: "Customer Acme — rough notes", ts: "3m ago", status: "in-review" as const },
      { title: "Cold email variants", ts: "1h ago", status: "draft" as const },
      { title: "Positioning brief", ts: "6h ago", status: "promoted" as const },
    ],
  },
  {
    slug: "eng",
    name: "Founding Engineer",
    color: colors.eng,
    role: "Engineering",
    stats: "17 entries · 4 promoted · updated 7m ago",
    entries: [
      { title: "Broker architecture", ts: "7m ago", status: "draft" as const },
      { title: "Worktree isolation", ts: "2h ago", status: "draft" as const },
      { title: "SSE fan-out benchmarks", ts: "1d ago", status: "in-review" as const },
      { title: "Retry policy v2", ts: "3d ago", status: "promoted" as const },
    ],
  },
  {
    slug: "pm",
    name: "Product Manager",
    color: colors.pm,
    role: "Product",
    stats: "6 entries · 1 promoted · updated 11m ago",
    entries: [
      { title: "Pricing objection from discovery", ts: "11m ago", status: "in-review" as const },
      { title: "Roadmap — Q4", ts: "yesterday", status: "draft" as const },
      { title: "Churn playbook", ts: "4d ago", status: "promoted" as const },
    ],
  },
];

const statusColor = (s: "draft" | "promoted" | "in-review") =>
  s === "promoted" ? NB.green : s === "in-review" ? NB.amber : "#6B6B6B";
const statusLabel = (s: "draft" | "promoted" | "in-review") =>
  s === "promoted" ? "Promoted" : s === "in-review" ? "In review" : "Draft";

// Scribbled shelf divider — matches the SVG in notebook.css:701
const SHELF_DIVIDER_SVG =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 400 6' preserveAspectRatio='none'><path d='M0 3.1 L 7 2.8 L 14 3.3 L 22 2.9 L 31 3.2 L 38 2.7 L 46 3.4 L 54 3.0 L 63 2.8 L 71 3.3 L 79 2.9 L 88 3.2 L 96 2.6 L 105 3.1 L 113 3.4 L 121 2.8 L 130 3.2 L 138 2.9 L 147 3.3 L 155 2.7 L 163 3.0 L 172 3.4 L 180 2.8 L 189 3.1 L 197 3.3 L 205 2.7 L 214 3.2 L 222 2.9 L 231 3.4 L 239 2.8 L 247 3.1 L 256 2.7 L 264 3.3 L 273 3.0 L 281 2.8 L 289 3.2 L 298 3.4 L 306 2.9 L 315 3.0 L 323 3.3 L 331 2.7 L 340 3.2 L 348 2.9 L 357 3.1 L 365 3.4 L 373 2.8 L 382 3.0 L 390 3.3 L 400 2.9' fill='none' stroke='%232A2721' stroke-width='1.2' stroke-linecap='round' stroke-linejoin='round'/></svg>",
  );

const EDIT_LOG: Array<{ who: string; slug: string; color: string; verb: string; target: string; ago: string }> = [
  { who: "CEO", slug: "ceo", color: colors.ceo, verb: "promoted", target: "Deal prep playbook", ago: "just now" },
  { who: "PM", slug: "pm", color: colors.pm, verb: "updated", target: "Playbook — Churn", ago: "3m ago" },
  { who: "Designer", slug: "designer", color: colors.designer, verb: "created", target: "Brand Voice", ago: "6m ago" },
  { who: "Eng", slug: "eng", color: colors.eng, verb: "wrote", target: "Broker architecture", ago: "13m ago" },
];

export const Scene5cWikiAndNotebooks: React.FC = () => {
  const frame = useCurrentFrame();

  // Window slides in from the bottom — same approach as Scene 4
  const slideY = interpolate(frame, [0, 18], [80, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: ELEGANT,
  });
  const uiOpacity = interpolate(frame, [0, 14], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  // Tail fade-out — keeps gradient bg continuous with Scene 6 (dark cut)
  const SCENE_DURATION_5C = sec(11);
  const exitFade = interpolate(
    frame,
    [SCENE_DURATION_5C - 18, SCENE_DURATION_5C],
    [1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: ELEGANT },
  );

  // Beat 1: show Notebooks tab with 4 entries (0 → 3.2s)
  // Beat 2: tab switch to Wiki + article reveals (3.2s → end)
  const switchFrame = sec(3.2);
  const onWiki = frame >= switchFrame;

  // Tab underline slides from "Notebooks" to "Wiki"
  const tabSlide = interpolate(frame, [switchFrame, switchFrame + 10], [70, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });
  const tabTransition = interpolate(frame, [switchFrame, switchFrame + 10], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Article springs in
  const articleOpacity = interpolate(frame, [switchFrame, switchFrame + 14], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const articleSlide = interpolate(frame, [switchFrame, switchFrame + 14], [16, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.cubic),
  });

  // Notebooks list slides out up / fades when switching
  const notebooksOpacity = interpolate(frame, [switchFrame, switchFrame + 10], [1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const notebooksSlide = interpolate(frame, [switchFrame, switchFrame + 10], [0, -14], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Amber pulse for live-edit dot.
  const pulse = 0.45 + 0.55 * (0.5 + 0.5 * Math.sin((frame / FPS) * Math.PI * 1.8));

  // Edit-log footer appears a beat after wiki reveals.
  const footerAppear = (i: number) =>
    interpolate(frame, [switchFrame + 12 + i * 4, switchFrame + 12 + i * 4 + 10], [0, 1], {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
    });

  // Shelves stagger in
  const shelfDelay = (i: number) => sec(0.2) + i * 8;

  return (
    <AbsoluteFill style={{
      opacity: uiOpacity,
      background: "radial-gradient(ellipse at top, #f3e8ff 0%, #ede2f7 40%, #d9c6ea 100%)",
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
    }}>
      {/* Window shell — full-window visible (no zoom), same aesthetic as Scene 4 */}
      <div style={{
        width: 1440,
        height: 880,
        opacity: exitFade,
        transform: `translateY(${slideY}px)`,
        background: "#ebe5f0",
        borderRadius: 20,
        padding: 4,
        overflow: "hidden",
        boxShadow: "0 0 0 1px rgba(0,0,0,0.05), 0 40px 100px rgba(66, 26, 104, 0.35), 0 12px 32px rgba(0,0,0,0.12)",
        display: "flex",
        flexDirection: "column",
      }}>
        {/* Inner UI */}
        <div style={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          minHeight: 0,
        }}>
          {/* Titlebar */}
          <div style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            height: 40,
            padding: "0 14px",
            background: "#ebe5f0",
            borderBottom: "1px solid rgba(0,0,0,0.06)",
            flexShrink: 0,
          }}>
            <div style={{ display: "flex", gap: 8 }}>
              <span style={trafficDot("#ff5f57")} />
              <span style={trafficDot("#febc2e")} />
              <span style={trafficDot("#28c840")} />
            </div>
            <span style={{
              flex: 1,
              textAlign: "center",
              fontFamily: fonts.sans,
              fontSize: 12,
              color: "#686c6e",
              letterSpacing: "0.01em",
            }}>
              wuphf.app — Wiki
            </span>
            <span style={{ width: 54 }} />
          </div>

          {/* UI container with 16px rounded corners, overflow clipped */}
          <div style={{
            flex: 1,
            position: "relative",
            borderRadius: 16,
            overflow: "hidden",
            minHeight: 0,
          }}>
    <AppShell
      activeView="app:wiki"
      liveAgents={["ceo", "gtm", "pm", "designer", "eng"]}
      headerTitle="Wiki"
      allQuiet
      footerSlot={
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 16,
            flexWrap: "nowrap",
            overflow: "hidden",
            whiteSpace: "nowrap",
          }}
        >
          <span
            style={{
              color: olive[500],
              fontWeight: 700,
              letterSpacing: 0.5,
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
              fontSize: 11,
            }}
          >
            <span
              style={{
                width: 7,
                height: 7,
                borderRadius: "50%",
                background: olive[400],
                opacity: pulse,
              }}
            />
            LIVE
          </span>
          {EDIT_LOG.map((e, i) => (
            <span
              key={i}
              style={{
                opacity: onWiki ? footerAppear(i) : 0,
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
                color: colors.textSecondary,
                fontFamily: fonts.sans,
                fontSize: 11.5,
              }}
            >
              <div style={{ width: 14, height: 14 }}>
                <PixelAvatar slug={e.slug} color={e.color} size={14} />
              </div>
              <span style={{ color: colors.text, fontWeight: 600 }}>{e.who}</span>
              <span>{e.verb}</span>
              <span style={{ color: colors.text, fontWeight: 500 }}>{e.target}</span>
              <span style={{ color: colors.textTertiary }}>{e.ago}</span>
            </span>
          ))}
        </div>
      }
    >
      <WikiFonts />

      {/* wiki-shell: top tab bar + body (matches web/src/styles/wiki-shell.css) */}
      <div style={{ position: "absolute", inset: 0, display: "flex", flexDirection: "column", minHeight: 0 }}>
        {/* Top tab bar — full-width 42px, matches .wiki-tabs */}
        <nav
          style={{
            display: "flex",
            alignItems: "center",
            gap: 0,
            padding: "0 20px",
            height: 42,
            background: "#FFFFFF",
            borderBottom: `1px solid ${colors.border}`,
            fontFamily: fonts.sans,
            flexShrink: 0,
            position: "relative",
          }}
        >
          {[
            { id: "wiki", label: "Wiki", active: onWiki },
            { id: "notebooks", label: "Notebooks", active: !onWiki },
            { id: "reviews", label: "Reviews", badge: 3 },
          ].map((t) => (
            <div
              key={t.id}
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
                height: "100%",
                padding: "0 14px",
                color: t.active ? colors.text : colors.textSecondary,
                fontSize: 13,
                fontWeight: 500,
                borderBottom: `2px solid ${t.active ? colors.accent : "transparent"}`,
                transition: "color 120ms, border-color 120ms",
              }}
            >
              <span style={{ letterSpacing: "0.005em" }}>{t.label}</span>
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
                    lineHeight: 1,
                  }}
                >
                  {t.badge}
                </span>
              )}
            </div>
          ))}
        </nav>

        {/* wiki-shell-body */}
        <div style={{ flex: 1, display: "flex", minHeight: 0 }}>
        {/* Wiki-only left sidebar (hidden on Notebooks view) */}
        {onWiki && (
        <div
          style={{
            width: 240,
            borderRight: `1px solid ${colors.border}`,
            padding: "20px 14px",
            fontFamily: fonts.sans,
            fontSize: 13,
            color: colors.textSecondary,
            flexShrink: 0,
            opacity: articleOpacity,
          }}
        >
            <div
              style={{
                border: `1px solid ${colors.border}`,
                borderRadius: 8,
                padding: "7px 10px",
                fontSize: 12,
                color: colors.textTertiary,
                marginBottom: 14,
              }}
            >
              Search wiki…
            </div>
            {[
              { label: "CUSTOMERS", items: [{ name: "Customer onboarding", active: true }] },
              { label: "PLAYBOOKS", items: [{ name: "Deal prep playbook", active: false, fresh: true }] },
              { label: "DECISIONS", items: [{ name: "Decisions log", active: false }] },
              { label: "ENG", items: [{ name: "Broker architecture", active: false }] },
              { label: "GTM", items: [{ name: "Positioning", active: false }] },
            ].map((g) => (
              <div key={g.label} style={{ marginBottom: 10 }}>
                <div
                  style={{
                    fontSize: 9,
                    fontWeight: 700,
                    textTransform: "uppercase",
                    letterSpacing: "0.09em",
                    color: colors.textTertiary,
                    padding: "0 6px 4px",
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                  }}
                >
                  {g.label}
                </div>
                {g.items.map((it: { name: string; active: boolean; fresh?: boolean }) => (
                  <div
                    key={it.name}
                    style={{
                      padding: "5px 8px",
                      borderRadius: 5,
                      background: it.active ? olive[200] : "transparent",
                      color: it.active ? colors.text : colors.textSecondary,
                      fontWeight: it.active ? 500 : 400,
                      fontSize: 13,
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                    }}
                  >
                    <span>{it.name}</span>
                    {it.fresh && (
                      <span
                        style={{
                          marginLeft: "auto",
                          background: olive[200],
                          color: olive[500],
                          fontSize: 9,
                          fontWeight: 700,
                          padding: "1px 5px",
                          borderRadius: 3,
                        }}
                      >
                        NEW
                      </span>
                    )}
                  </div>
                ))}
              </div>
            ))}
          </div>
        )}

        {/* ─── Main area ─── */}
        <div style={{ flex: 1, position: "relative", overflow: "hidden" }}>
          {/* Notebooks view — bookshelf / catalog (matches web Notebook.tsx) */}
          <div
            style={{
              position: "absolute",
              inset: 0,
              opacity: notebooksOpacity,
              transform: `translateY(${notebooksSlide}px)`,
              pointerEvents: onWiki ? "none" : "auto",
              background: NB.paper,
              backgroundImage: `repeating-linear-gradient(0deg, transparent 0, transparent 15px, ${NB.rule} 15px, ${NB.rule} 16px)`,
              color: NB.text,
              fontFamily: NB.bodySerif,
              overflow: "auto",
            }}
          >
            <div
              style={{
                maxWidth: 1100,
                margin: "32px auto",
                padding: "0 40px",
              }}
            >
              {/* Catalog header */}
              <div
                style={{
                  display: "flex",
                  alignItems: "baseline",
                  gap: 16,
                  flexWrap: "wrap",
                  marginBottom: 28,
                }}
              >
                <h1
                  style={{
                    fontFamily: NB.display,
                    fontSize: 36,
                    fontWeight: 600,
                    color: NB.text,
                    lineHeight: 1,
                    margin: 0,
                  }}
                >
                  Team notebooks
                </h1>
                <div
                  style={{
                    marginLeft: "auto",
                    fontFamily: NB.chrome,
                    fontSize: 12,
                    color: NB.text,
                    opacity: 0.55,
                  }}
                >
                  4 agents · 43 entries · 2 pending promotion
                </div>
              </div>

              {/* Shelves */}
              {NOTEBOOK_SHELVES.map((shelf, si) => {
                const d = shelfDelay(si);
                const o = interpolate(frame, [d, d + 10], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                });
                const tY = interpolate(frame, [d, d + 10], [10, 0], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                });
                const isLast = si === NOTEBOOK_SHELVES.length - 1;
                return (
                  <section
                    key={shelf.slug}
                    style={{
                      opacity: o,
                      transform: `translateY(${tY}px)`,
                      display: "grid",
                      gridTemplateColumns: "260px 1fr",
                      gap: 32,
                      padding: "20px 0 28px",
                      alignItems: "start",
                      backgroundRepeat: "no-repeat",
                      backgroundPosition: "left bottom",
                      backgroundSize: "100% 6px",
                      backgroundImage: isLast ? "none" : `url("${SHELF_DIVIDER_SVG}")`,
                    }}
                  >
                    {/* Shelf head */}
                    <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
                      <div style={{ width: 28, height: 28, flexShrink: 0 }}>
                        <PixelAvatar slug={shelf.slug} color={shelf.color} size={28} />
                      </div>
                      <div>
                        <div
                          style={{
                            fontFamily: NB.display,
                            fontSize: 24,
                            fontWeight: 600,
                            color: NB.text,
                            lineHeight: 1.1,
                          }}
                        >
                          {shelf.name}&rsquo;s notebook
                        </div>
                        <div
                          style={{
                            fontFamily: NB.chrome,
                            fontSize: 11,
                            color: NB.textMuted,
                          }}
                        >
                          {shelf.role}
                        </div>
                        <div
                          style={{
                            fontFamily: NB.chrome,
                            fontSize: 11,
                            color: NB.text,
                            opacity: 0.55,
                            marginTop: 4,
                          }}
                        >
                          {shelf.stats}
                        </div>
                      </div>
                    </div>

                    {/* Shelf cards — mini grid */}
                    <div
                      style={{
                        display: "grid",
                        gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))",
                        gap: 10,
                      }}
                    >
                      {shelf.entries.map((entry) => (
                        <div
                          key={entry.title}
                          style={{
                            background: "#ffffff",
                            outline: "0.5px solid rgba(0, 0, 0, 0.10)",
                            outlineOffset: -0.5,
                            borderRadius: 8,
                            padding: "12px 14px",
                            fontFamily: NB.bodySerif,
                            fontSize: 13,
                            lineHeight: 1.35,
                            color: NB.text,
                            minHeight: 64,
                            display: "flex",
                            flexDirection: "column",
                            justifyContent: "space-between",
                            gap: 6,
                            boxShadow:
                              "0 1px 1px rgba(42, 39, 33, 0.04), 0 1px 2px rgba(42, 39, 33, 0.04)",
                          }}
                        >
                          <span style={{ fontWeight: 500, color: NB.text }}>{entry.title}</span>
                          <span
                            style={{
                              display: "flex",
                              gap: 8,
                              alignItems: "center",
                              fontFamily: NB.chrome,
                              fontSize: 11,
                              color: "rgba(42, 39, 33, 0.55)",
                            }}
                          >
                            <span>{entry.ts}</span>
                            <span
                              style={{
                                fontWeight: 600,
                                color: statusColor(entry.status),
                              }}
                            >
                              {statusLabel(entry.status)}
                            </span>
                          </span>
                        </div>
                      ))}
                    </div>
                  </section>
                );
              })}
            </div>
          </div>

          {/* Wiki article view (shows after switch) */}
          <div
            style={{
              position: "absolute",
              inset: 0,
              padding: "24px 48px 40px",
              opacity: articleOpacity,
              transform: `translateY(${articleSlide}px)`,
              pointerEvents: !onWiki ? "none" : "auto",
              display: "flex",
              gap: 24,
            }}
          >
            {/* Article body */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div
                style={{
                  fontFamily: fonts.sans,
                  fontSize: 12,
                  color: colors.textSecondary,
                  marginBottom: 10,
                  display: "flex",
                  gap: 6,
                }}
              >
                <span>Team Wiki</span>
                <span style={{ color: colors.textTertiary }}>›</span>
                <span>team</span>
                <span style={{ color: colors.textTertiary }}>›</span>
                <span>playbooks</span>
                <span style={{ color: colors.textTertiary }}>›</span>
                <span style={{ color: colors.text }}>Deal prep playbook</span>
              </div>

              <div
                style={{
                  fontFamily: fonts.display,
                  fontSize: 56,
                  fontWeight: 500,
                  lineHeight: 1.05,
                  letterSpacing: -1,
                  color: colors.text,
                  fontVariationSettings: '"opsz" 100',
                  margin: "0 0 4px",
                }}
              >
                Deal prep playbook
              </div>
              <div
                style={{
                  fontFamily: fonts.serif,
                  fontStyle: "italic",
                  fontSize: 16,
                  color: colors.textSecondary,
                  marginBottom: 12,
                }}
              >
                From Team Wiki, your team&rsquo;s encyclopedia.
              </div>
              <div
                style={{
                  borderTop: `1px solid ${colors.text}`,
                  borderBottom: `1px solid ${colors.text}`,
                  height: 3,
                  margin: "10px 0 18px",
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
                  color: colors.textSecondary,
                  marginBottom: 20,
                }}
              >
                <div style={{ imageRendering: "pixelated" as const }}>
                  <PixelAvatar slug="ceo" color={colors.ceo} size={22} />
                </div>
                <span>Last edited by</span>
                <span style={{ color: colors.text, fontWeight: 600 }}>CEO</span>
                <span
                  style={{
                    background: olive[200],
                    color: colors.text,
                    padding: "2px 8px",
                    borderRadius: 4,
                    fontFamily: fonts.mono,
                    fontSize: 11,
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
                      opacity: pulse,
                    }}
                  />
                  just promoted
                </span>
                <span style={{ color: colors.textTertiary }}>·</span>
                <span>1 revision</span>
                <span style={{ color: colors.textTertiary }}>·</span>
                <span>approved by you</span>
              </div>

              <div
                style={{
                  fontFamily: fonts.serif,
                  fontStyle: "italic",
                  fontSize: 15,
                  color: colors.textSecondary,
                  borderLeft: `2px solid ${colors.border}`,
                  padding: "4px 12px",
                  marginBottom: 22,
                  maxWidth: 760,
                }}
              >
                This article was promoted from CEO&rsquo;s notebook. Every other agent can now read it.
              </div>

              <div
                style={{
                  fontFamily: fonts.serif,
                  fontSize: 20,
                  lineHeight: 1.68,
                  color: colors.text,
                  maxWidth: 760,
                  fontVariationSettings: '"opsz" 24',
                }}
              >
                <strong style={{ fontWeight: 700 }}>Deal prep</strong> pulls the company brief, past
                touchpoints, the buying committee, and likely objections. The prep note goes in the
                relevant{" "}
                <span
                  style={{
                    color: colors.wkWikilink,
                    borderBottom: `1px dashed ${colors.wkWikilink}`,
                  }}
                >
                  channel
                </span>{" "}
                ten minutes before every meeting, with a link to the upstream{" "}
                <span
                  style={{
                    color: colors.wkWikilink,
                    borderBottom: `1px dashed ${colors.wkWikilink}`,
                  }}
                >
                  Customer brief
                </span>
                .
              </div>
            </div>

            {/* Right rail: Contents + stats */}
            <div
              style={{
                width: 290,
                flexShrink: 0,
                fontFamily: fonts.sans,
                fontSize: 12,
                color: colors.textSecondary,
              }}
            >
              <div
                style={{
                  background: colors.bgSubtle,
                  border: `1px solid ${colors.border}`,
                  padding: 14,
                  borderRadius: 6,
                }}
              >
                <div
                  style={{
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: "uppercase",
                    letterSpacing: "0.09em",
                    color: colors.textTertiary,
                    marginBottom: 10,
                  }}
                >
                  Contents
                </div>
                {[
                  { num: "1", label: "Overview" },
                  { num: "2", label: "When to run" },
                  { num: "3", label: "Checklist" },
                  { num: "4", label: "Sources" },
                ].map((t) => (
                  <div
                    key={t.num}
                    style={{
                      display: "flex",
                      gap: 8,
                      padding: "3px 0",
                      color: colors.text,
                      fontSize: 12.5,
                    }}
                  >
                    <span style={{ fontFamily: fonts.mono, color: colors.textTertiary, minWidth: 22 }}>
                      {t.num}
                    </span>
                    <span>{t.label}</span>
                  </div>
                ))}
              </div>

              <div
                style={{
                  marginTop: 18,
                  padding: "0 4px",
                  display: "flex",
                  flexDirection: "column",
                  gap: 6,
                }}
              >
                <div
                  style={{
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: "uppercase",
                    letterSpacing: "0.09em",
                    color: colors.textTertiary,
                  }}
                >
                  Cite this page
                </div>
                <div
                  style={{
                    padding: "6px 10px",
                    background: colors.bgSubtle,
                    border: `1px solid ${colors.border}`,
                    borderRadius: 6,
                    fontFamily: fonts.mono,
                    fontSize: 11,
                    color: colors.text,
                  }}
                >
                  [[team/playbooks/deal-prep.md]]
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>
      </div>
    </AppShell>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};

// silence unused spring import for consistency with other scenes
void spring;

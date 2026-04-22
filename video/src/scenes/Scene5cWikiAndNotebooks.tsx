import { Easing, interpolate, spring, useCurrentFrame } from "remotion";
import { FPS, colors, fonts, olive, sec } from "../theme";
import { AppShell } from "../components/AppShell";
import { PixelAvatar } from "../components/PixelAvatar";
import { WikiFonts } from "../components/WikiFonts";

// Scene 5c (new 2026-04-22): a cohesive wiki + notebooks beat.
// Flow: Notebooks tab (agent drafts) → tab switch → Wiki article with
// live edit log footer. Narration rides on this beat and pays off the
// "a team wiki your agents write" promise that /wiki delivers.

const NOTEBOOK_ENTRIES = [
  {
    slug: "ceo",
    name: "CEO",
    color: colors.ceo,
    file: "team/playbooks/deal-prep.md",
    title: "Deal prep playbook",
    snippet: "Pull the company brief, past touchpoints, the buying committee, and likely…",
    state: "promoted" as const,
    ts: "just now",
  },
  {
    slug: "gtm",
    name: "GTM Lead",
    color: colors.gtm,
    file: "team/customers/acme-notes.md",
    title: "Customer Acme — rough notes",
    snippet: "14-truck fleet, dispatching pain, renewal hinges on SLA. Their CRO asked…",
    state: "pending" as const,
    ts: "3 min ago",
  },
  {
    slug: "eng",
    name: "Founding Engineer",
    color: colors.eng,
    file: "team/tech/broker-architecture.md",
    title: "Broker architecture",
    snippet: "Push-driven wake. Each agent in its own worktree. MCP tools per-agent so…",
    state: "draft" as const,
    ts: "7 min ago",
  },
  {
    slug: "pm",
    name: "Product Manager",
    color: colors.pm,
    file: "team/decisions/pricing-objection.md",
    title: "Pricing objection from discovery",
    snippet: "Per-seat tier flagged as a blocker. Bundled counter-offer in review…",
    state: "pending" as const,
    ts: "11 min ago",
  },
];

const EDIT_LOG: Array<{ who: string; slug: string; color: string; verb: string; target: string; ago: string }> = [
  { who: "CEO", slug: "ceo", color: colors.ceo, verb: "promoted", target: "Deal prep playbook", ago: "just now" },
  { who: "PM", slug: "pm", color: colors.pm, verb: "updated", target: "Playbook — Churn", ago: "3m ago" },
  { who: "Designer", slug: "designer", color: colors.designer, verb: "created", target: "Brand Voice", ago: "6m ago" },
  { who: "Eng", slug: "eng", color: colors.eng, verb: "wrote", target: "Broker architecture", ago: "13m ago" },
];

export const Scene5cWikiAndNotebooks: React.FC = () => {
  const frame = useCurrentFrame();

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

  // Draft cards stagger in.
  const entryDelay = (i: number) => sec(0.2) + i * 6;

  return (
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

      <div style={{ position: "absolute", inset: 0, display: "flex", minHeight: 0 }}>
        {/* Inner wiki-app left column with tabs */}
        <div
          style={{
            width: 240,
            borderRight: `1px solid ${colors.border}`,
            padding: "20px 0 20px",
            fontFamily: fonts.sans,
            fontSize: 13,
            color: colors.textSecondary,
            flexShrink: 0,
            position: "relative",
          }}
        >
          <div
            style={{
              padding: "0 18px 10px",
              display: "flex",
              gap: 18,
              borderBottom: `1px solid ${colors.borderLight}`,
              position: "relative",
            }}
          >
            <span
              style={{
                color: onWiki ? colors.text : colors.textSecondary,
                fontWeight: onWiki ? 600 : 400,
                paddingBottom: 8,
              }}
            >
              Wiki
            </span>
            <span
              style={{
                color: !onWiki ? colors.text : colors.textSecondary,
                fontWeight: !onWiki ? 600 : 400,
                paddingBottom: 8,
              }}
            >
              Notebooks
            </span>
            <span
              style={{
                color: colors.textSecondary,
                paddingBottom: 8,
                display: "inline-flex",
                alignItems: "center",
                gap: 4,
              }}
            >
              Reviews
              <span
                style={{
                  background: colors.accent,
                  color: colors.textBright,
                  borderRadius: 999,
                  fontSize: 10,
                  padding: "1px 6px",
                  fontWeight: 700,
                }}
              >
                3
              </span>
            </span>
            {/* Active underline */}
            <span
              style={{
                position: "absolute",
                left: 18,
                bottom: -1,
                height: 2,
                width: 34,
                background: colors.accent,
                transform: `translateX(${tabSlide}px)`,
              }}
            />
          </div>

          {/* Search box + left-nav items */}
          <div style={{ padding: "16px 14px 0" }}>
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
              {onWiki ? "Search wiki…" : "Search notebooks…"}
            </div>
            {onWiki
              ? [
                  { label: "CUSTOMERS", items: [{ name: "Customer onboarding", active: true }] },
                  { label: "PLAYBOOKS", items: [{ name: "Deal prep playbook", active: false, fresh: true }] },
                  { label: "DECISIONS", items: [{ name: "Decisions log", active: false }] },
                  { label: "ENG", items: [{ name: "Broker architecture", active: false }] },
                  { label: "GTM", items: [{ name: "Positioning", active: false }] },
                ].map((g) => (
                  <div key={g.label} style={{ marginBottom: 10, opacity: articleOpacity }}>
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
                ))
              : ["CEO", "GTM Lead", "Founding Engineer", "Product Manager", "Designer"].map((a) => (
                  <div
                    key={a}
                    style={{
                      padding: "5px 8px",
                      borderRadius: 5,
                      color: colors.textSecondary,
                      fontSize: 13,
                      fontWeight: a === "CEO" ? 500 : 400,
                      background: a === "CEO" ? olive[200] : "transparent",
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                    }}
                  >
                    <span>{a}</span>
                    <span style={{ marginLeft: "auto", color: colors.textTertiary, fontSize: 11 }}>
                      {a === "CEO" ? "4" : a === "GTM Lead" ? "7" : a === "Founding Engineer" ? "12" : "2"}
                    </span>
                  </div>
                ))}
          </div>
        </div>

        {/* ─── Main area ─── */}
        <div style={{ flex: 1, position: "relative", overflow: "hidden" }}>
          {/* Notebooks view */}
          <div
            style={{
              position: "absolute",
              inset: 0,
              padding: "24px 40px 40px",
              opacity: notebooksOpacity,
              transform: `translateY(${notebooksSlide}px)`,
              pointerEvents: onWiki ? "none" : "auto",
            }}
          >
            <div
              style={{
                fontFamily: fonts.display,
                fontSize: 40,
                fontWeight: 500,
                color: colors.text,
                letterSpacing: -0.5,
                fontVariationSettings: '"opsz" 48',
              }}
            >
              Notebooks
            </div>
            <div style={{ fontFamily: fonts.sans, fontSize: 14, color: colors.textSecondary, marginTop: 4, marginBottom: 22 }}>
              Your agents&rsquo; private scratch. Promote drafts to the team wiki.
            </div>

            {/* Stack of entry cards */}
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              {NOTEBOOK_ENTRIES.map((e, i) => {
                const d = entryDelay(i);
                const o = interpolate(frame, [d, d + 10], [0, 1], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                });
                const tY = interpolate(frame, [d, d + 10], [10, 0], {
                  extrapolateLeft: "clamp",
                  extrapolateRight: "clamp",
                });
                const badge =
                  e.state === "promoted"
                    ? { label: "Promoted", bg: colors.greenBg, fg: colors.green }
                    : e.state === "pending"
                    ? { label: "Pending review", bg: olive[200], fg: olive[500] }
                    : { label: "Draft", bg: colors.bgWarm, fg: colors.textSecondary };

                return (
                  <div
                    key={e.file}
                    style={{
                      opacity: o,
                      transform: `translateY(${tY}px)`,
                      background: colors.bgCard,
                      border: `1px solid ${colors.border}`,
                      borderRadius: 12,
                      padding: "14px 18px",
                      display: "flex",
                      gap: 14,
                      alignItems: "flex-start",
                    }}
                  >
                    <div
                      style={{
                        width: 34,
                        height: 34,
                        background: colors.bgWarm,
                        borderRadius: 7,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        flexShrink: 0,
                        overflow: "hidden",
                      }}
                    >
                      <PixelAvatar slug={e.slug} color={e.color} size={28} />
                    </div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
                        <span style={{ fontFamily: fonts.sans, fontSize: 15, fontWeight: 700, color: colors.text }}>
                          {e.title}
                        </span>
                        <span
                          style={{
                            background: colors.greenBg,
                            color: colors.green,
                            padding: "1px 6px",
                            borderRadius: 3,
                            fontSize: 10.5,
                            fontWeight: 500,
                          }}
                        >
                          {e.name.toLowerCase().includes("eng") ? "engineering" : e.slug === "gtm" ? "gtm" : e.slug}
                        </span>
                        <span style={{ color: colors.textTertiary, fontSize: 11 }}>{e.ts}</span>
                        <span
                          style={{
                            marginLeft: "auto",
                            background: badge.bg,
                            color: badge.fg,
                            padding: "2px 8px",
                            borderRadius: 999,
                            fontSize: 10.5,
                            fontWeight: 600,
                          }}
                        >
                          {badge.label}
                        </span>
                      </div>
                      <div
                        style={{
                          fontFamily: fonts.mono,
                          fontSize: 11,
                          color: colors.textTertiary,
                          marginTop: 3,
                          marginBottom: 6,
                        }}
                      >
                        {e.file}
                      </div>
                      <div style={{ fontFamily: fonts.sans, fontSize: 13.5, color: colors.textSecondary, lineHeight: 1.5 }}>
                        {e.snippet}
                      </div>
                    </div>
                  </div>
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
    </AppShell>
  );
};

// silence unused spring import for consistency with other scenes
void spring;

import { AbsoluteFill } from "remotion";
import { fonts } from "./theme";
import { NexSidebar } from "./components/NexSidebar";
import { WuphfLabel } from "./components/WuphfLabel";

// Static Tasks-app composition (Kanban board).

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

// Status badge palette — mirrors the real task-board styles
const STATUS = {
  "in progress": { bg: "#F0F68B", fg: "#5c5000" },
  open: { bg: "#F0F68B", fg: "#5c5000" },
  review: { bg: "#F0F68B", fg: "#5c5000" },
  blocked: { bg: "#ffdec8", fg: "#8c4713" },
  done: { bg: "#c8eecd", fg: "#1e5f2a" },
  "won't do": { bg: "#eeeff0", fg: "#7a7d7f", line: true as const },
} as const;

type StatusKey = keyof typeof STATUS;

// Mention chip — purple
const Mention: React.FC<{ slug: string }> = ({ slug }) => (
  <span
    style={{
      background: "#ffebfc",
      color: "#9F4DBF",
      padding: "0 5px",
      borderRadius: 3,
      fontWeight: 600,
      fontSize: 11,
      fontFamily: fonts.sans,
    }}
  >
    @{slug}
  </span>
);

type Card = {
  title: string;
  status: StatusKey;
  owner: string;
  channel?: string;
  when: string;
};

type Column = { label: string; count: number; cards: Card[] };

const COLUMNS: Column[] = [
  {
    label: "IN PROGRESS",
    count: 3,
    cards: [
      {
        title: "Finalize Acme pilot onboarding plan",
        status: "in progress",
        owner: "pm",
        channel: "general",
        when: "1h ago",
      },
      {
        title: "Wire staging broker + SSE for Acme",
        status: "in progress",
        owner: "be",
        channel: "launch",
        when: "34m ago",
      },
      {
        title: "Draft Acme weekly cadence template",
        status: "in progress",
        owner: "pm",
        channel: "customer-rollout",
        when: "2h ago",
      },
    ],
  },
  {
    label: "OPEN",
    count: 3,
    cards: [
      {
        title: "Write Acme pilot kickoff email",
        status: "open",
        owner: "cmo",
        channel: "general",
        when: "1h ago",
      },
      {
        title: "Audit landing-page mobile polish",
        status: "open",
        owner: "designer",
        channel: "product-polish",
        when: "18m ago",
      },
      {
        title: "Price card A/B test plan",
        status: "open",
        owner: "pm",
        channel: "launch",
        when: "yesterday",
      },
    ],
  },
  {
    label: "REVIEW",
    count: 2,
    cards: [
      {
        title: "Review first-run workspace polish",
        status: "review",
        owner: "designer",
        channel: "general",
        when: "1h ago",
      },
      {
        title: "Broker retry policy v2 spec",
        status: "review",
        owner: "be",
        channel: "launch",
        when: "40m ago",
      },
    ],
  },
  {
    label: "BLOCKED",
    count: 2,
    cards: [
      {
        title: "Confirm CRM write scopes with Acme",
        status: "blocked",
        owner: "fe",
        channel: "general",
        when: "1h ago",
      },
      {
        title: "Intercom webhook signing secret",
        status: "blocked",
        owner: "fe",
        channel: "customer-rollout",
        when: "3h ago",
      },
    ],
  },
  {
    label: "DONE",
    count: 2,
    cards: [
      {
        title: "Draft stakeholder follow-up for Acme ops lead",
        status: "done",
        owner: "cro",
        channel: "general",
        when: "1h ago",
      },
      {
        title: "Publish v0.7 changelog",
        status: "done",
        owner: "pm",
        channel: "product-polish",
        when: "yesterday",
      },
    ],
  },
  {
    label: "WON'T DO",
    count: 1,
    cards: [
      {
        title: "Enable automatic writes before approval",
        status: "won't do",
        owner: "ceo",
        channel: "general",
        when: "1h ago",
      },
    ],
  },
];

const StatusPill: React.FC<{ status: StatusKey }> = ({ status }) => {
  const s = STATUS[status];
  return (
    <span
      style={{
        background: s.bg,
        color: s.fg,
        padding: "2px 8px",
        borderRadius: 4,
        fontFamily: fonts.sans,
        fontSize: 11,
        fontWeight: 500,
        textDecoration: "line" in s && s.line ? "line-through" : "none",
      }}
    >
      {status}
    </span>
  );
};

export const WuphfTasks: React.FC = () => {
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
      <WuphfLabel>Tasks</WuphfLabel>
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
            wuphf.app — Tasks
          </span>
          <span style={{ width: 54 }} />
        </div>

        <div
          style={{
            flex: 1,
            display: "flex",
            minHeight: 0,
            borderRadius: 16,
            overflow: "hidden",
          }}
        >
          <NexSidebar active={{ kind: "app", slug: "tasks" }} />

          {/* Main Tasks area */}
          <div
            style={{
              flex: 1,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
              background: NEX.bg,
            }}
          >
            {/* Header */}
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
              <span
                style={{
                  fontSize: 16,
                  fontWeight: 700,
                  color: NEX.text,
                  fontFamily: fonts.sans,
                }}
              >
                Tasks
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
                  fontFamily: fonts.sans,
                }}
              >
                2 blocked
              </span>
            </div>

            {/* Section header */}
            <div
              style={{
                padding: "22px 32px 14px",
                borderBottom: `1px solid ${NEX.border}`,
                fontFamily: fonts.sans,
                flexShrink: 0,
              }}
            >
              <div style={{ fontSize: 18, fontWeight: 700, color: NEX.text, marginBottom: 4 }}>
                Office tasks
              </div>
              <div style={{ fontSize: 13, color: NEX.textSecondary }}>
                All active lanes across the office. Drag a card to move it.
              </div>
            </div>

            {/* Kanban board — columns are fixed-width so cards stay legible;
                the rightmost column clips off the edge like a real board. */}
            <div
              style={{
                flex: 1,
                overflow: "hidden",
                padding: "20px 24px 24px",
                display: "grid",
                gridTemplateColumns: `repeat(${COLUMNS.length}, 250px)`,
                gap: 16,
                fontFamily: fonts.sans,
              }}
            >
              {COLUMNS.map((col) => (
                <div key={col.label} style={{ display: "flex", flexDirection: "column", gap: 10, minWidth: 0 }}>
                  {/* Column header */}
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      padding: "0 4px 8px",
                      borderBottom: `2px solid ${NEX.border}`,
                    }}
                  >
                    <span
                      style={{
                        fontSize: 11,
                        fontWeight: 700,
                        textTransform: "uppercase" as const,
                        letterSpacing: "0.08em",
                        color: NEX.textTertiary,
                      }}
                    >
                      {col.label}
                    </span>
                    <span
                      style={{
                        fontFamily: fonts.mono,
                        fontSize: 10,
                        padding: "1px 7px",
                        borderRadius: 999,
                        background: "#f2f2f3",
                        color: NEX.textSecondary,
                      }}
                    >
                      {col.count}
                    </span>
                  </div>

                  {/* Cards */}
                  {col.cards.map((c, i) => (
                    <div
                      key={i}
                      style={{
                        background: NEX.bg,
                        border: `1px solid ${NEX.border}`,
                        borderRadius: 10,
                        padding: "14px 14px 12px",
                        display: "flex",
                        flexDirection: "column",
                        gap: 8,
                        minWidth: 0,
                      }}
                    >
                      <div
                        style={{
                          fontSize: 14,
                          fontWeight: 700,
                          color: NEX.text,
                          lineHeight: 1.3,
                          textDecoration: c.status === "won't do" ? "line-through" : "none",
                        }}
                      >
                        {c.title}
                      </div>
                      <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                        <StatusPill status={c.status} />
                        <Mention slug={c.owner} />
                        {c.channel && (
                          <span style={{ fontSize: 12, color: NEX.textSecondary }}>#{c.channel}</span>
                        )}
                      </div>
                      <div style={{ fontSize: 12, color: NEX.textTertiary, marginTop: 2 }}>
                        {c.when}
                      </div>
                    </div>
                  ))}
                </div>
              ))}
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
              <span>tasks</span>
              <span>office</span>
              <span style={{ flex: 1 }} />
              <span>7 agents</span>
              <span>⚙ codex · gpt-5.4</span>
              <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <span
                  style={{
                    width: 7,
                    height: 7,
                    borderRadius: "50%",
                    background: "#03a04c",
                  }}
                />
                connected
              </span>
            </div>
          </div>
        </div>
      </div>
    </AbsoluteFill>
  );
};

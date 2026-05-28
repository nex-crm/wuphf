// Shared visual primitive for every integration card. Card-level files
// (OpenClawCard, HermesCard, TelegramCard, ...) compose this — they own
// per-card state and submit logic; CardShell owns the chrome.

export type CardStatus = "connected" | "available" | "unconfigured" | "warning";

interface CardShellProps {
  icon: React.ReactNode;
  title: string;
  status: CardStatus;
  statusLabel: string;
  body: React.ReactNode;
}

function statusColor(status: CardStatus): string {
  switch (status) {
    case "connected":
      return "#16a34a";
    case "available":
      return "#3b82f6";
    case "warning":
      return "#d97706";
    default:
      return "var(--text-tertiary)";
  }
}

export function CardShell({
  icon,
  title,
  status,
  statusLabel,
  body,
}: CardShellProps) {
  return (
    <section
      style={{
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: 16,
        marginBottom: 16,
        background: "var(--bg-card)",
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span style={{ fontSize: 22 }}>{icon}</span>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>{title}</h3>
        </div>
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
            fontSize: 11,
            color: statusColor(status),
            fontWeight: 600,
            textTransform: "uppercase",
            letterSpacing: 0.4,
          }}
        >
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: statusColor(status),
              display: "inline-block",
            }}
          />
          {statusLabel}
        </span>
      </header>
      <div>{body}</div>
    </section>
  );
}

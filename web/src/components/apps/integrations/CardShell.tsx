// CardShell is the visual primitive every integration card composes. The
// shape is a three-column grid: a "channel strip" rail with the icon, the
// title + body, and a top-aligned status cell. Card-level files own state
// and submit logic — CardShell owns chrome and layout.
//
// Status semantics map to operator.css's LED + status classes:
//   connected  → green LED · CONNECTED
//   available  → blue LED  · AVAILABLE
//   warning    → amber LED · ATTENTION
//   unconfigured → grey LED · NOT CONFIGURED

export type CardStatus = "connected" | "available" | "unconfigured" | "warning";

interface CardShellProps {
  icon: React.ReactNode;
  title: string;
  status: CardStatus;
  statusLabel: string;
  body: React.ReactNode;
}

const STATUS_TONE: Record<CardStatus, "on" | "warn" | "off" | "info"> = {
  connected: "on",
  available: "info",
  warning: "warn",
  unconfigured: "off",
};

export function CardShell({
  icon,
  title,
  status,
  statusLabel,
  body,
}: CardShellProps) {
  const tone = STATUS_TONE[status];
  return (
    <article className="op-card">
      <div className="op-card-rail" aria-hidden="true">
        {icon}
      </div>
      <div className="op-card-body">
        <div className="op-card-title-row">
          <h4 className="op-card-title">{title}</h4>
        </div>
        {body}
      </div>
      <div className="op-card-status-cell">
        <span className={`op-status is-${tone}`}>
          <span className={`op-led is-${tone}`} />
          {statusLabel}
        </span>
      </div>
    </article>
  );
}

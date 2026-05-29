import { NavArrowLeft, NavArrowRight } from "iconoir-react";
import type { ReactElement, ReactNode } from "react";

import type { IntegrationStatus, IntegrationStatusTone } from "./types";

// CardShell is the chrome primitive used by both list rows and detail
// headers. Two exports:
//
//   - IntegrationListRow: clickable row showing logo + title + summary +
//     status. Used in the default list view of the Integrations app.
//
//   - IntegrationDetailHeader: back button + logo + title + status, sits
//     above the descriptor's render() body in the detail view.
//
// Status semantics map to operator.css's LED + status classes:
//   connected   → green LED · CONNECTED
//   available   → blue LED  · AVAILABLE
//   warning     → amber LED · ATTENTION
//   unconfigured → grey LED · NOT CONFIGURED

const TONE_CLASS: Record<IntegrationStatusTone, "on" | "warn" | "off" | "info"> =
  {
    connected: "on",
    available: "info",
    warning: "warn",
    unconfigured: "off",
  };

function StatusPill({ status }: { status: IntegrationStatus }) {
  const tone = TONE_CLASS[status.tone];
  return (
    <span className={`op-status is-${tone}`}>
      <span className={`op-led is-${tone}`} />
      {status.label}
    </span>
  );
}

interface IntegrationListRowProps {
  logo: ReactElement;
  title: string;
  summary: string;
  status: IntegrationStatus;
  onOpen: () => void;
}

export function IntegrationListRow({
  logo,
  title,
  summary,
  status,
  onOpen,
}: IntegrationListRowProps) {
  return (
    <button
      type="button"
      className="op-list-row"
      onClick={onOpen}
      aria-label={`Open ${title} integration settings`}
    >
      <div className="op-list-row-logo" aria-hidden="true">
        {logo}
      </div>
      <div className="op-list-row-body">
        <div className="op-list-row-title">{title}</div>
        <div className="op-list-row-summary">{summary}</div>
      </div>
      <div className="op-list-row-status">
        <StatusPill status={status} />
      </div>
      <NavArrowRight
        className="op-list-row-chevron"
        width={18}
        height={18}
        aria-hidden="true"
      />
    </button>
  );
}

interface IntegrationDetailHeaderProps {
  logo: ReactElement;
  title: string;
  summary: string;
  status: IntegrationStatus;
  onBack: () => void;
}

export function IntegrationDetailHeader({
  logo,
  title,
  summary,
  status,
  onBack,
}: IntegrationDetailHeaderProps) {
  return (
    <header className="op-detail-header">
      <button
        type="button"
        className="op-detail-back"
        onClick={onBack}
        aria-label="Back to integrations list"
      >
        <NavArrowLeft width={16} height={16} aria-hidden="true" />
        <span>All integrations</span>
      </button>
      <div className="op-detail-id">
        <div className="op-detail-id-logo" aria-hidden="true">
          {logo}
        </div>
        <div className="op-detail-id-text">
          <h2 className="op-detail-title">{title}</h2>
          <p className="op-detail-summary">{summary}</p>
        </div>
        <div className="op-detail-status">
          <StatusPill status={status} />
        </div>
      </div>
    </header>
  );
}

// Legacy export kept for any caller that still composes the inline-card
// shape. New code uses IntegrationListRow + IntegrationDetailHeader.
export type CardStatus = IntegrationStatusTone;

interface CardShellProps {
  icon: ReactNode;
  title: string;
  status: CardStatus;
  statusLabel: string;
  body: ReactNode;
}

export function CardShell({
  icon,
  title,
  status,
  statusLabel,
  body,
}: CardShellProps) {
  return (
    <section className="op-card">
      <div className="op-card-rail" aria-hidden="true">
        {icon}
      </div>
      <div className="op-card-body">
        <div className="op-card-title-row">
          <h3 className="op-card-title">{title}</h3>
        </div>
        {body}
      </div>
      <div className="op-card-status-cell">
        <StatusPill status={{ tone: status, label: statusLabel }} />
      </div>
    </section>
  );
}

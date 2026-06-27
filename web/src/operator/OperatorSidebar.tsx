// The operator shell's left nav — deliberately small: five surfaces, a call
// CTA, and the operator's identity. No agents, channels, skills, or wiki
// vocabulary; this is the whole product.

import {
  BookOpen,
  MessageSquare,
  PhoneCall,
  Plug,
  Plus,
  Settings,
  Workflow,
  type LucideIcon,
} from "lucide-react";

export type OperatorSurface =
  | "chats"
  | "tools"
  | "knowledge"
  | "integrations"
  | "settings";

interface NavDef {
  id: OperatorSurface;
  label: string;
  icon: LucideIcon;
  count?: number;
}

const NAV: readonly NavDef[] = [
  { id: "chats", label: "Chats", icon: MessageSquare, count: 1 },
  { id: "tools", label: "Internal tools", icon: Workflow, count: 3 },
  { id: "knowledge", label: "Knowledge", icon: BookOpen },
  { id: "integrations", label: "Integrations", icon: Plug },
  { id: "settings", label: "Settings", icon: Settings },
];

interface OperatorSidebarProps {
  active: OperatorSurface;
  onSelect: (surface: OperatorSurface) => void;
  onStartCall: () => void;
  onBuild: () => void;
}

export function OperatorSidebar({
  active,
  onSelect,
  onStartCall,
  onBuild,
}: OperatorSidebarProps) {
  return (
    <aside className="opr-sidebar">
      <div className="opr-brand">
        <img
          className="opr-brand-mark"
          src="/favicon.svg"
          alt="wuphf"
          width={27}
          height={27}
        />
        <div>
          <div className="opr-brand-name">wuphf</div>
        </div>
      </div>

      <nav className="opr-nav" aria-label="Primary">
        {NAV.map((item) => (
          // Icons mirror the manual's sparse instrumentation controls: small,
          // utilitarian, and subordinate to the label.
          <button
            key={item.id}
            type="button"
            className={`opr-nav-item${item.id === active ? " is-active" : ""}`}
            onClick={() => onSelect(item.id)}
          >
            <span className="opr-nav-icon" aria-hidden>
              <item.icon size={15} strokeWidth={1.8} />
            </span>
            {item.label}
            {item.count ? (
              <span className="opr-nav-count">{item.count}</span>
            ) : null}
          </button>
        ))}
      </nav>

      <div className="opr-sidebar-spacer" />

      <button
        type="button"
        className="opr-btn opr-btn-primary opr-build-cta"
        onClick={onBuild}
      >
        <Plus size={14} strokeWidth={1.9} aria-hidden />
        Build a tool
      </button>
      <button
        type="button"
        className="opr-call-cta opr-call-cta-secondary"
        onClick={onStartCall}
      >
        <PhoneCall size={14} strokeWidth={1.9} aria-hidden />
        Teach your workflow to Nex
      </button>

      <div className="opr-user">
        <div className="opr-user-avatar">M</div>
        <div>
          <div className="opr-user-name">Maya</div>
          <div className="opr-user-role">RevOps · your workspace</div>
        </div>
      </div>
    </aside>
  );
}

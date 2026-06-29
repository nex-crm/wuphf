// The operator shell's left nav — deliberately small: Apps and Settings, a
// build/call CTA, and the operator's identity. No agents, channels, skills, or
// wiki vocabulary; this is the whole product. Everything else (chat,
// integrations, knowledge, data) lives as tabs inside an app.

import {
  type LucideIcon,
  PhoneCall,
  Plus,
  Settings,
  Workflow,
} from "lucide-react";

// Chats, Knowledge, and Integrations are no longer top-level surfaces: they live
// as tabs inside a Work Tool, because each is scoped to the tool that uses it
// (an integration is connected once but shown under the tools that use it; a
// tool's chat only affects that tool). The shell is just Work Tools + Settings.
export type OperatorSurface = "tools" | "settings";

interface NavDef {
  id: OperatorSurface;
  label: string;
  icon: LucideIcon;
  count?: number;
}

const NAV: readonly NavDef[] = [
  { id: "tools", label: "Apps", icon: Workflow, count: 3 },
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
            <span className="opr-nav-icon" aria-hidden={true}>
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
        <Plus size={14} strokeWidth={1.9} aria-hidden={true} />
        Build a tool
      </button>
      <button
        type="button"
        className="opr-call-cta opr-call-cta-secondary"
        onClick={onStartCall}
      >
        <PhoneCall size={14} strokeWidth={1.9} aria-hidden={true} />
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

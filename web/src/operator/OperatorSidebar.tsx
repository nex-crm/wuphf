// The operator shell's left nav — deliberately small: Agents and Settings, a
// build/call CTA, and the operator's identity. Under Agents sits a collapsible
// rail of every agent so the operator can jump straight to one; everything else
// (chat, artifacts, integrations, knowledge) lives as tabs inside an agent.

import { useState } from "react";
import {
  ChevronDown,
  type LucideIcon,
  PhoneCall,
  Plus,
  Settings,
  Workflow,
} from "lucide-react";

export type OperatorSurface = "tools" | "settings";

/** One agent in the sidebar rail (real or mock; name overrides pre-applied). */
export interface SidebarAgent {
  id: string;
  name: string;
  glyph: string;
  building?: boolean;
}

interface NavDef {
  id: OperatorSurface;
  label: string;
  icon: LucideIcon;
}

const NAV: readonly NavDef[] = [
  { id: "tools", label: "Agents", icon: Workflow },
  { id: "settings", label: "Settings", icon: Settings },
];

interface OperatorSidebarProps {
  active: OperatorSurface;
  onSelect: (surface: OperatorSurface) => void;
  onStartCall: () => void;
  onBuild: () => void;
  agents?: SidebarAgent[];
  activeAgentId?: string | null;
  onOpenAgent?: (id: string) => void;
}

export function OperatorSidebar({
  active,
  onSelect,
  onStartCall,
  onBuild,
  agents = [],
  activeAgentId,
  onOpenAgent,
}: OperatorSidebarProps) {
  // The rail is collapsible so a long roster doesn't crowd the nav.
  const [agentsOpen, setAgentsOpen] = useState(true);

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
          <div key={item.id}>
            <div
              className={`opr-nav-item${item.id === active ? " is-active" : ""}`}
            >
              <button
                type="button"
                className="opr-nav-item-main"
                onClick={() => onSelect(item.id)}
              >
                <span className="opr-nav-icon" aria-hidden={true}>
                  <item.icon size={15} strokeWidth={1.8} />
                </span>
                {item.label}
                {item.id === "tools" && agents.length > 0 ? (
                  <span className="opr-nav-count">{agents.length}</span>
                ) : null}
              </button>
              {item.id === "tools" && agents.length > 0 ? (
                <button
                  type="button"
                  className="opr-nav-caret"
                  onClick={() => setAgentsOpen((v) => !v)}
                  aria-expanded={agentsOpen}
                  aria-label={agentsOpen ? "Collapse agents" : "Expand agents"}
                >
                  <ChevronDown
                    size={13}
                    strokeWidth={2}
                    aria-hidden={true}
                    className={agentsOpen ? "is-open" : ""}
                  />
                </button>
              ) : null}
            </div>

            {item.id === "tools" && agentsOpen && agents.length > 0 ? (
              <div className="opr-agent-rail" aria-label="Your agents">
                {agents.map((a) => (
                  <button
                    key={a.id}
                    type="button"
                    className={`opr-agent-rail-item${
                      a.id === activeAgentId ? " is-active" : ""
                    }`}
                    onClick={() => onOpenAgent?.(a.id)}
                  >
                    <span className="opr-agent-rail-glyph" aria-hidden={true}>
                      {a.glyph}
                    </span>
                    <span className="opr-agent-rail-name">{a.name}</span>
                    {a.building ? (
                      <span
                        className="opr-led opr-led-draft"
                        title="Building"
                      />
                    ) : null}
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        ))}
      </nav>

      <div className="opr-sidebar-spacer" />

      <button
        type="button"
        className="opr-btn opr-btn-primary opr-build-cta"
        onClick={onBuild}
      >
        <Plus size={14} strokeWidth={1.9} aria-hidden={true} />
        Build an agent
      </button>
      <button
        type="button"
        className="opr-call-cta opr-call-cta-secondary"
        onClick={onStartCall}
      >
        <PhoneCall size={14} strokeWidth={1.9} aria-hidden={true} />
        Demo workflow to Nex
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

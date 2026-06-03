/**
 * TourMockupSidebar — a decorative, non-interactive mock of WUPHF's real left
 * sidebar, used inside the office tour to show the office filling in.
 *
 * It is NOT the real sidebar and is never wired to routing or stores: the
 * whole subtree is `aria-hidden` so assistive tech reads the slide copy, not
 * a duplicate navigation tree. Visually it mirrors `Sidebar.tsx`: a workspace
 * logo header, an Agents group (CEO orchestrator + reporting specialists), and
 * a Channels group, using the same `#`/`@` language and `PixelAvatar` portraits
 * the real sidebar uses.
 *
 * Slides drive two pieces of state:
 *   - `activeAgent` highlights one agent row (e.g. the slide that explains
 *     what an agent is lights up `@analyst`).
 *   - `litRows` is the set of row slugs that have "completed", each earning a
 *     green tick (`--green` on `--green-bg`) so the user watches their office
 *     come to life one item at a time across slides.
 *
 * All colors come from design tokens; the component renders in nex, nex-dark,
 * and noir-gold without overrides.
 */

import { useMemo } from "react";

import { PixelAvatar } from "../../ui/PixelAvatar";

/** One channel row in the mock Channels group. */
interface MockChannel {
  /** Stable slug used for `litRows` matching and React keys. */
  slug: string;
  /** Channel name shown after the `#`. */
  name: string;
}

/** One agent row in the mock Agents group. */
interface MockAgent {
  /**
   * Stable slug used for `litRows` matching, `activeAgent` matching, the `@`
   * handle, the PixelAvatar seed, and React keys.
   */
  slug: string;
  /** Display name shown above the role line. */
  name: string;
  /** One-line role, mirrors the real sidebar's secondary agent text. */
  role: string;
  /** CEO renders as the orchestrator; everyone else reports to it. */
  isCeo?: boolean;
}

interface TourMockupSidebarProps {
  /** Agent slug to render in the active/highlighted state, if any. */
  activeAgent?: string;
  /**
   * Row slugs (channel or agent) that have completed and should show a green
   * tick. Defaults to none so the office starts empty and fills in per slide.
   */
  litRows?: string[];
}

/**
 * Mock agents mirror the real CEO-plus-specialists shape from `AgentList`:
 * the CEO is the orchestrator and specialists report to it.
 */
const MOCK_AGENTS: MockAgent[] = [
  { slug: "ceo", name: "CEO", role: "Orchestrator", isCeo: true },
  { slug: "analyst", name: "Analyst", role: "Watches the funnel" },
  { slug: "revops", name: "RevOps", role: "Keeps the CRM clean" },
];

/** Mock channels mirror the `#general` default plus one work channel. */
const MOCK_CHANNELS: MockChannel[] = [
  { slug: "general", name: "general" },
  { slug: "revops", name: "revops" },
];

/** Green completion tick. Decorative; the parent subtree is already hidden. */
function CompletionTick() {
  return (
    <span className="tour-mockup-tick" aria-hidden="true">
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path
          d="M1.5 5.2 L4 7.5 L8.5 2.5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}

export function TourMockupSidebar({
  activeAgent,
  litRows = [],
}: TourMockupSidebarProps) {
  const lit = useMemo(() => new Set(litRows), [litRows]);

  return (
    <aside className="tour-mockup-sidebar" aria-hidden="true">
      <header className="tour-mockup-header">
        <span className="tour-mockup-logo">WUPHF</span>
      </header>

      <section className="tour-mockup-section">
        <div className="tour-mockup-section-title">Agents</div>
        <div className="tour-mockup-agents">
          {MOCK_AGENTS.map((agent) => {
            const isActive =
              activeAgent === `@${agent.slug}` || activeAgent === agent.slug;
            const isLit = lit.has(agent.slug);
            return (
              <div
                key={agent.slug}
                className={`tour-mockup-agent${isActive ? " is-active" : ""}${
                  agent.isCeo ? " is-ceo" : " is-specialist"
                }`}
              >
                <span className="tour-mockup-agent-avatar">
                  <PixelAvatar slug={agent.slug} size={22} />
                </span>
                <span className="tour-mockup-agent-body">
                  <span className="tour-mockup-agent-name">
                    {agent.name}
                    <span className="tour-mockup-agent-handle">
                      @{agent.slug}
                    </span>
                  </span>
                  <span className="tour-mockup-agent-role">{agent.role}</span>
                </span>
                {isLit ? (
                  <CompletionTick />
                ) : (
                  <span className="tour-mockup-status-dot" aria-hidden="true" />
                )}
              </div>
            );
          })}
        </div>
      </section>

      <section className="tour-mockup-section">
        <div className="tour-mockup-section-title">Channels</div>
        <div className="tour-mockup-channels">
          {MOCK_CHANNELS.map((channel) => {
            const isLit = lit.has(channel.slug);
            return (
              <div key={channel.slug} className="tour-mockup-channel">
                <span className="tour-mockup-channel-hash">#</span>
                <span className="tour-mockup-channel-name">{channel.name}</span>
                {isLit ? <CompletionTick /> : null}
              </div>
            );
          })}
        </div>
      </section>
    </aside>
  );
}

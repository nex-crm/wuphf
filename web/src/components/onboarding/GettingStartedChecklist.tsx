/**
 * GettingStartedChecklist — the dismissible "Settle into your office" panel
 * shown to onboarded-but-not-settled users.
 *
 * Renders the five dormant DefaultChecklist items (pick_team, second_key,
 * github_repo, github_star, discord) with WUPHF copy overlaid on top of the
 * server's id+done wire shape. Each row carries a --green tick once done plus
 * an action: internal nav (pick_team -> team/agents view, second_key ->
 * Settings runtimes) or an external link (GitHub repo, GitHub star, Discord).
 * Taking an action marks the item done. The panel hides itself entirely when
 * the user has dismissed it or every item is complete.
 *
 * Copy is verbatim from docs/specs/office-onboarding-uplift.md section 6.
 */

import type { ReactNode } from "react";
import {
  ArrowRight,
  Check,
  Discord,
  Github,
  OpenNewWindow,
} from "iconoir-react";

import "../../styles/getting-started-checklist.css";
import { router } from "../../lib/router";
import { Button } from "../ui/Button";
import { CollapsibleSection } from "../ui/CollapsibleSection";
import { useGettingStartedChecklist } from "./useGettingStartedChecklist";

/** External destinations. Kept as named constants per the build contract. */
export const GITHUB_REPO_URL = "https://github.com/nex-crm/wuphf";
export const DISCORD_INVITE_URL = "https://discord.gg/gjSySC3PzV";

const PANEL_TITLE = "Settle into your office";
const DISMISS_LABEL = "I am settled in";

/** Stable ordering + WUPHF copy for each known checklist id. */
const ITEM_IDS = [
  "pick_team",
  "second_key",
  "github_repo",
  "github_star",
  "discord",
] as const;

type ChecklistItemId = (typeof ITEM_IDS)[number];

interface ChecklistItemConfig {
  label: string;
  /** Verb shown on the action control. */
  action: string;
  /** Icon rendered on the action control. */
  icon: ReactNode;
  /** External URL for link items; absent for internal-nav items. */
  href?: string;
  /** Internal navigation for non-link items. */
  navigate?: () => void;
}

const ITEM_CONFIG: Record<ChecklistItemId, ChecklistItemConfig> = {
  pick_team: {
    label: "Pick or trim your team",
    action: "Open your team",
    icon: <ArrowRight width={14} height={14} />,
    navigate: () =>
      void router.navigate({
        to: "/agents/$agentSlug",
        params: { agentSlug: "ceo" },
      }),
  },
  second_key: {
    label: "Add a second runtime so the office never stalls",
    action: "Open Settings",
    icon: <ArrowRight width={14} height={14} />,
    navigate: () =>
      void router.navigate({
        to: "/apps/$appId",
        params: { appId: "settings" },
      }),
  },
  github_repo: {
    // Internal nav to the seeded how-to page, NOT the source repo: clicking
    // "Connect a GitHub repo" should teach the user how to connect their own
    // repo, not drop them on WUPHF's source. This also keeps the destination
    // distinct from github_star below, which genuinely points at the repo.
    label: "Connect a GitHub repo and bring real work in",
    action: "How to connect",
    icon: <Github width={14} height={14} />,
    navigate: () =>
      void router.navigate({
        to: "/wiki/$",
        params: { _splat: "team/getting-started/connecting-your-work" },
      }),
  },
  github_star: {
    label: "Star WUPHF on GitHub (Michael would be proud, probably)",
    action: "Star on GitHub",
    icon: <Github width={14} height={14} />,
    href: GITHUB_REPO_URL,
  },
  discord: {
    label: "Join the Discord and meet other founders",
    action: "Open Discord",
    icon: <Discord width={14} height={14} />,
    href: DISCORD_INVITE_URL,
  },
};

function isKnownItemId(id: string): id is ChecklistItemId {
  return (ITEM_IDS as readonly string[]).includes(id);
}

export function GettingStartedChecklist() {
  const { items, dismissed, isLoading, markItemDone, dismiss } =
    useGettingStartedChecklist();

  // Map server rows (id + done) onto our fixed, copy-bearing config. Unknown
  // ids are dropped so a future server-only item cannot render label-less.
  const rows = ITEM_IDS.map((id) => {
    const serverItem = items.find((item) => item.id === id);
    return {
      id,
      done: serverItem?.done ?? false,
      config: ITEM_CONFIG[id],
    };
  });

  const knownDoneCount = items.filter(
    (item) => isKnownItemId(item.id) && item.done,
  ).length;
  const allDone = knownDoneCount === ITEM_IDS.length;

  // Hide entirely while loading, once dismissed, or after every item is done.
  if (isLoading || dismissed || allDone) {
    return null;
  }

  const meta = (
    <span className="getting-started-checklist-count">
      {knownDoneCount}/{ITEM_IDS.length}
    </span>
  );

  return (
    <div className="getting-started-checklist">
      <CollapsibleSection
        id="getting-started"
        title={PANEL_TITLE}
        meta={meta}
        defaultOpen={true}
      >
        <div
          className="getting-started-checklist-progress"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={ITEM_IDS.length}
          aria-valuenow={knownDoneCount}
          aria-label={`${knownDoneCount} of ${ITEM_IDS.length} steps done`}
        >
          <span
            className="getting-started-checklist-progress-fill"
            style={{
              width: `${(knownDoneCount / ITEM_IDS.length) * 100}%`,
            }}
          />
        </div>
        <ul className="getting-started-checklist-items">
          {rows.map(({ id, done, config }) => (
            <li
              key={id}
              className={`getting-started-checklist-item${done ? " is-done" : ""}`}
            >
              <span
                className={`getting-started-checklist-tick${done ? " is-done" : ""}`}
                aria-hidden="true"
              >
                {done ? <Check width={13} height={13} /> : null}
              </span>
              <span className="getting-started-checklist-label">
                {/* The tick is aria-hidden and the strikethrough is purely
                    visual, so completion is otherwise invisible to screen
                    readers. Announce it in the label itself. */}
                {done ? <span className="sr-only">Done. </span> : null}
                {config.label}
              </span>
              {config.href ? (
                <a
                  className="getting-started-checklist-action"
                  href={config.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={() => markItemDone(id)}
                >
                  {config.icon}
                  <span>{config.action}</span>
                  <OpenNewWindow
                    width={12}
                    height={12}
                    className="getting-started-checklist-action-ext"
                  />
                </a>
              ) : (
                <button
                  type="button"
                  className="getting-started-checklist-action"
                  onClick={() => {
                    markItemDone(id);
                    config.navigate?.();
                  }}
                >
                  {config.icon}
                  <span>{config.action}</span>
                </button>
              )}
            </li>
          ))}
        </ul>
        <div className="getting-started-checklist-footer">
          <Button variant="ghost" size="sm" onClick={() => dismiss()}>
            {DISMISS_LABEL}
          </Button>
        </div>
      </CollapsibleSection>
    </div>
  );
}

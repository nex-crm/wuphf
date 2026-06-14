/**
 * StepTeam — wizard step 03, "Pick a team pack."
 *
 * Full-width, roomy layout (not the 2-column slide split) so the packs and
 * their agents have space to breathe:
 *
 *   1. Pick a pack. Each blueprint is its own agent pack, shown as a roomy
 *      card with an icon, name, agent count, and one-line description.
 *   2. Picking a pack opens a real DRAWER inline, directly beneath that card's
 *      row, that slides its roster open. Reclicking the same card slides the
 *      drawer shut. Check or uncheck any non-lead agent (the lead is always
 *      kept). This is the default team; doing nothing else is a valid choice.
 *   3. "Add a custom agent" is opt-in below, collapsed until asked for, so the
 *      step never becomes a wall of inputs.
 *
 * Company name is collected on the Meet step. The advance gate is satisfied by
 * a chosen pack (or a named custom agent); the host's "set this up later"
 * escape clears both and takes the scratch path.
 */

import {
  Fragment,
  type ReactNode,
  type Ref,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { track } from "../../../../lib/analytics";
import { PixelAvatar } from "../../../ui/PixelAvatar";
import type { BlueprintOption } from "../types";
import {
  ONBOARDING_WIZARD_COPY,
  type OnboardingWizardStepProps,
} from "../wizardSteps";

const COPY = ONBOARDING_WIZARD_COPY.team;

const CUSTOM_NAME_PLACEHOLDER = "RevOps";
const CUSTOM_INSTRUCTIONS_PLACEHOLDER =
  "Keeps the CRM clean: dedupes accounts, backfills owners, flags stale deals.";

/** The scratch path's drawer key, distinct from any blueprint id. */
const SCRATCH_KEY = "scratch";

/** The drawer's open/close duration; the unmount waits this long on close. */
const DRAWER_MS = 320;

/**
 * Fallback icons by blueprint id, used when the backend template carries no
 * emoji of its own. Keeps every pack card visually anchored.
 */
const BLUEPRINT_ICONS: Record<string, string> = {
  "ai-revops": "📊",
  "ai-startup": "🚀",
  "bookkeeping-invoicing-service": "🧾",
  "local-business-ai-package": "🏪",
  "niche-crm": "📇",
  "niche-newsletter": "📰",
  "paid-discord-community": "💬",
  "youtube-factory": "🎬",
};

function blueprintIcon(blueprint: BlueprintOption): string {
  return blueprint.emoji || BLUEPRINT_ICONS[blueprint.id] || "🏢";
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

/** The agent slugs a blueprint contributes when picked (default pack). */
function defaultPickedAgents(blueprint: BlueprintOption): string[] {
  return blueprint.agents
    .filter((agent) => agent.checked || agent.builtIn)
    .map((agent) => agent.slug);
}

/**
 * A collapsible drawer that spans the full width of the pack grid and animates
 * its real height open and shut via the `grid-template-rows: 0fr → 1fr` trick.
 * It is rendered immediately after the card it belongs to, so it reads as that
 * card opening into a panel. The parent keeps it mounted through the close
 * animation, then unmounts it.
 */
function PackDrawer({
  id,
  open,
  children,
}: {
  id: string;
  open: boolean;
  children: ReactNode;
}) {
  return (
    <div
      id={id}
      className={`onboarding-pack-drawer${open ? " is-open" : ""}`}
      data-testid="onboarding-roster-drawer"
    >
      <div className="onboarding-pack-drawer-inner">{children}</div>
    </div>
  );
}

/**
 * The selected pack's roster — the prominent select/unselect list inside the
 * drawer. `panelRef` is used by the host to scroll the opened panel into view.
 */
function PackRoster({
  pack,
  pickedAgents,
  onToggle,
  panelRef,
}: {
  pack: BlueprintOption;
  pickedAgents: string[];
  onToggle: (slug: string, builtIn: boolean) => void;
  panelRef: Ref<HTMLFieldSetElement>;
}) {
  const pickedCount = pack.agents.filter((a) =>
    pickedAgents.includes(a.slug),
  ).length;
  return (
    <fieldset
      ref={panelRef}
      className="onboarding-pack-team onboarding-pack-team--inline"
      data-testid="onboarding-roster-panel"
    >
      {/* Native group semantics for the checkbox set; the visible title below
          stays a <p> so its layout is unaffected. */}
      <legend className="sr-only">Team for {pack.name}</legend>
      <p className="onboarding-pack-team-title">
        <span>
          Your team
          <span className="onboarding-pack-team-sub">
            {" · "}
            {pack.name}
          </span>
        </span>
        <span className="onboarding-pack-team-count">
          {pickedCount} of {pack.agents.length} selected
        </span>
      </p>
      <p className="onboarding-pack-team-hint">
        Every agent in this pack is on by default. Uncheck anyone you do not
        need; the lead stays.
      </p>
      <ul className="onboarding-team-roster" data-testid="onboarding-roster">
        {pack.agents.map((agent) => {
          const isPicked = pickedAgents.includes(agent.slug);
          return (
            <li key={agent.slug}>
              <label
                className={`onboarding-team-agent${isPicked ? " is-picked" : ""}`}
              >
                <input
                  type="checkbox"
                  className="onboarding-team-agent-check"
                  checked={isPicked}
                  disabled={agent.builtIn}
                  onChange={() => onToggle(agent.slug, agent.builtIn)}
                  data-testid={`onboarding-roster-${agent.slug}`}
                />
                <span
                  className="onboarding-team-agent-avatar"
                  aria-hidden="true"
                >
                  <PixelAvatar slug={agent.slug} size={32} />
                </span>
                <span className="onboarding-team-agent-text">
                  <span className="onboarding-team-agent-name">
                    {agent.name}
                    <span className="onboarding-team-agent-handle">
                      @{agent.slug}
                    </span>
                  </span>
                  {agent.role ? (
                    <span className="onboarding-team-agent-role">
                      {agent.role}
                    </span>
                  ) : null}
                </span>
                {agent.builtIn ? (
                  <span className="onboarding-team-agent-lead">Lead</span>
                ) : null}
              </label>
            </li>
          );
        })}
      </ul>
    </fieldset>
  );
}

export function StepTeam({
  active,
  answers,
  setAnswers,
  blueprints,
}: OnboardingWizardStepProps) {
  const [showCustom, setShowCustom] = useState(answers.agentName.trim() !== "");
  const rosterRef = useRef<HTMLFieldSetElement>(null);

  // The drawer key that should be open: a blueprint id, the scratch key, or "".
  const openTarget =
    answers.blueprintId || (answers.startFromScratch ? SCRATCH_KEY : "");

  // Drawer presence + open state, so close animates before unmount. `mounted`
  // is the key whose drawer is in the DOM; `open` drives the slide.
  const [mounted, setMounted] = useState<string | null>(openTarget || null);
  const [open, setOpen] = useState<boolean>(Boolean(openTarget));

  const pickBlueprint = useCallback(
    (blueprint: BlueprintOption) => {
      // Reclicking the open pack slides its drawer shut (deselect).
      if (blueprint.id === answers.blueprintId) {
        setAnswers({
          blueprintId: "",
          pickedAgents: [],
          startFromScratch: false,
        });
        return;
      }
      const picked = defaultPickedAgents(blueprint);
      setAnswers({
        blueprintId: blueprint.id,
        pickedAgents: picked,
        startFromScratch: false,
      });
      track("onboarding_blueprint_selected", {
        blueprint_id: blueprint.id,
        agent_count: picked.length,
        start_from_scratch: false,
      });
    },
    [answers.blueprintId, setAnswers],
  );

  const pickScratch = useCallback(() => {
    // Reclicking scratch slides its drawer shut too.
    if (answers.startFromScratch) {
      setAnswers({ startFromScratch: false });
      return;
    }
    // Empty blueprint = the broker's scratch path (it synthesizes a small
    // founding team). startFromScratch records the deliberate choice so the
    // advance gate lets the user continue with no pack selected.
    setAnswers({ blueprintId: "", pickedAgents: [], startFromScratch: true });
  }, [answers.startFromScratch, setAnswers]);

  const toggleAgent = useCallback(
    (slug: string, builtIn: boolean) => {
      if (builtIn) return; // The lead is always kept.
      const isPicked = answers.pickedAgents.includes(slug);
      setAnswers({
        pickedAgents: isPicked
          ? answers.pickedAgents.filter((s) => s !== slug)
          : [...answers.pickedAgents, slug],
      });
    },
    [answers.pickedAgents, setAnswers],
  );

  const removeCustom = useCallback(() => {
    setShowCustom(false);
    setAnswers({ agentName: "", agentInstructions: "" });
  }, [setAnswers]);

  // Drive the drawer from the open target. Opening mounts the drawer collapsed
  // then flips it open on the next frame so the height transition runs. Closing
  // flips it shut, then unmounts after the slide (instantly under reduced
  // motion, where there is no transition to wait for).
  useEffect(() => {
    if (openTarget) {
      setMounted(openTarget);
      setOpen(false);
      const raf = requestAnimationFrame(() => setOpen(true));
      return () => cancelAnimationFrame(raf);
    }
    setOpen(false);
    const timer = setTimeout(
      () => setMounted(null),
      prefersReducedMotion() ? 0 : DRAWER_MS,
    );
    return () => clearTimeout(timer);
  }, [openTarget]);

  // Bring the opened roster into view so it is never stranded below the fold
  // when the selected card sits in a lower row. `nearest` is a no-op when the
  // panel is already on screen, and the scratch note carries no ref.
  useEffect(() => {
    if (open && rosterRef.current) {
      rosterRef.current.scrollIntoView({ block: "nearest" });
    }
  }, [open]);

  return (
    <div
      className="onboarding-team-full"
      data-active={active}
      data-testid="onboarding-step-team"
    >
      <header className="onboarding-team-header">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
      </header>

      {blueprints.length > 0 ? (
        <div
          className="onboarding-pack-grid"
          data-testid="onboarding-blueprints"
        >
          {blueprints.map((blueprint) => {
            const isSelected = blueprint.id === answers.blueprintId;
            const agentCount = blueprint.agents.length;
            return (
              <Fragment key={blueprint.id}>
                <button
                  type="button"
                  aria-pressed={isSelected}
                  aria-expanded={isSelected}
                  aria-controls={
                    isSelected ? `pack-drawer-${blueprint.id}` : undefined
                  }
                  className={`onboarding-pack-card${isSelected ? " is-selected" : ""}`}
                  onClick={() => pickBlueprint(blueprint)}
                  data-testid={`onboarding-blueprint-${blueprint.id}`}
                >
                  <span
                    className="onboarding-pack-card-icon"
                    aria-hidden="true"
                  >
                    {blueprintIcon(blueprint)}
                  </span>
                  <span className="onboarding-pack-card-body">
                    <span className="onboarding-pack-card-name">
                      {blueprint.name}
                    </span>
                    {blueprint.description ? (
                      <span className="onboarding-pack-card-desc">
                        {blueprint.description}
                      </span>
                    ) : null}
                    <span className="onboarding-pack-card-count">
                      {agentCount} {agentCount === 1 ? "agent" : "agents"}
                    </span>
                  </span>
                </button>

                {/* The card's drawer slides its roster open right here. */}
                {mounted === blueprint.id ? (
                  <PackDrawer id={`pack-drawer-${blueprint.id}`} open={open}>
                    <PackRoster
                      pack={blueprint}
                      pickedAgents={answers.pickedAgents}
                      onToggle={toggleAgent}
                      panelRef={rosterRef}
                    />
                  </PackDrawer>
                ) : null}
              </Fragment>
            );
          })}

          {/* Start from scratch: no pack, the broker synthesizes a founding
              team. Styled as a quieter, dashed sibling of the pack cards. */}
          <button
            type="button"
            aria-pressed={answers.startFromScratch}
            aria-expanded={answers.startFromScratch}
            aria-controls={
              answers.startFromScratch ? "pack-drawer-scratch" : undefined
            }
            className={`onboarding-pack-card onboarding-pack-card--scratch${
              answers.startFromScratch ? " is-selected" : ""
            }`}
            onClick={pickScratch}
            data-testid="onboarding-blueprint-scratch"
          >
            <span className="onboarding-pack-card-icon" aria-hidden="true">
              ✏️
            </span>
            <span className="onboarding-pack-card-body">
              <span className="onboarding-pack-card-name">
                Start from scratch
              </span>
              <span className="onboarding-pack-card-desc">
                Skip the packs. WUPHF stands up a small founding team and you
                grow it from there.
              </span>
              <span className="onboarding-pack-card-count">No pack</span>
            </span>
          </button>

          {/* The scratch card's drawer slides a short confirmation open. */}
          {mounted === SCRATCH_KEY ? (
            <PackDrawer id="pack-drawer-scratch" open={open}>
              <div className="onboarding-pack-team onboarding-pack-team--inline">
                <p className="onboarding-pack-team-hint">
                  Starting from scratch. WUPHF will stand up a CEO and a small
                  founding team. Add a custom agent below, or grow the team
                  later from the office.
                </p>
              </div>
            </PackDrawer>
          ) : null}
        </div>
      ) : (
        <p className="onboarding-blueprint-empty">
          No starter packs loaded. Add a custom agent below, or set the team up
          later from the office.
        </p>
      )}

      {!openTarget ? (
        <p className="onboarding-team-prompt">
          Pick a pack to see its team and tune it, or start from scratch.
        </p>
      ) : null}

      <div className="onboarding-team-custom">
        {showCustom ? (
          <article
            className="onboarding-agent-brief"
            data-testid="onboarding-agent-brief"
          >
            <header className="onboarding-agent-brief-head">
              <span
                className="onboarding-agent-brief-avatar"
                aria-hidden="true"
              >
                <PixelAvatar slug="revops" size={28} />
              </span>
              <span className="onboarding-agent-brief-title">Custom agent</span>
              <button
                type="button"
                className="onboarding-custom-remove"
                onClick={removeCustom}
                data-testid="onboarding-custom-remove"
              >
                Remove
              </button>
            </header>

            <div className="onboarding-custom-fields">
              <div className="onboarding-team-field">
                <label
                  className="onboarding-team-label"
                  htmlFor="onboarding-agent-name"
                >
                  Agent name
                </label>
                <input
                  id="onboarding-agent-name"
                  className="onboarding-team-input"
                  type="text"
                  value={answers.agentName}
                  placeholder={CUSTOM_NAME_PLACEHOLDER}
                  onChange={(event) =>
                    setAnswers({ agentName: event.target.value })
                  }
                  data-testid="onboarding-agent-name"
                />
              </div>

              <div className="onboarding-team-field">
                <label
                  className="onboarding-team-label"
                  htmlFor="onboarding-agent-instructions"
                >
                  What does it do?
                </label>
                <textarea
                  id="onboarding-agent-instructions"
                  className="onboarding-team-textarea"
                  value={answers.agentInstructions}
                  placeholder={CUSTOM_INSTRUCTIONS_PLACEHOLDER}
                  onChange={(event) =>
                    setAnswers({ agentInstructions: event.target.value })
                  }
                  data-testid="onboarding-agent-instructions"
                />
              </div>
            </div>
          </article>
        ) : (
          <button
            type="button"
            className="onboarding-add-custom"
            onClick={() => setShowCustom(true)}
            data-testid="onboarding-add-custom"
          >
            <span className="onboarding-add-custom-plus" aria-hidden="true">
              +
            </span>
            Add a custom agent
          </button>
        )}
      </div>
    </div>
  );
}

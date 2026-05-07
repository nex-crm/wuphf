import { useEffect, useId, useRef, useState } from "react";

import type { Skill, SkillStatus } from "../../../api/client";
import { drawPixelAvatar } from "../../../lib/pixelAvatar";

/**
 * Pokémon-TCG-style pixel skill card.
 *
 * Pure presentational chrome around an existing skill. The owning page
 * (SkillsApp) keeps providing the action buttons + side-panel preview;
 * this component just paints the card body. The pixel art portrait is
 * deterministic per skill — same name + same lead owner always produces
 * the same character through pixelAvatar.ts.
 */
export interface PixelSkillCardProps {
  skill: Skill;
  /** Rendered under the card frame (action buttons, suggest expander). */
  actions?: React.ReactNode;
  /** Opens the SKILL.md SidePanel preview from the back-face CTA. */
  onPreview?: () => void;
}

const STATUS_TO_TYPE: Record<
  SkillStatus,
  "electric" | "psychic" | "dark" | "steel"
> = {
  active: "electric",
  proposed: "psychic",
  disabled: "dark",
  archived: "steel",
};

const TYPE_LABEL: Record<"electric" | "psychic" | "dark" | "steel", string> = {
  electric: "ACTIVE",
  psychic: "PROPOSED",
  dark: "DISABLED",
  steel: "ARCHIVED",
};

function PixelPortrait({ slug }: { slug: string }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    // Pass any positive `size` — the actual rendered size is governed by
    // the .pixel-skill-card__portrait CSS class. Pixel-perfect upscaling
    // happens via image-rendering: pixelated.
    drawPixelAvatar(canvas, slug, 144);
  }, [slug]);
  return <canvas ref={ref} className="pixel-skill-card__portrait" />;
}

// Format an ISO date string with year, e.g. "May 7, 2026". Used on the
// back face where there's room for the full date and ambiguity hurts
// (a skill from last May should not collapse into "May 7" with no year).
function formatLongDate(iso?: string): string | null {
  if (!iso) return null;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

interface DetailRow {
  label: string;
  value: React.ReactNode;
}

function buildDetailRows(skill: Skill): DetailRow[] {
  // Every row maps to a real, typed Skill field. We omit a row entirely
  // rather than render a "—" placeholder when the field is missing, so
  // the back face never claims data that doesn't exist.
  const rows: DetailRow[] = [];
  const status = skill.status ?? "active";
  // Title-case for parity with all the other label/value pairs on the back
  // face (e.g. "@ceo", "May 7, 2026"). Lowercase looked like a stray token.
  rows.push({
    label: "Status",
    value: status.charAt(0).toUpperCase() + status.slice(1),
  });

  const owners = (skill.owner_agents ?? []).filter((s) => s.trim().length > 0);
  rows.push({
    label: "Owners",
    value: owners.length > 0 ? `@${owners.join(", @")}` : "Lead-routable",
  });

  // Trigger lives on the front face now (it is THE field that decides
  // whether a skill is relevant to the moment, so hiding it behind a flip
  // taxes every catalog scan). Don't duplicate it on the back.
  if (skill.created_by) {
    rows.push({ label: "Created by", value: `@${skill.created_by}` });
  }
  const created = formatLongDate(skill.created_at);
  if (created) rows.push({ label: "Created", value: created });
  const updated = formatLongDate(skill.updated_at);
  if (updated && updated !== created) {
    rows.push({ label: "Updated", value: updated });
  }
  if (skill.source) rows.push({ label: "Source", value: skill.source });

  return rows;
}

export function PixelSkillCard({
  skill,
  actions,
  onPreview,
}: PixelSkillCardProps) {
  const status: SkillStatus = skill.status ?? "active";
  const type = STATUS_TO_TYPE[status];
  const owners = (skill.owner_agents ?? []).filter((s) => s.trim().length > 0);
  // Portrait identity: prefer the first owning agent so a skill scoped to
  // @ceo always shows the CEO portrait. Fall back to the skill name so
  // lead-routable skills get their own deterministic character.
  const portraitSlug = owners[0] || skill.name || "skill";
  // Use useId for the name id so the component is self-contained — relying
  // on `skill.name` as a DOM id breaks aria-labelledby if the same slug is
  // ever rendered twice in one tree.
  const reactId = useId();
  const cardId = `pixel-skill-${reactId}`;
  const ownerLabel =
    owners.length > 0 ? `@${owners.join(" / @")}` : "Lead-routable";

  const [flipped, setFlipped] = useState(false);
  const detailRows = buildDetailRows(skill);
  const skillTitle = skill.title || skill.name || "Untitled Skill";

  return (
    <div
      className={["pixel-skill-card-wrap", flipped ? "is-flipped" : ""]
        .filter(Boolean)
        .join(" ")}
      data-type={type}
    >
      <div className="pixel-skill-card-flipper">
        {/* ── FRONT ───────────────────────────────────────────────── */}
        <article
          className="pixel-skill-card pixel-skill-card--front"
          data-type={type}
          data-testid="pixel-skill-card"
          aria-labelledby={`${cardId}-name`}
          aria-hidden={flipped}
          inert={flipped}
        >
          <header className="pixel-skill-card__header">
            <span
              id={`${cardId}-name`}
              className="pixel-skill-card__name"
              title={skillTitle}
            >
              {skillTitle}
            </span>
            {skill.created_by ? (
              <span className="pixel-skill-card__byline">
                {`by @${skill.created_by}`}
              </span>
            ) : null}
          </header>

          {/* Art window — pure decoration; the flip control lives at the
              bottom of the card so it's findable in the same slot on both
              faces. The "NEEDS REVIEW" stamp on proposed cards replaces the
              old holo shimmer, which mis-coded "needs your decision" as
              "rare/foil" — TCG holo means valuable, not pending. */}
          <div className="pixel-skill-card__art" aria-hidden="true">
            <span className="pixel-skill-card__art-floor" aria-hidden="true" />
            <PixelPortrait slug={portraitSlug} />
            {status === "proposed" ? (
              <span
                className="pixel-skill-card__stamp"
                role="img"
                aria-label="Needs review"
              >
                NEEDS
                <br />
                REVIEW
              </span>
            ) : null}
          </div>

          {/* Stat strip uses a <dl> with visually-hidden labels so screen
              readers announce "Status: Active, Owners: @ceo" instead of
              raw values floating without context. Visual layout is
              unchanged — labels are only present in the AT tree. */}
          <dl className="pixel-skill-card__stats">
            <div className="pixel-skill-card__stats-row">
              <dt className="pixel-skill-card__visually-hidden">Status</dt>
              <dd>{TYPE_LABEL[type]}</dd>
            </div>
            <div className="pixel-skill-card__stats-row">
              <dt className="pixel-skill-card__visually-hidden">Owners</dt>
              <dd>{ownerLabel}</dd>
            </div>
          </dl>

          {/* Trigger is the most-asked-for field on a skills shelf — promoted
              from the back so users don't pay a 700ms flip per card to see
              "how does this fire?" */}
          {skill.trigger ? (
            <div className="pixel-skill-card__trigger-row">
              <span className="pixel-skill-card__trigger-label">
                Triggers on
              </span>
              <span className="pixel-skill-card__trigger-value">
                {skill.trigger}
              </span>
            </div>
          ) : null}

          <div className="pixel-skill-card__body">
            {skill.description ? (
              <p className="pixel-skill-card__flavor">{skill.description}</p>
            ) : null}
          </div>

          {/* Flip control sits in the same bottom slot as the back-face flip
              button so users always know where to find the toggle. */}
          <div className="pixel-skill-card__flip-bar">
            <button
              type="button"
              className="pixel-skill-card__flip"
              onClick={() => setFlipped(true)}
              aria-label={`Flip ${skill.name} card to see details`}
              /* aria-pressed (toggle button) maps to a 3D flip more
                 honestly than aria-expanded (disclosure widget) — the
                 back face is always in the DOM, it's just rotated away,
                 not appearing/disappearing. Drop aria-controls for the
                 same reason: there's no disclosure region to point at. */
              aria-pressed={flipped}
            >
              ↻ flip for details
            </button>
          </div>

          {/* Front footer is reserved for the source/set code only — dates
              live on the back (full year-disambiguated) so the front
              doesn't compete with the flavor text for attention. */}
          {skill.source ? (
            <footer className="pixel-skill-card__footer">
              <span
                className="pixel-skill-card__set"
                title={`source: ${skill.source}`}
              >
                {skill.source}
              </span>
            </footer>
          ) : null}
        </article>

        {/* ── BACK ────────────────────────────────────────────────── */}
        {/* `inert` removes the back face's buttons from the tab order while
            it's flipped away — without it, sighted-keyboard users would tab
            into invisible "View full SKILL.md" / "flip back" buttons that
            visually live in the 3D-rotated face behind the front. */}
        <article
          className="pixel-skill-card pixel-skill-card--back"
          data-type={type}
          data-testid="pixel-skill-card-back"
          aria-hidden={!flipped}
          inert={!flipped}
        >
          <header className="pixel-skill-card__header">
            <span className="pixel-skill-card__back-eyebrow" aria-hidden="true">
              SKILL DETAILS
            </span>
          </header>

          <div className="pixel-skill-card__back-body">
            <h3 className="pixel-skill-card__back-title">{skillTitle}</h3>
            <p className="pixel-skill-card__back-name">{skill.name}</p>

            <dl className="pixel-skill-card__detail-list">
              {detailRows.map((row) => (
                <div key={row.label} className="pixel-skill-card__detail-row">
                  <dt>{row.label}</dt>
                  <dd>{row.value}</dd>
                </div>
              ))}
            </dl>
          </div>

          <div className="pixel-skill-card__back-cta">
            {onPreview ? (
              <button
                type="button"
                className="pixel-skill-card__back-button"
                onClick={onPreview}
                aria-label={`View full SKILL.md for ${skill.name}`}
              >
                View full SKILL.md →
              </button>
            ) : null}
          </div>

          {/* Mirror of the front face's flip slot — same place, same shape,
              just pointing the other direction so the muscle-memory holds. */}
          <div className="pixel-skill-card__flip-bar">
            <button
              type="button"
              className="pixel-skill-card__flip"
              onClick={() => setFlipped(false)}
              aria-label={`Flip ${skill.name} card back to front`}
              aria-pressed={flipped}
            >
              ↺ flip back
            </button>
          </div>
        </article>
      </div>

      {actions ? (
        <div className="pixel-skill-card-actions">{actions}</div>
      ) : null}
    </div>
  );
}

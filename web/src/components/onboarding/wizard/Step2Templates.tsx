import { Children, useEffect, useMemo, useState } from "react";

import { InfoCircle, NavArrowDown, Plus } from "iconoir-react";
import { Briefcase } from "pixelarticons/react/Briefcase";
import { MessageText } from "pixelarticons/react/MessageText";
import { Receipt } from "pixelarticons/react/Receipt";
import { Store } from "pixelarticons/react/Store";
import { Target } from "pixelarticons/react/Target";
import { Video } from "pixelarticons/react/Video";

import { BtnLabel, EnterHint } from "./components";

// Pixel-art icon override per pack id. Falls back to the emoji from
// BLUEPRINT_DISPLAY when there's no mapping.
const PACK_PIXEL_ICONS: Record<
  string,
  React.ComponentType<React.SVGProps<SVGSVGElement>>
> = {
  "bookkeeping-invoicing-service": Receipt,
  "local-business-ai-package": Store,
  "multi-agent-workflow-consulting": Briefcase,
  "niche-crm": Target,
  "paid-discord-community": MessageText,
  "youtube-factory": Video,
};
import { BLUEPRINT_CATEGORIES } from "./constants";
import { adaptPackPreview, type PackPreview } from "./packPreview";
import type { BlueprintCategoryKey, BlueprintTemplate } from "./types";

type FilterKey = BlueprintCategoryKey | "all" | "other";

interface TemplatesStepProps {
  templates: BlueprintTemplate[];
  loading: boolean;
  selected: string | null;
  onSelect: (id: string | null) => void;
  onNext: () => void;
  onBack: () => void;
}

const ALL_FILTER: FilterKey = "all";

interface FilterChip {
  key: FilterKey;
  label: string;
}

const STATIC_FILTER_CHIPS: ReadonlyArray<FilterChip> = [
  { key: "all", label: "All" },
  ...BLUEPRINT_CATEGORIES.map(
    (cat): FilterChip => ({ key: cat.key, label: cat.label }),
  ),
];

export function TemplatesStep({
  templates,
  loading,
  selected,
  onSelect,
  onNext,
  onBack,
}: TemplatesStepProps) {
  const previews = useMemo(() => templates.map(adaptPackPreview), [templates]);

  const hasOther = useMemo(
    () => previews.some((p) => p.category === "other"),
    [previews],
  );

  const filterChips = useMemo<ReadonlyArray<FilterChip>>(
    () =>
      hasOther
        ? [...STATIC_FILTER_CHIPS, { key: "other", label: "Other" }]
        : STATIC_FILTER_CHIPS,
    [hasOther],
  );

  const [filter, setFilter] = useState<FilterKey>(ALL_FILTER);
  const [openId, setOpenId] = useState<string | null>(null);

  // (No auto-open on mount — the modal is purely opt-in via the info
  // button on each card.)

  // Drop the "other" filter if the underlying data no longer has any
  // unknown blueprints. Without this the chip can stick after a
  // refresh and silently filter out everything.
  useEffect(() => {
    if (filter === "other" && !hasOther) {
      setFilter(ALL_FILTER);
    }
  }, [filter, hasOther]);

  const visiblePreviews = useMemo(() => {
    if (filter === ALL_FILTER) return previews;
    return previews.filter((p) => p.category === filter);
  }, [previews, filter]);

  const openPreview = useMemo(
    () => previews.find((p) => p.id === openId) ?? null,
    [previews, openId],
  );

  // Clicking the card body now just selects the pack — it does NOT open
  // the detail modal anymore. The dedicated info button opens the modal.
  const handleCardClick = (id: string) => {
    onSelect(id);
  };

  const handleInfoClick = (id: string) => {
    setOpenId(id);
  };

  const handleScratchClick = () => {
    onSelect(null);
    setOpenId(null);
  };

  // Close modal on Escape so it feels like a real dialog.
  useEffect(() => {
    if (!openId) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpenId(null);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [openId]);

  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline">Choose your office's focus</h1>
        <p className="wizard-subhead">
          Each one is a ready-made team for a specific outcome. You can tweak
          everything later.
        </p>
      </div>

      {loading ? (
        <div className="wizard-panel">
          <div
            style={{
              color: "var(--text-tertiary)",
              fontSize: 13,
              textAlign: "center",
              padding: 20,
            }}
          >
            Loading blueprints&hellip;
          </div>
        </div>
      ) : (
        <>
          <div className="template-from-scratch">
            <button
              className={`template-from-scratch-btn ${selected === null ? "selected" : ""}`}
              onClick={handleScratchClick}
              aria-pressed={selected === null}
              type="button"
            >
              <span className="pack-card-radio" aria-hidden="true" />
              Start from scratch
            </button>
          </div>

          <h2 className="pack-library-title">
            or pick a ready-made office
          </h2>
          <section className="pack-library-section">
            <div className="pack-library">
              {visiblePreviews.length === 0 ? (
              <div className="pack-empty">No packs match this filter yet.</div>
            ) : (
              <PackGrid>
                {visiblePreviews.map((preview) => (
                  <PackCard
                    key={preview.id}
                    preview={preview}
                    selected={selected === preview.id}
                    open={openId === preview.id}
                    onClick={() => handleCardClick(preview.id)}
                    onInfo={() => handleInfoClick(preview.id)}
                  />
                ))}
              </PackGrid>
            )}

            {openPreview && (
              <PackDetailModal
                preview={openPreview}
                selected={selected === openPreview.id}
                onChoose={() => {
                  onSelect(openPreview.id);
                  setOpenId(null);
                }}
                onClose={() => setOpenId(null)}
              />
            )}
            </div>
          </section>
        </>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          <BtnLabel>Pick your team</BtnLabel>
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

// ── Pack grid ──────────────────────────────────────────────────────────
//
// 3 cards per row. Initially shows 1.5 rows with a bottom gradient mask
// that fades the second row's bottom half — signalling "more below."
// A down-arrow button under the grid expands by 3 more rows per click;
// it disappears once everything is visible.
const PACK_GRID_INITIAL_ROWS = 1.5;
const PACK_GRID_ROW_STEP = 3;

function PackGrid({ children }: { children: React.ReactNode }) {
  const total = Children.count(children);
  const totalRows = Math.ceil(total / 3);
  const [visibleRows, setVisibleRows] = useState<number>(
    PACK_GRID_INITIAL_ROWS,
  );

  // Are there more rows beyond what's currently visible? If not, hide
  // the expand affordance.
  const collapsed = visibleRows < totalRows;
  const expand = () =>
    setVisibleRows((r) =>
      Math.min(totalRows, Math.ceil(r) + PACK_GRID_ROW_STEP),
    );

  return (
    <div className="pack-grid">
      <div
        className="pack-grid-track"
        data-collapsed={collapsed || undefined}
        style={{ "--pack-grid-rows": visibleRows } as React.CSSProperties}
      >
        {children}
      </div>
      {collapsed ? (
        <button
          type="button"
          className="pack-grid-more"
          onClick={expand}
          aria-label="Show more packs"
        >
          <NavArrowDown width={18} height={18} aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}

interface PackCardProps {
  preview: PackPreview;
  selected: boolean;
  open: boolean;
  onClick: () => void;
  onInfo: () => void;
}

function PackCard({
  preview,
  selected,
  open,
  onClick,
  onInfo,
}: PackCardProps) {
  return (
    <div
      className={`pack-card ${selected ? "selected" : ""} ${open ? "open" : ""}`}
    >
      <button
        type="button"
        className="pack-card-body"
        onClick={onClick}
        aria-pressed={selected}
      >
        <div className="pack-card-head">
          <span className="pack-card-head-row">
            <span className="pack-card-radio" aria-hidden="true" />
            {(() => {
              const PixelIcon = PACK_PIXEL_ICONS[preview.id];
              if (PixelIcon) {
                return (
                  <span className="pack-card-icon" aria-hidden="true">
                    <PixelIcon width={20} height={20} />
                  </span>
                );
              }
              return preview.icon ? (
                <span className="pack-card-icon" aria-hidden="true">
                  {preview.icon}
                </span>
              ) : null;
            })()}
          </span>
          <span className="pack-card-name">{preview.name}</span>
        </div>
        <p className="pack-card-outcome">{preview.outcome}</p>
      </button>
      <button
        type="button"
        className="pack-card-info"
        onClick={(e) => {
          e.stopPropagation();
          onInfo();
        }}
        aria-label={`See ${preview.name} details`}
        aria-haspopup="dialog"
      >
        <InfoCircle
          width={18}
          height={18}
          strokeWidth={1.75}
          aria-hidden="true"
        />
      </button>
    </div>
  );
}

interface PackDetailModalProps {
  preview: PackPreview;
  selected: boolean;
  onChoose: () => void;
  onClose: () => void;
}

function PackDetailModal({
  preview,
  selected,
  onChoose,
  onClose,
}: PackDetailModalProps) {
  const noMetadata =
    preview.firstTasks.length === 0 &&
    preview.skills.length === 0 &&
    preview.requirements.length === 0 &&
    preview.wikiScaffold.length === 0;

  return (
    <div
      className="pack-modal-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
      role="presentation"
    >
      <section
        className="pack-detail pack-modal"
        aria-label={`${preview.name} details`}
        role="dialog"
        aria-modal="true"
      >
      <header className="pack-detail-head">
        <div>
          <p className="pack-detail-eyebrow">Pack details</p>
          <h2 className="pack-detail-title">{preview.name}</h2>
          <p className="pack-detail-outcome">{preview.outcome}</p>
        </div>
        <button
          type="button"
          className="pack-detail-close"
          onClick={onClose}
          aria-label="Close pack details"
        >
          Close
        </button>
      </header>

      {preview.description && preview.description !== preview.outcome ? (
        <p className="pack-detail-description">{preview.description}</p>
      ) : null}

      <div className="pack-detail-grid">
        <PackDetailSection title="Team" empty="Configured during setup.">
          {preview.agents.length > 0 && (
            <ul className="pack-detail-list">
              {preview.agents.map((agent) => (
                <li key={agent.slug}>
                  <span className="pack-detail-name">{agent.name}</span>
                  {agent.role ? (
                    <span className="pack-detail-role"> — {agent.role}</span>
                  ) : null}
                  {agent.builtIn ? (
                    <span className="pack-detail-tag">lead</span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>

        <PackDetailSection
          title="Channels"
          empty="Channels are added during setup."
        >
          {preview.channels.length > 0 && (
            <ul className="pack-detail-list">
              {preview.channels.map((channel) => (
                <li key={channel.slug}>
                  <span className="pack-detail-name">#{channel.slug}</span>
                  {channel.purpose ? (
                    <span className="pack-detail-role">
                      {" "}
                      — {channel.purpose}
                    </span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>

        <PackDetailSection
          title="Skills"
          empty="Skills will be configured during setup."
        >
          {preview.skills.length > 0 && (
            <ul className="pack-detail-list">
              {preview.skills.map((skill) => (
                <li key={skill.name}>
                  <span className="pack-detail-name">{skill.name}</span>
                  {skill.purpose ? (
                    <span className="pack-detail-role"> — {skill.purpose}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>

        <PackDetailSection
          title="Wiki scaffold"
          empty="Wiki entries are added during setup."
        >
          {preview.wikiScaffold.length > 0 && (
            <ul className="pack-detail-list">
              {preview.wikiScaffold.map((entry) => (
                <li key={entry.path}>
                  <span className="pack-detail-name">{entry.title}</span>
                  <span className="pack-detail-path"> — {entry.path}</span>
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>

        <PackDetailSection
          title="First tasks"
          empty="Tasks and skills will be configured during setup."
        >
          {preview.firstTasks.length > 0 && (
            <ul className="pack-detail-list">
              {preview.firstTasks.map((task) => (
                <li key={task.id} className="pack-detail-task">
                  <span className="pack-detail-name">{task.title}</span>
                  {task.expectedOutput ? (
                    <p className="pack-detail-expected">
                      <span className="pack-detail-expected-label">
                        Result:
                      </span>{" "}
                      {task.expectedOutput}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>

        <PackDetailSection
          title="Requirements"
          empty="No external dependencies required."
        >
          {preview.requirements.length > 0 && (
            <ul className="pack-detail-list">
              {preview.requirements.map((req) => (
                <li key={`${req.kind}-${req.name}`}>
                  <span className="pack-detail-name">{req.name}</span>
                  <span className="pack-detail-tag">{req.kind}</span>
                  {!req.required ? (
                    <span className="pack-detail-tag pack-detail-tag-optional">
                      optional
                    </span>
                  ) : null}
                  {req.detail ? (
                    <span className="pack-detail-role"> — {req.detail}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </PackDetailSection>
      </div>

      {noMetadata ? (
        <p className="pack-detail-empty">
          Tasks, skills, and requirements will be configured during setup.
        </p>
      ) : null}

      <div className="pack-detail-cta">
        <button
          type="button"
          className="btn btn-secondary"
          onClick={onChoose}
          disabled={selected}
        >
          {selected ? "Selected" : "Choose this pack"}
        </button>
      </div>
      </section>
    </div>
  );
}

interface PackDetailSectionProps {
  title: string;
  empty: string;
  children?: React.ReactNode;
}

function PackDetailSection({ title, empty, children }: PackDetailSectionProps) {
  const hasChildren =
    children !== null && children !== undefined && children !== false;
  return (
    <div className="pack-detail-section">
      <h3 className="pack-detail-section-title">{title}</h3>
      {hasChildren ? (
        children
      ) : (
        <p className="pack-detail-section-empty">{empty}</p>
      )}
    </div>
  );
}

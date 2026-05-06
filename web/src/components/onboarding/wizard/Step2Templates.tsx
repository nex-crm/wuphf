import { useEffect, useMemo, useState } from "react";

import { ArrowIcon, EnterHint } from "./components";
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

  // Auto-open the detail panel for the currently-selected pack so a
  // user who returns to this step (Back from Team) sees their pick
  // already expanded.
  useEffect(() => {
    if (selected && previews.some((p) => p.id === selected)) {
      setOpenId(selected);
    }
  }, [selected, previews]);

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

  const handleCardClick = (id: string) => {
    onSelect(id);
    setOpenId(id);
  };

  const handleScratchClick = () => {
    onSelect(null);
    setOpenId(null);
  };

  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Pick the outcome you want to ship
        </div>
        <h1 className="wizard-headline">What should your office run?</h1>
        <p className="wizard-subhead">
          Each pack is a starting team, channels, skills, and a first task —
          assembled around an outcome. You can change anything later.
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
          <div className="pack-library">
            <div
              className="pack-filter-row"
              role="tablist"
              aria-label="Pack categories"
            >
              {filterChips.map((chip) => (
                <button
                  key={chip.key}
                  type="button"
                  role="tab"
                  aria-selected={filter === chip.key}
                  className={`pack-filter-chip ${filter === chip.key ? "active" : ""}`}
                  onClick={() => setFilter(chip.key)}
                >
                  {chip.label}
                </button>
              ))}
            </div>

            {visiblePreviews.length === 0 ? (
              <div className="pack-empty">No packs match this filter yet.</div>
            ) : (
              <div className="pack-grid">
                {visiblePreviews.map((preview) => (
                  <PackCard
                    key={preview.id}
                    preview={preview}
                    selected={selected === preview.id}
                    open={openId === preview.id}
                    onClick={() => handleCardClick(preview.id)}
                  />
                ))}
              </div>
            )}

            {openPreview && (
              <PackDetailPanel
                preview={openPreview}
                selected={selected === openPreview.id}
                onChoose={() => onSelect(openPreview.id)}
                onClose={() => setOpenId(null)}
              />
            )}
          </div>

          <div className="template-from-scratch">
            <button
              className={`template-from-scratch-btn ${selected === null ? "selected" : ""}`}
              onClick={handleScratchClick}
              aria-pressed={selected === null}
              type="button"
            >
              <span className="template-from-scratch-icon">+</span>
              Start from scratch
              <span className="template-from-scratch-sub">
                5-person founding team: CEO, GTM Lead, Founding Engineer, PM,
                Designer
              </span>
            </button>
          </div>
        </>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Review the team
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

interface PackCardProps {
  preview: PackPreview;
  selected: boolean;
  open: boolean;
  onClick: () => void;
}

function PackCard({ preview, selected, open, onClick }: PackCardProps) {
  return (
    <button
      type="button"
      className={`pack-card ${selected ? "selected" : ""} ${open ? "open" : ""}`}
      onClick={onClick}
      aria-pressed={selected}
      aria-expanded={open}
    >
      <div className="pack-card-head">
        {preview.icon ? (
          <span className="pack-card-icon" aria-hidden="true">
            {preview.icon}
          </span>
        ) : null}
        <span className="pack-card-name">{preview.name}</span>
      </div>
      <p className="pack-card-outcome">{preview.outcome}</p>
      <div className="pack-card-meta">
        {preview.agents.length > 0 && (
          <span className="pack-card-meta-item">
            {preview.agents.length} agents
          </span>
        )}
        {preview.firstTasks.length > 0 && (
          <span className="pack-card-meta-item">
            {preview.firstTasks.length} first task
            {preview.firstTasks.length === 1 ? "" : "s"}
          </span>
        )}
        {typeof preview.estimatedSetupMinutes === "number" && (
          <span className="pack-card-meta-item">
            ~{preview.estimatedSetupMinutes} min setup
          </span>
        )}
      </div>
    </button>
  );
}

interface PackDetailPanelProps {
  preview: PackPreview;
  selected: boolean;
  onChoose: () => void;
  onClose: () => void;
}

function PackDetailPanel({
  preview,
  selected,
  onChoose,
  onClose,
}: PackDetailPanelProps) {
  const noMetadata =
    preview.firstTasks.length === 0 &&
    preview.skills.length === 0 &&
    preview.requirements.length === 0 &&
    preview.wikiScaffold.length === 0;

  return (
    <section className="pack-detail" aria-label={`${preview.name} details`}>
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

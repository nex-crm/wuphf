import { ArrowIcon, EnterHint } from "./components";
import { BLUEPRINT_CATEGORIES, BLUEPRINT_DISPLAY } from "./constants";
import type { BlueprintCategoryKey, BlueprintTemplate } from "./types";

interface TemplatesStepProps {
  templates: BlueprintTemplate[];
  loading: boolean;
  selected: string | null;
  onSelect: (id: string | null) => void;
  onNext: () => void;
  onBack: () => void;
}

export function TemplatesStep({
  templates,
  loading,
  selected,
  onSelect,
  onNext,
  onBack,
}: TemplatesStepProps) {
  // Group templates by display category. Unknown blueprint ids (not in the
  // frontend catalog) land in a catch-all "Other" bucket so new backend
  // templates still render, just without the short-description and icon
  // treatment.
  const grouped = new Map<
    BlueprintCategoryKey | "other",
    BlueprintTemplate[]
  >();
  for (const t of templates) {
    const display = BLUEPRINT_DISPLAY[t.id];
    const key: BlueprintCategoryKey | "other" = display?.category ?? "other";
    const list = grouped.get(key) ?? [];
    list.push(t);
    grouped.set(key, list);
  }

  const renderTile = (t: BlueprintTemplate) => {
    const display = BLUEPRINT_DISPLAY[t.id];
    const icon = display?.icon ?? t.emoji;
    const desc = display?.shortDescription ?? t.description;
    return (
      <button
        key={t.id}
        className={`template-card ${selected === t.id ? "selected" : ""}`}
        onClick={() => onSelect(t.id)}
        aria-pressed={selected === t.id}
        type="button"
      >
        {icon ? <div className="template-card-emoji">{icon}</div> : null}
        <div className="template-card-name">{t.name}</div>
        <div className="template-card-desc">{desc}</div>
      </button>
    );
  };

  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Start with a preset, or build from scratch
        </div>
        <h1 className="wizard-headline">What should your office run?</h1>
        <p className="wizard-subhead">
          Pick the shape of work. We&apos;ll assemble the team, channels, and
          first tasks around it. You can change anything later.
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
          {BLUEPRINT_CATEGORIES.map((cat) => {
            const items = grouped.get(cat.key) ?? [];
            if (items.length === 0) return null;
            return (
              <div key={cat.key} className="wizard-panel template-group">
                <div className="template-group-head">
                  <p className="template-group-label">{cat.label}</p>
                  <p className="template-group-hint">{cat.hint}</p>
                </div>
                <div className="template-grid">{items.map(renderTile)}</div>
              </div>
            );
          })}

          {(grouped.get("other") ?? []).length > 0 && (
            <div className="wizard-panel template-group">
              <div className="template-group-head">
                <p className="template-group-label">Other</p>
              </div>
              <div className="template-grid">
                {(grouped.get("other") ?? []).map(renderTile)}
              </div>
            </div>
          )}

          <div className="template-from-scratch">
            <button
              className={`template-from-scratch-btn ${selected === null ? "selected" : ""}`}
              onClick={() => onSelect(null)}
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

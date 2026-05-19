import { useRef, useState } from "react";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, EnterHint } from "./components";

function rgbToHsl(r: number, g: number, b: number): [number, number, number] {
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h: number;
  switch (max) {
    case r:
      h = (g - b) / d + (g < b ? 6 : 0);
      break;
    case g:
      h = (b - r) / d + 2;
      break;
    default:
      h = (r - g) / d + 4;
  }
  return [h * 60, s, l];
}

// Foreground letter color in the SAME hue as the background — picks
// a darker shade when the bg is light, a lighter shade when the bg is
// dark, and bumps saturation so the letter pops.
function readableForeground(hex: string): string {
  const clean = hex.replace("#", "");
  if (clean.length !== 6) return "#18181b";
  const r = parseInt(clean.slice(0, 2), 16) / 255;
  const g = parseInt(clean.slice(2, 4), 16) / 255;
  const b = parseInt(clean.slice(4, 6), 16) / 255;
  const [h, s, l] = rgbToHsl(r, g, b);
  // Achromatic backgrounds (gray/white/black) get a neutral letter.
  if (s < 0.08) return l > 0.55 ? "#18181b" : "#ffffff";
  const targetL = l > 0.55 ? 0.28 : 0.88;
  const targetS = Math.min(1, Math.max(0.45, s));
  return `hsl(${h.toFixed(0)}, ${(targetS * 100).toFixed(0)}%, ${(targetL * 100).toFixed(0)}%)`;
}

// Hash a string to a stable HSL hue so the avatar color is deterministic
// per office name — doesn't flicker on every keystroke, only shifts when
// the name actually changes.
function avatarColors(seed: string): { bg: string; fg: string } {
  if (!seed) return { bg: "#e4e4e7", fg: "#71717a" };
  let h = 0;
  for (let i = 0; i < seed.length; i++) {
    h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  }
  const hue = h % 360;
  return {
    bg: `hsl(${hue}, 62%, 88%)`,
    fg: `hsl(${hue}, 55%, 32%)`,
  };
}

// Pixel-art "traffic-light" dot — 16×16 silhouette pulled from the
// user-supplied path. `currentColor` so each dot inherits its tint
// from the parent CSS class.
function PixelDot({ className }: { className?: string }) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 16 16"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      aria-hidden="true"
    >
      <path
        d="M12 2H14V4H16V12H14V14H12V16H4V14H2V12H0V4H2V2H4V0H12V2Z"
        fill="currentColor"
      />
    </svg>
  );
}

function OfficePreview({
  company,
  description,
  companyColor,
  onChangeCompanyColor,
}: {
  company: string;
  description: string;
  companyColor: string | null;
  onChangeCompanyColor: (v: string | null) => void;
}) {
  const displayName = company.trim() || "Your office";
  const displayDesc =
    description.trim() || "A short description of what this office runs.";
  const initial = (company.trim()[0] ?? "?").toUpperCase();
  const derived = avatarColors(company.trim().toLowerCase());

  // User-picked override is now lifted up to the wizard so it persists
  // through navigation, the draft, and onto the office record. Local
  // ref is just for triggering the native color picker.
  const colorInputRef = useRef<HTMLInputElement>(null);

  const bg = companyColor ?? derived.bg;
  const fg = companyColor ? readableForeground(companyColor) : derived.fg;

  return (
    <aside className="identity-preview" aria-label="Office preview">
      <div className="identity-window">
        <div className="identity-window-chrome">
          <PixelDot className="identity-window-dot identity-window-dot--red" />
          <PixelDot className="identity-window-dot identity-window-dot--yellow" />
          <PixelDot className="identity-window-dot identity-window-dot--green" />
        </div>
        <div className="identity-window-body">
          <button
            type="button"
            className="identity-preview-avatar"
            style={{ background: bg, color: fg }}
            onClick={() => colorInputRef.current?.click()}
            aria-label="Pick avatar color"
          >
            {initial}
            <input
              ref={colorInputRef}
              type="color"
              className="identity-preview-color-input"
              value={companyColor ?? "#e4e4e7"}
              onChange={(e) => onChangeCompanyColor(e.target.value)}
              aria-hidden="true"
              tabIndex={-1}
            />
          </button>
          <div className="identity-preview-text">
            <p
              className={`identity-preview-name ${company.trim() ? "" : "is-placeholder"}`}
            >
              {displayName}
            </p>
            <p
              className={`identity-preview-desc ${description.trim() ? "" : "is-placeholder"}`}
            >
              {displayDesc}
            </p>
          </div>
        </div>
      </div>
    </aside>
  );
}

interface IdentityStepProps {
  company: string;
  description: string;
  companyColor?: string | null;
  // Optional fields are still threaded through from the wizard so existing
  // wiring keeps working — they're just not rendered here anymore.
  priority?: string;
  website?: string;
  ownerName?: string;
  ownerRole?: string;
  onChangeCompany: (v: string) => void;
  onChangeDescription: (v: string) => void;
  onChangeCompanyColor?: (v: string | null) => void;
  onChangePriority?: (v: string) => void;
  onChangeWebsite?: (v: string) => void;
  onChangeOwnerName?: (v: string) => void;
  onChangeOwnerRole?: (v: string) => void;
  onNext: () => void;
  onBack: () => void;
}

export function IdentityStep({
  company,
  description,
  companyColor = null,
  onChangeCompany,
  onChangeDescription,
  onChangeCompanyColor = () => {},
  onNext,
  onBack,
}: IdentityStepProps) {
  const canContinue =
    company.trim().length > 0 && description.trim().length > 0;

  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline wizard-headline-sm">
          {ONBOARDING_COPY.step2_headline}
        </h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step2_subhead}</p>
      </div>

      <div className="wizard-panel identity-panel">
        <div className="identity-form">
          <div className="form-group">
            <label className="label" htmlFor="wiz-company">
              Office name <span style={{ color: "var(--red)" }}>*</span>
            </label>
            <input
              className="input"
              id="wiz-company"
              placeholder="Acme Operations, or your real project name"
              autoComplete="organization"
              value={company}
              onChange={(e) => onChangeCompany(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label className="label" htmlFor="wiz-description">
              Short description <span style={{ color: "var(--red)" }}>*</span>
            </label>
            <textarea
              className="input identity-description-input"
              id="wiz-description"
              placeholder="What real business or workflow should this office run?"
              rows={1}
              value={description}
              onChange={(e) => onChangeDescription(e.target.value)}
              onInput={(e) => {
                // Auto-resize: collapse then grow to content. The CSS
                // field-sizing:content fallback handles modern browsers
                // declaratively; this keeps Safari < 17.4 working too.
                const el = e.currentTarget;
                el.style.height = "auto";
                el.style.height = `${el.scrollHeight}px`;
              }}
            />
          </div>
        </div>
        <OfficePreview
          company={company}
          description={description}
          companyColor={companyColor}
          onChangeCompanyColor={onChangeCompanyColor}
        />
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button
          className="btn btn-primary"
          onClick={onNext}
          disabled={!canContinue}
          type="button"
        >
          <BtnLabel>{ONBOARDING_COPY.step2_cta}</BtnLabel>
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

import { useEffect } from "react";

import { Building } from "pixelarticons/react/Building";
import { SquareCursor } from "pixelarticons/react/SquareCursor";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel } from "./components";

// Keycap glyph showing a number — used on the choose-mode CTAs where
// "1" triggers Build and "2" triggers Tour. Mirrors the .kbd-hint
// keycap styling that EnterHint uses so the two buttons read as one
// vocabulary.
function NumberHint({ digit }: { digit: number }) {
  return (
    <span className="kbd-hint" aria-hidden="true">
      <span className="kbd-hint-num">{digit}</span>
    </span>
  );
}

interface ChooseModeStepProps {
  // Full-setup path — proceed into the rest of the wizard.
  onCreate: () => void;
  // Quick-start path — seed a sample office and jump to the ready
  // step. Optional so callers/tests that don't wire it up still compile.
  onTrySample?: () => void;
}

export function ChooseModeStep({ onCreate, onTrySample }: ChooseModeStepProps) {
  // Number keys go straight to the action — 1 = Build, 2 = Tour. Skip
  // when typing in an input (defensive, even though this step has none).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "1") {
        e.preventDefault();
        onCreate();
      } else if (e.key === "2" && onTrySample) {
        e.preventDefault();
        onTrySample();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onCreate, onTrySample]);

  return (
    <div className="wizard-step welcome-split-step">
      <div className="welcome-split">
        <section className="welcome-half welcome-half--primary">
          <div className="welcome-half-inner">
            <div className="welcome-half-text">
              <Building
                className="welcome-half-icon"
                aria-hidden="true"
                width={40}
                height={40}
              />
              <h1 className="wizard-headline">
                {ONBOARDING_COPY.step2_choose_create_headline}
              </h1>
              <p className="wizard-subhead" style={{ whiteSpace: "pre-line" }}>
                {ONBOARDING_COPY.step2_choose_create_subhead}
              </p>
            </div>
            <div className="welcome-half-cta">
              <button
                className="btn btn-primary"
                onClick={onCreate}
                type="button"
              >
                <BtnLabel>{ONBOARDING_COPY.step2_choose_create_cta}</BtnLabel>
                <NumberHint digit={1} />
              </button>
            </div>
          </div>
        </section>
        <section className="welcome-half welcome-half--sample">
          <div className="welcome-half-inner">
            <div className="welcome-half-text">
              <SquareCursor
                className="welcome-half-icon"
                aria-hidden="true"
                width={40}
                height={40}
              />
              <h1 className="wizard-headline">
                {ONBOARDING_COPY.step1_sample_headline}
              </h1>
              <p className="wizard-subhead" style={{ whiteSpace: "pre-line" }}>
                {ONBOARDING_COPY.step1_sample_subhead}
              </p>
            </div>
            <div className="welcome-half-cta">
              <button
                className="btn btn-sample"
                onClick={onTrySample}
                type="button"
                data-testid="welcome-try-sample"
                disabled={!onTrySample}
              >
                <BtnLabel>{ONBOARDING_COPY.step1_cta_sample}</BtnLabel>
                <NumberHint digit={2} />
              </button>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}

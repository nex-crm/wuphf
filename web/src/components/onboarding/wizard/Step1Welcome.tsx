import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, EnterHint } from "./components";

interface WelcomeStepProps {
  onNext: () => void;
}

export function WelcomeStep({ onNext }: WelcomeStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline">{ONBOARDING_COPY.step1_headline}</h1>
        <p className="wizard-subhead" style={{ whiteSpace: "pre-line" }}>
          {ONBOARDING_COPY.step1_subhead}
        </p>
      </div>
      <div style={{ display: "flex", justifyContent: "center" }}>
        <button className="btn btn-primary" onClick={onNext} type="button">
          <BtnLabel>{ONBOARDING_COPY.step1_cta}</BtnLabel>
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

import { ONBOARDING_COPY } from "../../../lib/constants";
import { ArrowIcon, EnterHint } from "./components";

interface WelcomeStepProps {
  onNext: () => void;
}

export function WelcomeStep({ onNext }: WelcomeStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Ready to set up
        </div>
        <h1 className="wizard-headline">{ONBOARDING_COPY.step1_headline}</h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step1_subhead}</p>
      </div>
      <div style={{ display: "flex", justifyContent: "center" }}>
        <button className="btn btn-primary" onClick={onNext} type="button">
          {ONBOARDING_COPY.step1_cta}
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

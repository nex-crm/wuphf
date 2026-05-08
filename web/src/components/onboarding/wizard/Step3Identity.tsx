import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, EnterHint } from "./components";

interface IdentityStepProps {
  company: string;
  description: string;
  priority: string;
  website: string;
  ownerName: string;
  ownerRole: string;
  onChangeCompany: (v: string) => void;
  onChangeDescription: (v: string) => void;
  onChangePriority: (v: string) => void;
  onChangeWebsite: (v: string) => void;
  onChangeOwnerName: (v: string) => void;
  onChangeOwnerRole: (v: string) => void;
  onNext: () => void;
  onBack: () => void;
}

export function IdentityStep({
  company,
  description,
  priority,
  website,
  ownerName,
  ownerRole,
  onChangeCompany,
  onChangeDescription,
  onChangePriority,
  onChangeWebsite,
  onChangeOwnerName,
  onChangeOwnerRole,
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

      <div className="wizard-panel">
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
          <input
            className="input"
            id="wiz-description"
            placeholder="What real business or workflow should this office run?"
            value={description}
            onChange={(e) => onChangeDescription(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="label" htmlFor="wiz-priority">
            Top priority right now
          </label>
          <input
            className="input"
            id="wiz-priority"
            placeholder="Win the first real customer loop"
            value={priority}
            onChange={(e) => onChangePriority(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="label" htmlFor="wiz-website">
            Company website <span className="label-optional">(optional)</span>
          </label>
          <input
            className="input"
            id="wiz-website"
            type="url"
            placeholder="https://acme.com"
            autoComplete="url"
            value={website}
            onChange={(e) => onChangeWebsite(e.target.value)}
          />
        </div>
        <div className="form-group form-group-row">
          <div className="form-group">
            <label className="label" htmlFor="wiz-owner-name">
              Your name <span className="label-optional">(optional)</span>
            </label>
            <input
              className="input"
              id="wiz-owner-name"
              placeholder="Nazz Mohammad"
              autoComplete="name"
              value={ownerName}
              onChange={(e) => onChangeOwnerName(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label className="label" htmlFor="wiz-owner-role">
              Your role <span className="label-optional">(optional)</span>
            </label>
            <input
              className="input"
              id="wiz-owner-role"
              placeholder="Founder, CTO..."
              value={ownerRole}
              onChange={(e) => onChangeOwnerRole(e.target.value)}
            />
          </div>
        </div>
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

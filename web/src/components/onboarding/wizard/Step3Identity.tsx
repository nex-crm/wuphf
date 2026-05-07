import { ArrowIcon, EnterHint } from "./components";
import { NexSignupPanel } from "./NexSignupPanel";
import type { NexSignupStatus } from "./types";

interface IdentityStepProps {
  company: string;
  description: string;
  priority: string;
  nexEmail: string;
  nexSignupStatus: NexSignupStatus;
  nexSignupError: string;
  website: string;
  ownerName: string;
  ownerRole: string;
  onChangeCompany: (v: string) => void;
  onChangeDescription: (v: string) => void;
  onChangePriority: (v: string) => void;
  onChangeNexEmail: (v: string) => void;
  onSubmitNexSignup: () => void;
  onOpenNexSignup: () => void;
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
  nexEmail,
  nexSignupStatus,
  nexSignupError,
  website,
  ownerName,
  ownerRole,
  onChangeCompany,
  onChangeDescription,
  onChangePriority,
  onChangeNexEmail,
  onSubmitNexSignup,
  onOpenNexSignup,
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
      <div className="wizard-panel">
        <p className="wizard-panel-title">Tell us about this office</p>
        <div className="form-group">
          <label className="label" htmlFor="wiz-company">
            Company or project name{" "}
            <span style={{ color: "var(--red)" }}>*</span>
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
            One-liner description <span style={{ color: "var(--red)" }}>*</span>
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

      {nexSignupStatus === "hidden" ? (
        <div className="wiz-nex-trigger">
          <button
            type="button"
            className="wiz-nex-trigger-link"
            onClick={onOpenNexSignup}
          >
            Don&apos;t have a Nex account? Sign up here.
          </button>
        </div>
      ) : (
        <NexSignupPanel
          email={nexEmail}
          status={nexSignupStatus}
          error={nexSignupError}
          onChangeEmail={onChangeNexEmail}
          onSubmit={onSubmitNexSignup}
        />
      )}

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
          Continue
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  );
}

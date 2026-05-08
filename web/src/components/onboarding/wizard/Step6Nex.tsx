import { ONBOARDING_COPY } from "../../../lib/constants";
import { NexSignupPanel } from "./NexSignupPanel";
import type { NexSignupStatus } from "./types";

interface NexStepProps {
  email: string;
  status: NexSignupStatus;
  error: string;
  onChangeEmail: (v: string) => void;
  onSubmit: () => void;
  onNext: () => void;
  onBack: () => void;
}

export function NexStep({
  email,
  status,
  error,
  onChangeEmail,
  onSubmit,
  onNext,
  onBack,
}: NexStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        {/* Headline is split: text "Power up with" + inline Nex logo SVG.
            We bypass ONBOARDING_COPY.step6_headline ("Power up with Nex")
            because the trailing "Nex" word is rendered as the logo, not text. */}
        <h1
          className="wizard-headline wizard-headline-sm"
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: 10,
          }}
        >
          Power up with
          <svg
            width="80"
            height="23"
            viewBox="0 0 110 32"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            aria-label="Nex"
            style={{ display: "inline-block", verticalAlign: "middle" }}
          >
            <path
              d="M18.3949 13.497C16.3742 11.4764 15.9816 8.35399 15.1776 5.61176C14.8281 4.41952 14.1835 3.29614 13.2438 2.35643C10.2446 -0.642794 5.37436 -0.63529 2.36588 2.37319C-0.642602 5.38168 -0.650106 10.2519 2.34912 13.2511C3.28881 14.1908 4.41217 14.8353 5.60439 15.1849C8.34663 15.9889 11.469 16.3816 13.4897 18.4023C15.5104 20.4229 15.903 23.5453 16.7071 26.2876C17.0566 27.4798 17.7012 28.6031 18.6408 29.5428C21.6401 32.5421 26.5103 32.5346 29.5188 29.5261C32.5272 26.5176 32.5348 21.6474 29.5355 18.6482C28.5958 17.7084 27.4724 17.0639 26.2802 16.7143C23.538 15.9103 20.4156 15.5177 18.3949 13.497Z"
              fill="currentColor"
            />
            <path
              d="M11.6148 26.1926C11.6148 29.3999 9.01475 32 5.80741 32C2.60006 32 0 29.3999 0 26.1926C0 22.9853 2.60006 20.3852 5.80741 20.3852C9.01475 20.3852 11.6148 22.9853 11.6148 26.1926Z"
              fill="currentColor"
            />
            <path
              d="M32 5.80741C32 9.01475 29.3999 11.6148 26.1926 11.6148C22.9853 11.6148 20.3852 9.01475 20.3852 5.80741C20.3852 2.60006 22.9853 0 26.1926 0C29.3999 0 32 2.60006 32 5.80741Z"
              fill="currentColor"
            />
            <path
              d="M47.2338 10.3642V31.872H42V2H47.6663L59.9073 22.9104V2H65.1411V31.872H59.8641L47.2338 10.3642Z"
              fill="currentColor"
            />
            <path
              d="M89.4058 22.1849H72.9259C73.5315 25.0868 75.9105 27.2205 78.722 27.2205C80.8415 27.2205 82.7014 26.0256 83.7395 24.1906H88.9733C87.5891 28.7141 83.5232 32 78.722 32C72.7529 32 67.9516 26.9218 67.9516 20.6913C67.9516 14.4609 72.7529 9.38265 78.722 9.38265C83.5232 9.38265 87.5891 12.6686 88.9733 17.192C89.3193 18.3016 89.4923 19.4538 89.4923 20.6913C89.4923 21.2034 89.4923 21.6728 89.4058 22.1849ZM73.7045 17.192H83.7395C82.7014 15.357 80.8415 14.1622 78.722 14.1622C76.6025 14.1622 74.7426 15.357 73.7045 17.192Z"
              fill="currentColor"
            />
            <path
              d="M110 9.04125L102.344 20.478L110 31.872H103.944L99.3162 24.9587L94.6447 31.872H88.6323L96.2883 20.478L88.6323 9.04125H94.6447L99.3162 15.9545L103.944 9.04125H110Z"
              fill="currentColor"
            />
          </svg>
        </h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step6_subhead}</p>
      </div>

      <NexSignupPanel
        email={email}
        status={status}
        error={error}
        onChangeEmail={onChangeEmail}
        onSubmit={onSubmit}
      />

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <div className="wizard-nav-right">
          {status !== "ok" && status !== "fallback" ? (
            <button
              className="task-skip"
              onClick={onNext}
              disabled={status === "submitting"}
              type="button"
            >
              Skip
            </button>
          ) : null}
          {status === "ok" || status === "fallback" ? (
            // `ok`: email queued, user continues with confirmation copy.
            // `fallback`: nex-cli missing, user registers in a browser and
            // pastes the key later from Settings → Integrations.
            // Both terminal states advance unconditionally.
            <button className="btn btn-primary" onClick={onNext} type="button">
              Continue
            </button>
          ) : (
            <button
              className="btn btn-primary"
              onClick={onSubmit}
              disabled={status === "submitting" || email.trim().length === 0}
              type="button"
            >
              {status === "submitting"
                ? "Registering…"
                : "Register and continue"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

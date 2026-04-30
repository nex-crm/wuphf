import { ONBOARDING_COPY } from "../../../lib/constants";
import { Kbd, MOD_KEY } from "../../ui/Kbd";
import { ArrowIcon, EnterHint } from "./components";
import type { TaskTemplate } from "./types";

interface TaskStepProps {
  taskTemplates: TaskTemplate[];
  selectedTaskTemplate: string | null;
  onSelectTaskTemplate: (id: string | null) => void;
  taskText: string;
  onChangeTaskText: (v: string) => void;
  onNext: () => void;
  onSkip: () => void;
  onBack: () => void;
  submitting: boolean;
}

export function TaskStep({
  taskTemplates,
  selectedTaskTemplate,
  onSelectTaskTemplate,
  taskText,
  onChangeTaskText,
  onNext,
  onSkip,
  onBack,
  submitting,
}: TaskStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline" style={{ fontSize: 28 }}>
          {ONBOARDING_COPY.step3_title}
        </h1>
        {taskTemplates.length > 0 && (
          <p className="wizard-subhead">
            Type your own first task, or pick from the blueprint&apos;s
            suggested sequence below.
          </p>
        )}
      </div>

      <div>
        <textarea
          className="task-textarea task-textarea-primary"
          id="wiz-task-input"
          placeholder={ONBOARDING_COPY.step3_placeholder}
          value={taskText}
          onChange={(e) => onChangeTaskText(e.target.value)}
        />
        <p className="task-textarea-hint">
          <Kbd size="sm">↵</Kbd> new line · <Kbd size="sm">{MOD_KEY}</Kbd>
          <Kbd size="sm">↵</Kbd> review setup
        </p>
      </div>

      {taskTemplates.length > 0 && (
        <div className="task-suggestions">
          <p className="task-suggestions-label">
            Suggested sequence for this blueprint
          </p>
          <div className="task-suggestions-list">
            {taskTemplates.map((t, idx) => {
              const isSelected = selectedTaskTemplate === t.id;
              return (
                <button
                  key={t.id}
                  className={`task-suggestion ${isSelected ? "selected" : ""}`}
                  onClick={() => {
                    const nextId = isSelected ? null : t.id;
                    onSelectTaskTemplate(nextId);
                    if (nextId) {
                      onChangeTaskText(t.prompt ?? t.name);
                    }
                  }}
                  type="button"
                >
                  <span className="task-suggestion-num">{idx + 1}</span>
                  <span className="task-suggestion-name">{t.name}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <div className="wizard-nav-right">
          <button
            className="task-skip"
            onClick={onSkip}
            disabled={submitting}
            type="button"
          >
            {ONBOARDING_COPY.step3_skip}
          </button>
          <button className="btn btn-primary" onClick={onNext} type="button">
            Review setup
            <ArrowIcon />
            <EnterHint modifier={MOD_KEY} />
          </button>
        </div>
      </div>
    </div>
  );
}

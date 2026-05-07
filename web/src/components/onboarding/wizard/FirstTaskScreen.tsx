import { ArrowIcon } from "./components";

interface FirstTaskScreenProps {
  taskText: string;
  onWatchTask: () => void;
  onSkipToOffice: () => void;
}

/**
 * Post-onboarding screen shown when the user submitted a first task.
 * Primary CTA routes to the live task view; secondary drops into the office.
 */
export function FirstTaskScreen({
  taskText,
  onWatchTask,
  onSkipToOffice,
}: FirstTaskScreenProps) {
  return (
    <div className="wizard-step" data-testid="first-task-screen">
      <div className="wizard-hero">
        <h1 className="wizard-headline" style={{ fontSize: 28 }}>
          Office is open
        </h1>
        <p className="wizard-subhead">
          Your team is live. Your first task is queued.
        </p>
      </div>

      <div
        className="wizard-panel"
        style={{ borderLeft: "3px solid var(--accent)" }}
        data-testid="first-task-preview"
      >
        <p
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: "var(--text-secondary)",
            marginBottom: 6,
          }}
        >
          First task
        </p>
        <p
          style={{
            fontSize: 14,
            color: "var(--text)",
            margin: 0,
            lineHeight: 1.5,
            display: "-webkit-box",
            WebkitLineClamp: 3,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
          }}
        >
          {taskText}
        </p>
      </div>

      <div className="wizard-panel" style={{ gap: 8, display: "flex", flexDirection: "column" }}>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            margin: 0,
          }}
        >
          An agent will pick this up in seconds. Watch the run live, or explore
          your office and check back.
        </p>
      </div>

      <div className="wizard-nav">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={onSkipToOffice}
          data-testid="first-task-skip"
        >
          Explore the office
        </button>
        <button
          type="button"
          className="btn btn-primary"
          onClick={onWatchTask}
          data-testid="first-task-watch"
        >
          Watch it run
          <ArrowIcon />
        </button>
      </div>
    </div>
  );
}

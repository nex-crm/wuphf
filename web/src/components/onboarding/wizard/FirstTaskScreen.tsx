import { useState } from "react";
import { ArrowIcon } from "./components";

interface FirstTaskScreenProps {
  taskText?: string;
  onWatchTask: () => void;
  onSkipToOffice: () => void;
}

/**
 * Post-onboarding screen shown when the user submitted a first task.
 * Primary CTA routes to the live task view; secondary drops into the office.
 */
export function FirstTaskScreen({
  taskText = "",
  onWatchTask,
  onSkipToOffice,
}: FirstTaskScreenProps) {
  const [acted, setActed] = useState(false);
  const trimmed = taskText.trim();

  return (
    <div className="wizard-step" data-testid="first-task-screen">
      <div className="wizard-hero">
        <h1 className="wizard-headline" style={{ fontSize: 28 }}>
          Office is open
        </h1>
        <p className="wizard-subhead">
          {trimmed
            ? "Your team is live. Your first task is queued."
            : "Your team is live. Give them a task from the general channel."}
        </p>
      </div>

      {trimmed ? (
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
              wordBreak: "break-word",
            }}
          >
            {trimmed}
          </p>
        </div>
      ) : (
        <div
          className="wizard-panel"
          data-testid="first-task-empty"
          style={{ textAlign: "center", padding: "20px 0" }}
        >
          <p style={{ fontSize: 14, color: "var(--text-secondary)", margin: 0 }}>
            No task queued yet. Head to{" "}
            <strong style={{ color: "var(--text)" }}>#general</strong> and type
            your first task — agents pick it up immediately.
          </p>
        </div>
      )}

      <div className="wizard-panel" style={{ gap: 8, display: "flex", flexDirection: "column" }}>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            margin: 0,
          }}
        >
          {trimmed
            ? "An agent will pick this up in seconds. Watch the run live, or explore your office and check back."
            : "Explore your office, check the wiki, or start chatting with your team."}
        </p>
      </div>

      <div className="wizard-nav">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => { setActed(true); onSkipToOffice(); }}
          disabled={acted}
          data-testid="first-task-skip"
        >
          Explore the office
        </button>
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => { setActed(true); onWatchTask(); }}
          disabled={acted}
          data-testid="first-task-watch"
        >
          Watch it run
          <ArrowIcon />
        </button>
      </div>
    </div>
  );
}

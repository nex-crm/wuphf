import type { TaskDefinition } from "../../api/tasks";

interface TaskDefinitionViewProps {
  /** The structured intake contract (R4) set via team_task action=define. */
  definition: TaskDefinition;
}

/**
 * TaskDefinitionView — renders the task's structured definition (the R4
 * intake contract) as a clean block inside the rail's Details section:
 * Goal, Deliverables with format chips, Success criteria checklist-style,
 * and Access needed. This is the contract the owner executes against; it
 * lives where the spec document used to live (core-loop R2 removed specs).
 */
export function TaskDefinitionView({ definition }: TaskDefinitionViewProps) {
  const deliverables = definition.deliverables ?? [];
  const criteria = definition.success_criteria ?? [];
  const access = definition.access_needed ?? [];
  return (
    <div className="task-definition" data-testid="task-definition">
      <div className="task-definition-group">
        <span className="task-definition-label">Goal</span>
        <p className="task-definition-goal">{definition.goal}</p>
      </div>
      {deliverables.length > 0 ? (
        <div className="task-definition-group">
          <span className="task-definition-label">Deliverables</span>
          <ul className="task-definition-list">
            {deliverables.map((d) => (
              <li
                key={`${d.name}::${d.format ?? ""}`}
                className="task-definition-deliverable"
              >
                <span className="task-definition-deliverable-name">
                  {d.name}
                </span>
                {d.format ? (
                  <span className="task-definition-chip">{d.format}</span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {criteria.length > 0 ? (
        <div className="task-definition-group">
          <span className="task-definition-label">Success criteria</span>
          <ul className="task-definition-list">
            {criteria.map((c) => (
              <li key={c} className="task-definition-criterion">
                <span className="task-definition-check" aria-hidden="true">
                  ☐
                </span>
                <span>{c}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {access.length > 0 ? (
        <div className="task-definition-group">
          <span className="task-definition-label">Access needed</span>
          <div className="task-definition-access">
            {access.map((a) => (
              <span key={a} className="task-definition-chip">
                {a}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

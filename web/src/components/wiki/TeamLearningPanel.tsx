import { useEffect, useState } from "react";

import { fetchTeamLearnings, type TeamLearning } from "../../api/learning";
import { formatAgentName } from "../../lib/agentName";

interface TeamLearningPanelProps {
  playbookSlug?: string;
  limit?: number;
}

export default function TeamLearningPanel({
  playbookSlug,
  limit = 6,
}: TeamLearningPanelProps) {
  const [entries, setEntries] = useState<TeamLearning[]>([]);
  const [loading, setLoading] = useState(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    if (!playbookSlug) {
      setEntries([]);
      setErrorMessage(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setErrorMessage(null);
    fetchTeamLearnings({ playbook_slug: playbookSlug, limit })
      .then((rows) => {
        if (!cancelled) {
          setEntries(rows);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setErrorMessage(
            err instanceof Error
              ? err.message
              : "Failed to load team learnings.",
          );
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [playbookSlug, limit]);

  if (loading) {
    return (
      <section className="wk-team-learnings" data-testid="wk-team-learnings">
        <h2>Team learnings</h2>
        <p className="wk-team-learnings__empty">loading learnings…</p>
      </section>
    );
  }

  if (errorMessage) {
    return (
      <section className="wk-team-learnings" data-testid="wk-team-learnings">
        <h2>Team learnings</h2>
        <p className="wk-team-learnings__empty">
          Could not load team learnings: {errorMessage}
        </p>
      </section>
    );
  }

  if (entries.length === 0) {
    return (
      <section className="wk-team-learnings" data-testid="wk-team-learnings">
        <h2>Team learnings</h2>
        <p className="wk-team-learnings__empty">
          No structured learnings linked here yet.
        </p>
      </section>
    );
  }

  return (
    <section className="wk-team-learnings" data-testid="wk-team-learnings">
      <h2>Team learnings</h2>
      <ol className="wk-team-learnings__list">
        {entries.map((entry) => (
          <li key={entry.id} className="wk-team-learning">
            <div className="wk-team-learning__topline">
              <span
                className={`wk-team-learning__type wk-team-learning__type--${entry.type}`}
              >
                {entry.type}
              </span>
              <code className="wk-team-learning__key">{entry.key}</code>
              {entry.trusted ? (
                <span className="wk-team-learning__trusted">trusted</span>
              ) : null}
            </div>
            <p className="wk-team-learning__insight">{entry.insight}</p>
            <div className="wk-team-learning__meta">
              <span>{entry.scope}</span>
              <span>{entry.source}</span>
              <span>
                {entry.effective_confidence ?? entry.confidence}/10 confidence
              </span>
              <span>{formatAgentName(entry.created_by)}</span>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

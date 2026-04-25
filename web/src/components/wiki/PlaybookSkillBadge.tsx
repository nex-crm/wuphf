import { useEffect, useState } from "react";

import { fetchPlaybooks, type PlaybookSummary } from "../../api/playbook";

interface PlaybookSkillBadgeProps {
  slug: string;
}

/**
 * Read-only badge surfacing the compiled-skill path for a given source
 * playbook article. v1.3 scope: the badge is informational only — no
 * run-from-UI button (automatic invocation is explicitly out-of-scope for
 * this milestone).
 */
export default function PlaybookSkillBadge({ slug }: PlaybookSkillBadgeProps) {
  const [playbook, setPlaybook] = useState<PlaybookSummary | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetchPlaybooks()
      .then((rows) => {
        if (cancelled) return;
        setPlaybook(rows.find((r) => r.slug === slug) ?? null);
      })
      .catch(() => {
        if (!cancelled) setPlaybook(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  if (loading) return null;

  // If the backend does not know about this playbook yet — e.g. because
  // the source article was written seconds ago and the catalog scan hasn't
  // picked it up — render a pending badge instead of nothing so the reader
  // knows compilation is in-flight.
  const skillPath =
    playbook?.skill_path ?? `team/playbooks/.compiled/${slug}/SKILL.md`;
  const compiled = playbook?.skill_exists ?? false;

  return (
    <div
      className={
        compiled
          ? "wk-playbook-badge wk-playbook-badge--compiled"
          : "wk-playbook-badge wk-playbook-badge--pending"
      }
      role="status"
      data-testid="wk-playbook-badge"
    >
      <span className="wk-playbook-badge__dot" aria-hidden="true" />
      <span className="wk-playbook-badge__label">
        {compiled ? (
          <>
            Compiled skill:{" "}
            <code className="wk-playbook-badge__path">{skillPath}</code>
          </>
        ) : (
          <>Compiled skill pending — recompiling…</>
        )}
      </span>
      {compiled && playbook && (
        <span className="wk-playbook-badge__meta">
          {playbook.execution_count} execution
          {playbook.execution_count === 1 ? "" : "s"} logged
        </span>
      )}
    </div>
  );
}

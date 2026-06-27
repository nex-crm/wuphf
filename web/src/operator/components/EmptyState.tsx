// Empty state — warmth + context + a primary action, never a dead-end line.
// Presentational; the action is optional.

interface EmptyStateProps {
  glyph: string;
  title: string;
  hint: string;
  actionLabel?: string;
  onAction?: () => void;
}

export function EmptyState({
  glyph,
  title,
  hint,
  actionLabel,
  onAction,
}: EmptyStateProps) {
  return (
    <div className="opr-empty">
      <span className="opr-empty-glyph" aria-hidden>
        {glyph}
      </span>
      <div className="opr-empty-title">{title}</div>
      <div className="opr-empty-hint">{hint}</div>
      {actionLabel ? (
        <div className="opr-empty-actions">
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={onAction}
          >
            {actionLabel}
          </button>
        </div>
      ) : null}
    </div>
  );
}

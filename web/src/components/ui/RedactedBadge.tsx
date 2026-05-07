interface RedactedBadgeProps {
  reasons?: string[];
}

export function RedactedBadge({ reasons }: RedactedBadgeProps) {
  const title = reasons?.length
    ? `Redacted: ${reasons.join(", ")}`
    : "Sensitive information was redacted";
  return (
    <span className="badge badge-neutral" title={title}>
      redacted
    </span>
  );
}

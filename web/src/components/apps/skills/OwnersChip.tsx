export interface OwnersChipProps {
  slugs?: string[];
}

/**
 * Small pill rendering the agent slugs that own a skill. Empty/missing
 * slugs render as "lead-routable" (italic, dim) to make ownership status
 * legible at a glance without the user squinting at a missing field.
 */
export function OwnersChip({ slugs }: OwnersChipProps) {
  const list = (slugs ?? []).map((s) => s.trim()).filter(Boolean);
  if (list.length === 0) {
    return (
      <span
        className="owners-chip owners-chip--lead"
        title="Lead-routable: any agent can route through the team lead"
      >
        lead-routable
      </span>
    );
  }
  return (
    <span
      className="owners-chip"
      title={`Scoped to: ${list.map((s) => `@${s}`).join(", ")}`}
    >
      {list.map((s) => `@${s}`).join(", ")}
    </span>
  );
}

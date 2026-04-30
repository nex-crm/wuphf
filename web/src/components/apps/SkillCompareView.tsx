import type { Skill } from "../../api/client";
import { LightningIcon } from "../ui/LightningIcon";

interface SkillCompareViewProps {
  /** The existing active skill the candidate is similar to. */
  existing: Skill | undefined;
  /** The candidate (proposed/ambiguous) skill. */
  candidate: Skill;
  /** Optional similarity score (0..1), shown in the header. */
  score?: number;
  /** Detection method ("embedding-cosine" | "jaccard-tokens"). */
  method?: string;
}

/**
 * Side-by-side compare panel used by the enhance-existing interview and
 * the ambiguous-band similarity banner's [Compare] action. Renders the
 * existing skill on the left, candidate on the right. Body is monospace
 * scrollable so long playbooks don't blow up the panel height.
 *
 * Designed to live inside the existing <SidePanel /> primitive. On mobile
 * the two panels stack via flex-wrap.
 */
export function SkillCompareView({
  existing,
  candidate,
  score,
  method,
}: SkillCompareViewProps) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 12,
        fontSize: 13,
        color: "var(--text)",
      }}
    >
      {typeof score === "number" ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            background: "var(--bg-warm, var(--neutral-50))",
            border: "1px solid var(--border)",
            borderRadius: 6,
            padding: "6px 10px",
          }}
        >
          Similarity score: <strong>{score.toFixed(2)}</strong>
          {method ? ` · ${method}` : ""}
        </div>
      ) : null}

      <div
        style={{
          display: "flex",
          gap: 12,
          flexWrap: "wrap",
        }}
      >
        <SkillPanel
          title="Existing"
          subtitle={existing ? `@${existing.name}` : undefined}
          skill={existing}
          tone="existing"
        />
        <SkillPanel
          title="Candidate"
          subtitle={candidate.name ? `@${candidate.name}` : undefined}
          skill={candidate}
          tone="candidate"
        />
      </div>
    </div>
  );
}

interface SkillPanelProps {
  title: string;
  subtitle?: string;
  skill: Skill | undefined;
  tone: "existing" | "candidate";
}

function SkillPanel({ title, subtitle, skill, tone }: SkillPanelProps) {
  const accent = tone === "candidate" ? "var(--yellow)" : "var(--neutral-300)";
  return (
    <section
      aria-label={`${title} skill`}
      style={{
        flex: "1 1 220px",
        minWidth: 0,
        border: "1px solid var(--border)",
        borderTop: `3px solid ${accent}`,
        borderRadius: 6,
        padding: 10,
        background: "var(--bg-card, #fff)",
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          marginBottom: 6,
        }}
      >
        <LightningIcon size={14} />
        <span style={{ fontSize: 12, fontWeight: 600, color: "var(--text)" }}>
          {title}
        </span>
        {subtitle ? (
          <span
            style={{
              fontSize: 11,
              fontFamily: "var(--font-mono)",
              color: "var(--text-tertiary)",
              marginLeft: "auto",
            }}
          >
            {subtitle}
          </span>
        ) : null}
      </header>

      {skill?.description ? (
        <p
          style={{
            margin: "0 0 6px 0",
            fontSize: 12,
            color: "var(--text-secondary)",
            lineHeight: 1.45,
          }}
        >
          {skill.description}
        </p>
      ) : null}

      {skill?.trigger ? (
        <p
          style={{
            margin: "0 0 6px 0",
            fontSize: 11,
            fontStyle: "italic",
            color: "var(--text-tertiary)",
          }}
        >
          Trigger: {skill.trigger}
        </p>
      ) : null}

      <pre
        style={{
          maxHeight: 320,
          overflow: "auto",
          background: "var(--bg-warm, var(--neutral-50))",
          border: "1px solid var(--border)",
          borderRadius: 4,
          padding: 8,
          margin: 0,
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          lineHeight: 1.5,
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
        }}
      >
        {skill?.content?.trim() ||
          (skill ? "(no body content)" : "(skill not found)")}
      </pre>
    </section>
  );
}

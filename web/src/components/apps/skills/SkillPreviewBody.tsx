import { useCallback, useEffect, useState } from "react";

import { patchSkill, type Skill } from "../../../api/client";
import { fetchArticle } from "../../../api/wiki";
import { showNotice } from "../../ui/Toast";
import { OwnersChip } from "./OwnersChip";
import { STATUS_BADGE_CLASS } from "./status";

interface SkillPreviewBodyProps {
  skill: Skill;
  /** Notifies the parent panel when the editor has unsaved changes. */
  onDirtyChange?: (dirty: boolean) => void;
  /** Called after a successful save with the updated skill. */
  onSaved?: (updated: Skill) => void;
}

export function SkillPreviewBody({
  skill,
  onDirtyChange,
  onSaved,
}: SkillPreviewBodyProps) {
  const owners = skill.owner_agents ?? [];
  const isProposed = skill.status === "proposed";

  // baseline = the content the editor is "based on" (the last server-known
  // state). It only updates when the user opens a different skill OR
  // explicitly saves. Comparing draft against baseline determines dirty
  // state; comparing against `skill.content` would let a background
  // refetch silently overwrite the user's typing.
  const [baseline, setBaseline] = useState(skill.content ?? "");
  const [draft, setDraft] = useState(skill.content ?? "");
  const [saving, setSaving] = useState(false);

  // Reset baseline + draft when the user navigates to a DIFFERENT skill.
  // Keyed on skill.name only so post-save updates to skill.content (from
  // the parent passing back the updated Skill) don't blow away chars the
  // user typed in the gap between save resolution and effect run.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see comment
  useEffect(() => {
    const next = skill.content ?? "";
    setBaseline(next);
    setDraft(next);
    onDirtyChange?.(false);
  }, [skill.name]);

  const dirty = isProposed && draft !== baseline;

  // Keep the parent informed of dirty-state so the SidePanel close path
  // can prompt for unsaved edits.
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleSave = useCallback(() => {
    if (!(isProposed && dirty && skill.name)) return;
    setSaving(true);
    // Snapshot the draft we're committing — a chained edit landing while
    // the request is in flight would otherwise change `draft` under us
    // and confuse the post-save baseline.
    const committed = draft;
    const oldString = baseline;
    patchSkill(skill.name, {
      old_string: oldString,
      new_string: committed,
      replace_all: false,
    })
      .then((res) => {
        showNotice("Saved.", "success");
        const canonicalContent = res.skill?.content ?? committed;
        // Sync baseline to the saved body BEFORE notifying the parent. If
        // the user typed more chars while the request was in flight they
        // remain in `draft`, the dirty flag flips back on, and the
        // editor naturally surfaces the new unsaved delta — instead of
        // the parent's effect resetting `draft` and silently losing them.
        setBaseline(canonicalContent);
        setDraft((current) =>
          current === committed ? canonicalContent : current,
        );
        const updated: Skill = res.skill
          ? { ...res.skill, content: canonicalContent }
          : { ...skill, content: canonicalContent };
        onSaved?.(updated);
      })
      .catch((e: Error) => {
        showNotice(`Couldn't save: ${e.message}`, "error");
      })
      .finally(() => setSaving(false));
  }, [isProposed, dirty, skill, baseline, draft, onSaved]);

  const handleRevert = useCallback(() => {
    setDraft(baseline);
  }, [baseline]);

  return (
    <section style={{ fontSize: 13, lineHeight: 1.55, color: "var(--text)" }}>
      <SkillPreviewHeader skill={skill} owners={owners} />
      {isProposed ? (
        <SkillBodyEditor
          skillName={skill.name}
          draft={draft}
          saving={saving}
          dirty={dirty}
          onDraftChange={setDraft}
          onRevert={handleRevert}
          onSave={handleSave}
        />
      ) : (
        <SkillFullContent
          skillName={skill.name}
          fallbackContent={skill.content}
        />
      )}
    </section>
  );
}

function SkillPreviewHeader({
  skill,
  owners,
}: {
  skill: Skill;
  owners: string[];
}) {
  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
          marginBottom: 12,
        }}
      >
        <OwnersChip slugs={owners} />
        {skill.status ? (
          <span className={STATUS_BADGE_CLASS[skill.status]}>
            {skill.status}
          </span>
        ) : null}
      </div>
      {skill.description ? (
        <p style={{ marginTop: 0, marginBottom: 12 }}>{skill.description}</p>
      ) : null}
      {skill.trigger ? (
        <p
          style={{
            marginTop: 0,
            marginBottom: 12,
            color: "var(--text-secondary)",
            fontStyle: "italic",
          }}
        >
          Trigger: {skill.trigger}
        </p>
      ) : null}
    </>
  );
}

function SkillBodyEditor({
  skillName,
  draft,
  saving,
  dirty,
  onDraftChange,
  onRevert,
  onSave,
}: {
  skillName: string;
  draft: string;
  saving: boolean;
  dirty: boolean;
  onDraftChange: (value: string) => void;
  onRevert: () => void;
  onSave: () => void;
}) {
  return (
    <>
      <label
        htmlFor="skill-body-editor"
        style={{
          display: "block",
          fontSize: 12,
          fontWeight: 500,
          color: "var(--text-secondary)",
          marginBottom: 6,
        }}
      >
        SKILL.md body
        <span
          style={{
            fontWeight: 400,
            color: "var(--text-tertiary)",
            marginLeft: 6,
          }}
        >
          (frontmatter is read-only in v1)
        </span>
      </label>
      <textarea
        id="skill-body-editor"
        value={draft}
        onChange={(e) => onDraftChange(e.target.value)}
        disabled={saving}
        spellCheck={false}
        aria-label={`Edit body for ${skillName}`}
        style={{
          width: "100%",
          minHeight: 240,
          maxHeight: "60vh",
          padding: 12,
          background: "var(--bg-warm, var(--neutral-50))",
          border: "1px solid var(--border)",
          borderRadius: 6,
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          lineHeight: 1.5,
          color: "var(--text)",
          resize: "vertical",
          boxSizing: "border-box",
        }}
      />
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "flex-end",
          gap: 8,
          marginTop: 8,
        }}
      >
        {dirty ? (
          <span
            aria-live="polite"
            style={{
              fontSize: 12,
              color: "var(--text-tertiary)",
              marginRight: "auto",
            }}
          >
            Unsaved changes.
          </span>
        ) : null}
        <button
          type="button"
          className="btn-text"
          onClick={onRevert}
          disabled={saving || !dirty}
        >
          Revert
        </button>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={onSave}
          disabled={saving || !dirty}
          aria-label={`Save edits to ${skillName}`}
        >
          {saving ? "Saving..." : "Save"}
        </button>
      </div>
      <p
        style={{
          marginTop: 12,
          marginBottom: 0,
          fontSize: 12,
          color: "var(--text-tertiary)",
        }}
      >
        Saving leaves this proposal pending. Approve or reject from the
        interview to promote it.
      </p>
    </>
  );
}

function SkillFullContent({
  skillName,
  fallbackContent,
}: {
  skillName: string;
  fallbackContent?: string;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    setContent(null);
    fetchArticle(`team/skills/${skillName}.md`)
      .then((article) => setContent(article.content))
      .catch(() => setContent(fallbackContent ?? null))
      .finally(() => setLoading(false));
  }, [skillName, fallbackContent]);

  if (loading) {
    return (
      <div
        style={{
          color: "var(--text-tertiary)",
          fontSize: 13,
          padding: "12px 0",
        }}
      >
        Loading SKILL.md…
      </div>
    );
  }

  return (
    <>
      <div
        style={{
          fontSize: 12,
          fontWeight: 500,
          color: "var(--text-secondary)",
          marginBottom: 6,
        }}
      >
        SKILL.md
      </div>
      <pre
        style={{
          background: "var(--bg-warm, var(--neutral-50))",
          border: "1px solid var(--border)",
          borderRadius: 6,
          padding: 12,
          fontSize: 12,
          fontFamily: "var(--font-mono)",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          margin: 0,
          maxHeight: "70vh",
          overflowY: "auto",
        }}
      >
        {content ?? "No content available."}
      </pre>
    </>
  );
}

export function ProposedPreviewBody({ skill }: { skill: Skill }) {
  const body = skill.content ?? "";
  const truncated = body.length > 500 ? `${body.slice(0, 500)}…` : body;
  if (!(truncated || skill.description || skill.trigger)) return null;
  return (
    <div
      style={{
        marginTop: 4,
        marginBottom: 8,
        paddingLeft: 10,
        borderLeft: "3px solid var(--neutral-200, #cfd1d2)",
      }}
    >
      {skill.trigger ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            fontStyle: "italic",
            marginBottom: 6,
          }}
        >
          Trigger: {skill.trigger}
        </div>
      ) : null}
      {truncated ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            whiteSpace: "pre-wrap",
            fontFamily: "var(--font-mono)",
            lineHeight: 1.5,
          }}
        >
          {truncated}
        </div>
      ) : null}
    </div>
  );
}

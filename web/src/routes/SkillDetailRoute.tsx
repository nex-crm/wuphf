import { useCallback, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ArrowLeft, Code, FloppyDisk, OpenBook } from "iconoir-react";

import {
  editSkillContent,
  getSkillsList,
  type Skill,
  type SkillStatus,
} from "../api/client";
import { fetchArticle } from "../api/wiki";
import { router } from "../lib/router";
import { showNotice } from "../components/ui/Toast";

interface SkillDetailRouteProps {
  skillName: string;
}

type Mode = "edit" | "preview";

/**
 * Full-screen SKILL.md editor + viewer.
 *
 * Header: back link + skill name + status pill + Save (when dirty).
 * Mode strip: Edit (raw markdown textarea) ↔ Preview (rendered markdown).
 * Both modes share the same buffer so toggling never loses the draft.
 * Save calls PUT /skills/{name} with the full body — broker re-runs the
 * safety scan + rewrites the wiki article in one shot.
 */
export function SkillDetailRoute({ skillName }: SkillDetailRouteProps) {
  const queryClient = useQueryClient();
  const decodedName = useMemo(() => {
    try {
      return decodeURIComponent(skillName);
    } catch {
      return skillName;
    }
  }, [skillName]);

  // Pull skill metadata from the cached catalog (already populated by
  // SkillsApp via the same key).
  const { data: catalog } = useQuery({
    queryKey: ["skills", "all"],
    queryFn: () => getSkillsList("all"),
    staleTime: 10_000,
  });
  const skill: Skill | undefined = useMemo(
    () => catalog?.skills.find((s) => s.name === decodedName),
    [catalog, decodedName],
  );

  // Fetch the full SKILL.md from the wiki — body + frontmatter. Falls
  // back to skill.content if the article isn't on disk yet (proposed).
  const articleQuery = useQuery({
    queryKey: ["skill-article", decodedName],
    queryFn: async () => {
      const path = `team/skills/${decodedName}.md`;
      try {
        const article = await fetchArticle(path);
        return article.content ?? "";
      } catch {
        return null;
      }
    },
    staleTime: 30_000,
  });

  const initialContent =
    articleQuery.data ?? skill?.content ?? "";

  const [mode, setMode] = useState<Mode>("edit");
  const [draft, setDraft] = useState<string>("");
  const [seedKey, setSeedKey] = useState<string>("");

  // Seed the draft buffer once per (skill, content) load. We compare
  // against a seed key built from the full content (not its length) so
  // a same-length edit still re-seeds when the skill switches. We also
  // re-seed on empty initialContent so switching to a blank skill
  // doesn't keep the previous skill's draft in the editor. The dirty
  // guard prevents a background article refetch from clobbering an
  // in-progress edit — a dirty edit always wins until it's saved or
  // discarded.
  useEffect(() => {
    const key = `${decodedName}::${initialContent}`;
    if (key === seedKey) return;
    const hasDirtyEdit = draft !== "" && draft !== initialContent;
    if (hasDirtyEdit) return;
    setDraft(initialContent);
    setSeedKey(key);
  }, [decodedName, initialContent, seedKey, draft]);

  // Any divergence from the seeded content is dirty — including an
  // intentional "clear this skill" edit. The save handler guards the
  // empty-buffer case if that's not desirable, but we don't block it
  // at the dirty-state layer.
  const isDirty = draft !== initialContent;

  const saveMutation = useMutation({
    mutationFn: (content: string) => editSkillContent(decodedName, content),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["skills"] });
      void queryClient.invalidateQueries({ queryKey: ["skill-article", decodedName] });
      showNotice("Skill saved", "success");
      // Force the seed key to flush so the next render rebases on the
      // new server-side content.
      setSeedKey("");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Could not save skill";
      showNotice(msg, "error");
    },
  });

  const handleSave = useCallback(() => {
    if (!isDirty || saveMutation.isPending) return;
    saveMutation.mutate(draft);
  }, [draft, isDirty, saveMutation]);

  const handleBack = useCallback(() => {
    if (
      isDirty &&
      !window.confirm("Discard unsaved changes to this skill?")
    ) {
      return;
    }
    if (window.history.length > 1) {
      window.history.back();
      return;
    }
    void router.navigate({ to: "/apps/$appId", params: { appId: "skills" } });
  }, [isDirty]);

  // ⌘+S / Ctrl+S saves while focused inside the page.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "s") {
        e.preventDefault();
        handleSave();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [handleSave]);

  return (
    <div className="skill-detail-page">
      <header className="skill-detail-header">
        <button
          type="button"
          className="skill-detail-back"
          onClick={handleBack}
          aria-label="Back to skills"
        >
          <ArrowLeft width={14} height={14} aria-hidden="true" />
          <span>Skills</span>
        </button>
        <div className="skill-detail-title-block">
          <div className="skill-detail-title-row">
            <h1 className="skill-detail-title">
              {skill?.title || decodedName}
            </h1>
            {skill?.status ? (
              <StatusBadge status={skill.status} />
            ) : null}
          </div>
          <div className="skill-detail-meta">
            <span className="skill-detail-slug">{decodedName}</span>
            {skill?.description ? (
              <span className="skill-detail-desc">{skill.description}</span>
            ) : null}
          </div>
        </div>
        <div className="skill-detail-actions">
          <ModeSwitch mode={mode} onChange={setMode} />
          <button
            type="button"
            className="skill-detail-save"
            onClick={handleSave}
            disabled={!isDirty || saveMutation.isPending}
          >
            <FloppyDisk width={14} height={14} aria-hidden="true" />
            <span>
              {saveMutation.isPending ? "Saving…" : isDirty ? "Save" : "Saved"}
            </span>
          </button>
        </div>
      </header>

      <div className="skill-detail-body" data-mode={mode}>
        {articleQuery.isLoading && !draft ? (
          <p className="skill-detail-empty">Loading skill…</p>
        ) : mode === "edit" ? (
          <textarea
            className="skill-detail-editor"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            spellCheck={false}
            autoComplete="off"
            placeholder="# Write the skill body in markdown…"
            aria-label={`Edit SKILL.md for ${decodedName}`}
          />
        ) : (
          <article className="skill-detail-preview">
            {draft.trim() ? (
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {draft}
              </ReactMarkdown>
            ) : (
              <p className="skill-detail-empty">
                Nothing to preview yet — switch to Edit and start writing.
              </p>
            )}
          </article>
        )}
      </div>

      {isDirty ? (
        <footer className="skill-detail-footer">
          <span className="skill-detail-dirty-dot" aria-hidden="true" />
          <span>Unsaved changes — ⌘S to save</span>
        </footer>
      ) : null}
    </div>
  );
}

interface ModeSwitchProps {
  mode: Mode;
  onChange: (mode: Mode) => void;
}

function ModeSwitch({ mode, onChange }: ModeSwitchProps) {
  return (
    <div className="skill-detail-mode-switch" role="tablist">
      <button
        type="button"
        role="tab"
        aria-selected={mode === "edit"}
        className={`skill-detail-mode-btn${mode === "edit" ? " skill-detail-mode-btn--active" : ""}`}
        onClick={() => onChange("edit")}
      >
        <Code width={12} height={12} aria-hidden="true" />
        <span>Edit</span>
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={mode === "preview"}
        className={`skill-detail-mode-btn${mode === "preview" ? " skill-detail-mode-btn--active" : ""}`}
        onClick={() => onChange("preview")}
      >
        <OpenBook width={12} height={12} aria-hidden="true" />
        <span>Preview</span>
      </button>
    </div>
  );
}

interface StatusBadgeProps {
  status: SkillStatus;
}

function StatusBadge({ status }: StatusBadgeProps) {
  return (
    <span
      className={`skill-detail-status skill-detail-status--${status}`}
      data-status={status}
    >
      {status}
    </span>
  );
}

export default SkillDetailRoute;

import { useMemo, useState } from "react";

import { type WikiCatalogEntry, writeHumanArticle } from "../../api/wiki";

type ArticleTemplate =
  | "blank"
  | "person"
  | "company"
  | "project"
  | "decision"
  | "playbook";

const TEMPLATE_LABELS: Record<ArticleTemplate, string> = {
  blank: "Blank page",
  person: "Person",
  company: "Company",
  project: "Project",
  decision: "Decision record",
  playbook: "Playbook",
};

interface NewArticleModalProps {
  catalog: WikiCatalogEntry[];
  onCancel: () => void;
  onCreated: (path: string) => void;
}

/**
 * Modal for creating a new wiki article. Supports choosing a section,
 * optional subfolder, slug, title, and a starter template. POSTs a stub
 * to `/wiki/write-human` with `expected_sha` empty so the broker commits
 * the article as a fresh create.
 */
export default function NewArticleModal({
  catalog,
  onCancel,
  onCreated,
}: NewArticleModalProps) {
  const existingGroups = useMemo(() => {
    const set = new Set<string>();
    for (const e of catalog) set.add(e.group);
    return Array.from(set).sort();
  }, [catalog]);

  const existingPaths = useMemo(
    () => new Set(catalog.map((e) => e.path)),
    [catalog],
  );

  const [group, setGroup] = useState(existingGroups[0] ?? "people");
  const [customGroup, setCustomGroup] = useState("");
  const [subfolder, setSubfolder] = useState("");
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [template, setTemplate] = useState<ArticleTemplate>("blank");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const resolvedGroup = (group === "__custom__" ? customGroup : group).trim();
  const resolvedSubfolder = subfolder.trim();

  const path = useMemo(() => {
    if (!resolvedGroup || !slug) return "";
    const parts = ["team", resolvedGroup];
    if (resolvedSubfolder) parts.push(resolvedSubfolder);
    parts.push(`${slug}.md`);
    return parts.join("/");
  }, [resolvedGroup, resolvedSubfolder, slug]);

  async function handleCreate() {
    setError(null);

    const groupErr = validateSegment(resolvedGroup, "Section");
    if (groupErr) { setError(groupErr); return; }

    if (resolvedSubfolder) {
      const subErr = validateSegment(resolvedSubfolder, "Subfolder");
      if (subErr) { setError(subErr); return; }
    }

    const slugErr = validateSegment(slug, "Slug");
    if (slugErr) { setError(slugErr); return; }

    if (!title.trim()) { setError("Title is required."); return; }

    if (existingPaths.has(path)) {
      setError("An article already exists at that path. Pick a different slug or subfolder.");
      return;
    }

    setSubmitting(true);
    try {
      const result = await writeHumanArticle({
        path,
        content: buildBody(title.trim(), template),
        commitMessage: `human: create ${path}`,
        expectedSha: "",
      });
      if ("conflict" in result) {
        setError("An article already exists at that path. Pick a different slug or subfolder.");
        return;
      }
      onCreated(path);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to create article.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="wk-modal-backdrop"
      data-testid="wk-new-article-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wk-new-article-title"
    >
      <div className="wk-modal">
        <h2 id="wk-new-article-title">New wiki article</h2>

        <label className="wk-editor-label" htmlFor="wk-new-group">
          Section
        </label>
        <select
          id="wk-new-group"
          value={group}
          onChange={(e) => setGroup(e.target.value)}
        >
          {existingGroups.map((g) => (
            <option key={g} value={g}>
              {g}
            </option>
          ))}
          <option value="__custom__">+ New section…</option>
        </select>
        {group === "__custom__" && (
          <input
            className="wk-editor-commit"
            type="text"
            placeholder="e.g. playbooks"
            value={customGroup}
            onChange={(e) => setCustomGroup(e.target.value)}
          />
        )}

        <label className="wk-editor-label" htmlFor="wk-new-subfolder">
          Subfolder <span className="wk-editor-optional">(optional)</span>
        </label>
        <input
          id="wk-new-subfolder"
          className="wk-editor-commit"
          data-testid="wk-new-subfolder"
          type="text"
          placeholder="e.g. engineering"
          value={subfolder}
          onChange={(e) =>
            setSubfolder(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))
          }
        />

        <label className="wk-editor-label" htmlFor="wk-new-slug">
          Slug
        </label>
        <input
          id="wk-new-slug"
          className="wk-editor-commit"
          data-testid="wk-new-slug"
          type="text"
          placeholder="sarah-chen"
          value={slug}
          onChange={(e) =>
            setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))
          }
        />

        <label className="wk-editor-label" htmlFor="wk-new-title">
          Title
        </label>
        <input
          id="wk-new-title"
          className="wk-editor-commit"
          type="text"
          placeholder="Sarah Chen"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />

        <label className="wk-editor-label" htmlFor="wk-new-template">
          Template
        </label>
        <select
          id="wk-new-template"
          data-testid="wk-new-template"
          value={template}
          onChange={(e) => setTemplate(e.target.value as ArticleTemplate)}
        >
          {(Object.keys(TEMPLATE_LABELS) as ArticleTemplate[]).map((t) => (
            <option key={t} value={t}>
              {TEMPLATE_LABELS[t]}
            </option>
          ))}
        </select>

        {path ? (
          <p className="wk-editor-help">
            Will create <code>{path}</code>
          </p>
        ) : null}

        {error ? (
          <div className="wk-editor-banner wk-editor-banner--error" role="alert">
            {error}
          </div>
        ) : null}

        <div className="wk-editor-actions">
          <button
            type="button"
            className="wk-editor-save"
            data-testid="wk-new-create"
            onClick={handleCreate}
            disabled={submitting}
          >
            {submitting ? "Creating…" : "Create article"}
          </button>
          <button
            type="button"
            className="wk-editor-cancel"
            onClick={onCancel}
            disabled={submitting}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

function buildBody(title: string, template: ArticleTemplate): string {
  switch (template) {
    case "person":
      return `# ${title}

## Role

_Describe their role and responsibilities._

## Background

_Key experience and context._

## Contact

_How to reach them._
`;
    case "company":
      return `# ${title}

## Overview

_What this company does and why it matters._

## Key contacts

_Decision-makers and points of contact._

## Status

_Current relationship status and next steps._
`;
    case "project":
      return `# ${title}

## Goal

_What this project is trying to achieve._

## Status

_Current phase and blockers._

## Decisions

_Key decisions made so far._
`;
    case "decision":
      return `# ${title}

## Context

_What situation prompted this decision._

## Decision

_What was decided._

## Rationale

_Why this option over the alternatives._

## Consequences

_What changes as a result._
`;
    case "playbook":
      return `# ${title}

## Trigger

_When to run this playbook._

## Steps

1. _First step._
2. _Second step._

## Definition of done

_How you know it worked._
`;
    default:
      return `# ${title}\n\n_Stub — write something useful here._\n`;
  }
}

/**
 * Mirror of the backend validateArticlePath shape. Rejects traversal,
 * leading slash, empty input, and non-slug characters so the user hears
 * the error before an HTTP round-trip.
 */
function validateSegment(seg: string, label: string): string | null {
  const trimmed = seg.trim();
  if (!trimmed) return `${label} is required.`;
  if (trimmed.startsWith(".") || trimmed.includes(".."))
    return `${label} cannot contain "..".`;
  if (trimmed.includes("/")) return `${label} cannot contain "/".`;
  if (!/^[a-z0-9][a-z0-9-]*$/.test(trimmed)) {
    return `${label} must be lowercase letters, numbers, and hyphens only.`;
  }
  return null;
}

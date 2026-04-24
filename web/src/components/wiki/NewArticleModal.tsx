import { useMemo, useState } from "react";

import { type WikiCatalogEntry, writeHumanArticle } from "../../api/wiki";

interface NewArticleModalProps {
  catalog: WikiCatalogEntry[];
  onCancel: () => void;
  onCreated: (path: string) => void;
}

/**
 * Modal for kicking off a brand-new wiki article. Asks for a section
 * (existing group or free-text), a slug, and a title, then POSTs a
 * single-line draft to `/wiki/write-human` with `expected_sha` empty
 * so the broker commits the article as a fresh create.
 *
 * Intentionally minimal — the heavy lift (markdown editing, headings,
 * wikilinks) happens in the WikiEditor once the article exists.
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

  const [group, setGroup] = useState(existingGroups[0] ?? "people");
  const [customGroup, setCustomGroup] = useState("");
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const resolvedGroup = (group === "__custom__" ? customGroup : group).trim();
  const path = resolvedGroup && slug ? `team/${resolvedGroup}/${slug}.md` : "";

  async function handleCreate() {
    setError(null);
    const groupErr = validateSegment(resolvedGroup, "Section");
    if (groupErr) {
      setError(groupErr);
      return;
    }
    const slugErr = validateSegment(slug, "Slug");
    if (slugErr) {
      setError(slugErr);
      return;
    }
    if (!title.trim()) {
      setError("Title is required.");
      return;
    }

    const fullPath = `team/${resolvedGroup}/${slug}.md`;
    const body = `# ${title.trim()}\n\n_Stub — write something useful here._\n`;

    setSubmitting(true);
    try {
      const result = await writeHumanArticle({
        path: fullPath,
        content: body,
        commitMessage: `human: create ${fullPath}`,
        expectedSha: "",
      });
      if ("conflict" in result) {
        setError(
          "An article already exists at that path. Pick a different slug.",
        );
        return;
      }
      onCreated(fullPath);
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to create article.",
      );
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

        {path && (
          <p className="wk-editor-help">
            Will create <code>{path}</code>
          </p>
        )}

        {error && (
          <div
            className="wk-editor-banner wk-editor-banner--error"
            role="alert"
          >
            {error}
          </div>
        )}

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

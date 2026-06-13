import { useState } from "react";
import ReactMarkdown from "react-markdown";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { EditPencil, NavArrowDown, NavArrowRight, Sparks } from "iconoir-react";
import remarkGfm from "remark-gfm";

import {
  AGENT_INSTRUCTION_FILES,
  type AgentFileResponse,
  type AgentInstructionFile,
  agentFilePath,
  generateAgentFile,
  isAIGeneratableFile,
  OFFICE_USER_FILE_PATH,
  readAgentFile,
  writeAgentFile,
} from "../../api/agentFiles";
import type { OfficeMember } from "../../api/client";
import WikiEditor from "../wiki/WikiEditor";

// One-line descriptions for each instruction file, shown under the file name so
// the human knows what each one governs without opening it.
const FILE_DESCRIPTIONS: Record<AgentInstructionFile, string> = {
  SOUL: "Persona, values, voice, and boundaries",
  IDENTITY: "Name, role, expertise, and runtime",
  OPERATIONS: "How this agent works and escalates",
  TOOLS: "Tool inventory and usage notes",
};

/**
 * Per-file purpose hints (DEFINITION-FILE MATURITY — parity with NousResearch
 * Hermes SOUL.md model). Shown above the editor so the human knows exactly
 * what belongs in each file.
 */
const FILE_PURPOSE_HINTS: Record<AgentInstructionFile | "USER", string> = {
  SOUL: "Persona, voice, values, and hard boundaries — who this agent is. Follows the agent everywhere.",
  IDENTITY:
    "Name, role, expertise, and runtime — the factual record. Mostly derived; edit rarely.",
  OPERATIONS:
    "How this agent works day to day, and when it escalates. The project/operating playbook.",
  TOOLS: "Tool inventory and usage notes — what this agent can do.",
  USER: "The human this office serves — preferences and how to optimize for their time.",
};

interface FileCardConfig {
  path: string;
  label: string;
  description: string;
  /** One-line "what belongs here" blurb shown above the editor. */
  purposeHint?: string;
}

function AgentFileCard({
  path,
  label,
  description,
  purposeHint,
}: FileCardConfig) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [editing, setEditing] = useState(false);
  /**
   * Raw-markdown mode: these are plain .md files and the rich Tiptap
   * WikiEditor normalises markdown / drops HTML comments. Default = true
   * (Raw) so edits are maximally faithful.
   */
  const [rawMode, setRawMode] = useState(true);
  const [rawDraft, setRawDraft] = useState<string | null>(null);
  const [rawSaving, setRawSaving] = useState(false);
  const [rawSaveError, setRawSaveError] = useState<string | null>(null);
  // An LLM-authored draft, held only for the editor session (never written to
  // the query cache, so disk stays the source of truth). When set, the editor
  // opens seeded with it; Save commits it, Cancel discards it.
  const [generatedDraft, setGeneratedDraft] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);
  const [genError, setGenError] = useState<string | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["agent-file", path],
    queryFn: () => readAgentFile(path),
    // Only fetch once the card is opened — keeps the panel light (4-5 files).
    enabled: expanded,
    staleTime: 15_000,
  });

  const toggle = () => {
    setExpanded((v) => {
      const next = !v;
      if (!next) {
        setEditing(false);
        setGeneratedDraft(null);
        setGenError(null);
        setRawDraft(null);
        setRawSaveError(null);
      }
      return next;
    });
  };

  const closeEditor = () => {
    setEditing(false);
    setGeneratedDraft(null);
    setRawDraft(null);
    setRawSaveError(null);
  };

  async function handleRawSave(currentData: AgentFileResponse) {
    if (rawDraft === null) return;
    setRawSaving(true);
    setRawSaveError(null);
    try {
      const result = await writeAgentFile({
        path,
        content: rawDraft,
        commitMessage: `Update ${label}`,
        expectedSha: currentData.sha,
      });
      if ("commit_sha" in result) {
        queryClient.setQueryData(
          ["agent-file", path],
          (old: AgentFileResponse | undefined) =>
            old ? { ...old, sha: result.commit_sha, exists: true } : old,
        );
        void queryClient.invalidateQueries({ queryKey: ["agent-file", path] });
        closeEditor();
      } else {
        // Conflict shape
        setRawSaveError(
          "Conflict: file changed on disk. Close and re-open to reload.",
        );
      }
    } catch (err: unknown) {
      setRawSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setRawSaving(false);
    }
  }

  async function handleGenerate() {
    setGenerating(true);
    setGenError(null);
    try {
      const { content } = await generateAgentFile(path);
      // Open the editor seeded with the draft so the human reviews + saves it.
      // Seed BOTH editor surfaces: rawDraft (the default raw textarea) and
      // generatedDraft (the rich editor, if the human toggles to it). Without
      // seeding rawDraft the draft would be invisible in the default raw mode.
      setGeneratedDraft(content);
      setRawDraft(content);
      setEditing(true);
    } catch (err: unknown) {
      setGenError(err instanceof Error ? err.message : "Generation failed");
    } finally {
      setGenerating(false);
    }
  }

  const canGenerate = isAIGeneratableFile(label);

  return (
    <div className={`agent-file-card${expanded ? " expanded" : ""}`}>
      <button
        type="button"
        className="agent-file-card-header"
        onClick={toggle}
        aria-expanded={expanded}
      >
        <span className="agent-file-card-chevron">
          {expanded ? (
            <NavArrowDown width={14} height={14} />
          ) : (
            <NavArrowRight width={14} height={14} />
          )}
        </span>
        <span className="agent-file-card-titles">
          <span className="agent-file-card-name">{label}</span>
          <span className="agent-file-card-desc">{description}</span>
        </span>
      </button>

      {expanded ? (
        <div className="agent-file-card-body">
          {/* Per-file purpose hint (DEFINITION-FILE MATURITY) */}
          {purposeHint ? (
            <p className="agent-file-purpose-hint">{purposeHint}</p>
          ) : null}

          {/* Raw / Rich mode toggle (shown when viewing or editing) */}
          {data && !isLoading && !isError ? (
            <div className="agent-file-mode-row">
              <fieldset className="agent-file-mode-switch">
                <legend className="sr-only">Editor mode</legend>
                <button
                  type="button"
                  className={`agent-file-mode-btn${rawMode ? " is-active" : ""}`}
                  onClick={() => {
                    setRawMode(true);
                    setEditing(false);
                    setRawDraft(null);
                    setRawSaveError(null);
                  }}
                  aria-pressed={rawMode}
                >
                  Raw
                </button>
                <button
                  type="button"
                  className={`agent-file-mode-btn${!rawMode ? " is-active" : ""}`}
                  onClick={() => {
                    setRawMode(false);
                    setRawDraft(null);
                    setRawSaveError(null);
                  }}
                  aria-pressed={!rawMode}
                >
                  Rich
                </button>
              </fieldset>
            </div>
          ) : null}

          {isLoading ? (
            <div className="agent-file-card-loading">Loading…</div>
          ) : isError ? (
            <div className="agent-file-card-error" role="alert">
              {error instanceof Error ? error.message : "Failed to load file"}
            </div>
          ) : !rawMode && editing && data ? (
            /* Rich / WikiEditor mode */
            <WikiEditor
              path={path}
              initialContent={generatedDraft ?? data.content}
              expectedSha={data.sha}
              writeArticle={writeAgentFile}
              hideWikiHelp={true}
              onSaved={(newSha) => {
                // Promote the new SHA into the cache immediately so re-opening
                // the card before the refetch lands does not seed the editor
                // with a stale expected_sha; then refetch for fresh content.
                queryClient.setQueryData(
                  ["agent-file", path],
                  (old: AgentFileResponse | undefined) =>
                    old ? { ...old, sha: newSha, exists: true } : old,
                );
                void queryClient.invalidateQueries({
                  queryKey: ["agent-file", path],
                });
                closeEditor();
              }}
              onCancel={closeEditor}
            />
          ) : rawMode && editing && data ? (
            /* Raw markdown textarea mode */
            <>
              <textarea
                className="agent-file-raw-editor"
                value={rawDraft ?? data.content}
                onChange={(e) => setRawDraft(e.target.value)}
                disabled={rawSaving}
                aria-label={`Raw markdown editor for ${label}`}
                rows={14}
              />
              {rawSaveError ? (
                <div className="agent-file-card-error" role="alert">
                  {rawSaveError}
                </div>
              ) : null}
              <div className="agent-file-card-actions">
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={closeEditor}
                  disabled={rawSaving}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  onClick={() => void handleRawSave(data)}
                  disabled={
                    rawSaving || rawDraft === null || rawDraft === data.content
                  }
                >
                  {rawSaving ? "Saving…" : "Save"}
                </button>
              </div>
            </>
          ) : data ? (
            <>
              <div className="agent-file-view">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {data.content || "_This file is empty._"}
                </ReactMarkdown>
              </div>
              {genError ? (
                <div className="agent-file-card-error" role="alert">
                  {genError}
                </div>
              ) : null}
              <div className="agent-file-card-actions">
                {!data.exists ? (
                  <span className="agent-file-card-badge">
                    not saved yet — seeded
                  </span>
                ) : null}
                {canGenerate ? (
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm agent-file-generate"
                    onClick={handleGenerate}
                    disabled={generating}
                    title="Draft a richer version with AI for your review"
                  >
                    <Sparks width={13} height={13} />
                    {generating ? "Generating…" : "Generate with AI"}
                  </button>
                ) : null}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm agent-file-edit"
                  onClick={() => {
                    setRawDraft(data.content);
                    setEditing(true);
                  }}
                  disabled={generating}
                >
                  <EditPencil width={13} height={13} />
                  Edit
                </button>
              </div>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

interface AgentInstructionsSectionProps {
  agent: OfficeMember;
}

// AgentInstructionsSection surfaces an agent's instruction file set — the
// SOUL/IDENTITY/OPERATIONS/TOOLS that are loaded into its system prompt — as a
// viewable + editable accordion. The office-wide USER.md (the human the office
// serves) is shown on the lead agent's panel since it is shared by everyone.
export function AgentInstructionsSection({
  agent,
}: AgentInstructionsSectionProps) {
  const isLead = agent.built_in === true || agent.slug === "ceo";

  const files: FileCardConfig[] = AGENT_INSTRUCTION_FILES.map((name) => ({
    path: agentFilePath(agent.slug, name),
    label: name,
    description: FILE_DESCRIPTIONS[name],
    purposeHint: FILE_PURPOSE_HINTS[name],
  }));

  return (
    <div className="agent-profile-section">
      <div className="agent-profile-section-title">instructions</div>
      <p className="agent-instructions-blurb">
        These files shape how this agent thinks and works. Each one is loaded
        into its system prompt — edits take effect on the next turn.
      </p>
      <div className="agent-file-list">
        {files.map((f) => (
          <AgentFileCard key={f.path} {...f} />
        ))}
      </div>

      {isLead ? (
        <div className="agent-file-office">
          <div className="agent-file-office-label">
            office context — shared by all agents
          </div>
          <div className="agent-file-list">
            <AgentFileCard
              path={OFFICE_USER_FILE_PATH}
              label="USER"
              description="The human this office serves"
              purposeHint={FILE_PURPOSE_HINTS.USER}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

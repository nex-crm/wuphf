import { useState } from "react";
import ReactMarkdown from "react-markdown";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { EditPencil, NavArrowDown, NavArrowRight } from "iconoir-react";
import remarkGfm from "remark-gfm";

import {
  AGENT_INSTRUCTION_FILES,
  type AgentFileResponse,
  type AgentInstructionFile,
  agentFilePath,
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

interface FileCardConfig {
  path: string;
  label: string;
  description: string;
}

function AgentFileCard({ path, label, description }: FileCardConfig) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [editing, setEditing] = useState(false);

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
      if (!next) setEditing(false);
      return next;
    });
  };

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
          {isLoading ? (
            <div className="agent-file-card-loading">Loading…</div>
          ) : isError ? (
            <div className="agent-file-card-error" role="alert">
              {error instanceof Error ? error.message : "Failed to load file"}
            </div>
          ) : editing && data ? (
            <WikiEditor
              path={path}
              initialContent={data.content}
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
                setEditing(false);
              }}
              onCancel={() => setEditing(false)}
            />
          ) : data ? (
            <>
              <div className="agent-file-view">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {data.content || "_This file is empty._"}
                </ReactMarkdown>
              </div>
              <div className="agent-file-card-actions">
                {!data.exists ? (
                  <span className="agent-file-card-badge">
                    not saved yet — seeded
                  </span>
                ) : null}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm agent-file-edit"
                  onClick={() => setEditing(true)}
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
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

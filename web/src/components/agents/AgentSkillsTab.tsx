import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Plus, Search, Xmark } from "iconoir-react";

import {
  disableSkillForAgent,
  enableSkillForAgent,
  getSkillsList,
  type Skill,
} from "../../api/client";
import { router } from "../../lib/router";
import { PixelSkillCard } from "../apps/skills/PixelSkillCard";
import { showNotice } from "../ui/Toast";

interface AgentSkillsTabProps {
  agentSlug: string;
  displayName: string;
}

type Mode = "enabled" | "library";

/**
 * Per-agent Skills tab — uses the same PixelSkillCard "collector card"
 * layout the Skills app renders. Two modes:
 *   - Enabled: cards for skills currently in this agent's owner_agents,
 *     each with a Remove action that disables-for-agent.
 *   - Library: every active skill in a card grid with a per-card Add
 *     button (or "Enabled" pill when already enabled for this agent).
 * Same React Query key ["skills","all"] as SkillsApp so toggles here are
 * reflected there instantly and vice versa.
 */
export function AgentSkillsTab({
  agentSlug,
  displayName,
}: AgentSkillsTabProps) {
  const [mode, setMode] = useState<Mode>("enabled");
  const [filter, setFilter] = useState("");
  const queryClient = useQueryClient();

  const { data, isLoading, isError } = useQuery({
    queryKey: ["skills", "all"],
    queryFn: () => getSkillsList("all"),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });

  const allActive: Skill[] = useMemo(
    () => (data?.skills ?? []).filter((s) => s.status === "active"),
    [data],
  );

  const enabled = useMemo(
    () =>
      allActive.filter((s) =>
        Array.isArray(s.owner_agents) && s.owner_agents.includes(agentSlug),
      ),
    [allActive, agentSlug],
  );

  const enabledNames = useMemo(
    () => new Set(enabled.map((s) => s.name)),
    [enabled],
  );

  const library = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return allActive;
    return allActive.filter((s) => {
      const hay = `${s.name} ${s.title ?? ""} ${s.description ?? ""}`.toLowerCase();
      return hay.includes(q);
    });
  }, [allActive, filter]);

  const enableMutation = useMutation({
    mutationFn: (skillName: string) => enableSkillForAgent(skillName, agentSlug),
    onSuccess: (_data, skillName) => {
      void queryClient.invalidateQueries({ queryKey: ["skills"] });
      showNotice(`Enabled ${skillName} for @${agentSlug}`, "success");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Could not enable skill";
      showNotice(msg, "error");
    },
  });

  const disableMutation = useMutation({
    mutationFn: (skillName: string) => disableSkillForAgent(skillName, agentSlug),
    onSuccess: (_data, skillName) => {
      void queryClient.invalidateQueries({ queryKey: ["skills"] });
      showNotice(`Removed ${skillName} from @${agentSlug}`, "info");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Could not disable skill";
      showNotice(msg, "error");
    },
  });

  const visibleCards = mode === "enabled" ? enabled : library;

  return (
    <div className="agent-skills-tab">
      <header className="agent-skills-header">
        <div>
          <h2 className="agent-skills-title">Skills</h2>
          <p className="agent-skills-subtitle">
            {displayName} can invoke the skills enabled below. Browse the
            full library to add more — agents must request enablement
            before they can use a skill they didn't create.
          </p>
        </div>
        <div className="agent-skills-mode-switch" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={mode === "enabled"}
            className={`agent-skills-mode-btn${mode === "enabled" ? " agent-skills-mode-btn--active" : ""}`}
            onClick={() => setMode("enabled")}
          >
            Enabled
            <span className="agent-skills-mode-count">{enabled.length}</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "library"}
            className={`agent-skills-mode-btn${mode === "library" ? " agent-skills-mode-btn--active" : ""}`}
            onClick={() => setMode("library")}
          >
            Library
            <span className="agent-skills-mode-count">{allActive.length}</span>
          </button>
        </div>
      </header>

      {mode === "library" ? (
        <div className="agent-skills-filter">
          <Search
            className="agent-skills-filter-icon"
            width={14}
            height={14}
            aria-hidden="true"
          />
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter skills…"
            className="agent-skills-filter-input"
            aria-label="Filter skills"
          />
        </div>
      ) : null}

      {isLoading ? (
        <p className="agent-skills-empty">Loading skills…</p>
      ) : isError ? (
        <p className="agent-skills-empty agent-skills-empty--error">
          Could not load skills.
        </p>
      ) : visibleCards.length === 0 ? (
        <p className="agent-skills-empty">
          {mode === "enabled"
            ? "No skills enabled yet. Switch to Library to add one."
            : filter
              ? "No skills match that filter."
              : "Library is empty. Skills appear here once an agent creates one or you add one in the Skills app."}
        </p>
      ) : (
        <div className="agent-skills-grid">
          {visibleCards.map((skill) => {
            const isEnabled = enabledNames.has(skill.name);
            return (
              <div key={skill.name} className="agent-skills-card-wrapper">
                <PixelSkillCard
                  skill={skill}
                  onPreview={() =>
                    void router.navigate({
                      to: "/skills/$skillName",
                      params: { skillName: skill.name },
                    })
                  }
                  actions={
                    isEnabled ? (
                      <button
                        type="button"
                        className="agent-skills-card-btn agent-skills-card-btn--remove"
                        onClick={() => disableMutation.mutate(skill.name)}
                        disabled={disableMutation.isPending}
                      >
                        <Xmark width={12} height={12} aria-hidden="true" />
                        <span>Remove from {displayName}</span>
                      </button>
                    ) : (
                      <button
                        type="button"
                        className="agent-skills-card-btn agent-skills-card-btn--add"
                        onClick={() => enableMutation.mutate(skill.name)}
                        disabled={enableMutation.isPending}
                      >
                        <Plus width={12} height={12} aria-hidden="true" />
                        <span>Add to {displayName}</span>
                      </button>
                    )
                  }
                />
                {isEnabled && mode === "library" ? (
                  <span className="agent-skills-card-pill">
                    <Check width={11} height={11} aria-hidden="true" />
                    Enabled
                  </span>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

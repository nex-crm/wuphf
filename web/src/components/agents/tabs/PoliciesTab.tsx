/**
 * Policies tab — list, create, and manage policies that apply to this agent.
 * Shows all global policies plus policies explicitly scoped to this agent.
 * Actions: Add, Remove from agent, Exclude from agent, Deactivate.
 */

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "../../../api/client";
import {
  createPolicy,
  deactivatePolicy,
  type Policy,
  policyAppliesToAgent,
  unassignPolicyAgent,
} from "../../../api/policies";
import { confirm } from "../../ui/ConfirmDialog";

interface PoliciesTabProps {
  agentSlug: string;
}

function sourceBadge(source: string): string {
  if (source === "auto" || source === "agent_generated") return "auto";
  return "human";
}

function isScopedToAgent(policy: Policy, agentSlug: string): boolean {
  return (
    Array.isArray(policy.agents) &&
    policy.agents.length > 0 &&
    policy.agents.includes(agentSlug)
  );
}

function isGlobalPolicy(policy: Policy): boolean {
  return !policy.agents || policy.agents.length === 0;
}

function PolicyRow({
  policy,
  agentSlug,
  onUnassign,
  onDeactivate,
  isMutating,
}: {
  policy: Policy;
  agentSlug: string;
  onUnassign: (id: string) => void;
  onDeactivate: (id: string) => void;
  isMutating: boolean;
}) {
  const global = isGlobalPolicy(policy);
  const scoped = isScopedToAgent(policy, agentSlug);

  return (
    <div className="agent-policy-row">
      <div className="agent-policy-row-body">
        <p className="agent-policy-rule">{policy.rule}</p>
        <div className="agent-policy-meta">
          <span
            className={`agent-policy-badge agent-policy-badge--source-${sourceBadge(policy.source)}`}
          >
            {sourceBadge(policy.source)}
          </span>
          {global ? (
            <span className="agent-policy-badge agent-policy-badge--scope">
              all agents
            </span>
          ) : (
            <span className="agent-policy-badge agent-policy-badge--scope agent-policy-badge--scoped">
              scoped
            </span>
          )}
        </div>
      </div>
      <div className="agent-policy-actions">
        {scoped ? (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={isMutating}
            onClick={() => onUnassign(policy.id)}
            title="Remove this policy from this agent"
          >
            Remove from @{agentSlug}
          </button>
        ) : global ? (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={isMutating}
            onClick={() => onUnassign(policy.id)}
            title="Exclude this agent from the policy"
          >
            Exclude @{agentSlug}
          </button>
        ) : null}
        <button
          type="button"
          className="btn btn-ghost btn-sm agent-policy-deactivate"
          disabled={isMutating}
          onClick={() => onDeactivate(policy.id)}
          title="Deactivate this policy"
        >
          Deactivate
        </button>
      </div>
    </div>
  );
}

export function PoliciesTab({ agentSlug }: PoliciesTabProps) {
  const queryClient = useQueryClient();
  const [newRule, setNewRule] = useState("");
  const [addError, setAddError] = useState<string | null>(null);
  const [mutateError, setMutateError] = useState<string | null>(null);

  const {
    data: allPolicies = [],
    isLoading,
    isError,
  } = useQuery({
    queryKey: ["policies"],
    queryFn: () => import("../../../api/policies").then((m) => m.getPolicies()),
    refetchInterval: 30_000,
  });

  const applicablePolicies = allPolicies.filter(
    (p) => p.active && policyAppliesToAgent(p, agentSlug),
  );

  function invalidate() {
    void queryClient.invalidateQueries({ queryKey: ["policies"] });
  }

  const addMutation = useMutation({
    mutationFn: (rule: string) =>
      createPolicy({ rule, source: "human_directed", agents: [agentSlug] }),
    onSuccess: () => {
      setNewRule("");
      setAddError(null);
      invalidate();
    },
    onError: (err: unknown) => {
      setAddError(err instanceof Error ? err.message : "Failed to add policy");
    },
  });

  const unassignMutation = useMutation({
    mutationFn: (id: string) => unassignPolicyAgent(id, agentSlug),
    onSuccess: () => {
      setMutateError(null);
      invalidate();
    },
    onError: (err: unknown) => {
      // 409 = server says "can't unassign last agent" — human-readable
      if (err instanceof ApiError && err.status === 409) {
        setMutateError(err.message);
      } else {
        setMutateError(
          err instanceof Error ? err.message : "Failed to update policy",
        );
      }
    },
  });

  const deactivateMutation = useMutation({
    mutationFn: (id: string) => deactivatePolicy(id),
    onSuccess: () => {
      setMutateError(null);
      invalidate();
    },
    onError: (err: unknown) => {
      setMutateError(
        err instanceof Error ? err.message : "Failed to deactivate policy",
      );
    },
  });

  function handleDeactivate(id: string) {
    const policy = allPolicies.find((p) => p.id === id);
    confirm({
      title: "Deactivate policy",
      message: policy
        ? `Deactivate this policy? "${policy.rule.slice(0, 80)}${policy.rule.length > 80 ? "…" : ""}"`
        : "Deactivate this policy?",
      confirmLabel: "Deactivate",
      danger: true,
      onConfirm: async () => {
        deactivateMutation.mutate(id);
      },
    });
  }

  const isMutating = unassignMutation.isPending || deactivateMutation.isPending;

  return (
    <div className="agent-policies-tab" data-testid="policies-tab">
      <div className="agent-policies-header">
        <h2 className="agent-policies-title">Policies</h2>
        <p className="agent-policies-subtitle">
          Rules that govern how @{agentSlug} behaves. Global policies apply to
          all agents; scoped policies apply only to those listed.
        </p>
      </div>

      {/* Add policy form */}
      <div className="agent-policies-add">
        <div className="agent-policies-add-title">
          Add policy for @{agentSlug}
        </div>
        <div className="agent-policies-add-row">
          <input
            className="input agent-policies-input"
            type="text"
            placeholder="e.g. Always cite sources when referencing external data"
            value={newRule}
            onChange={(e) => setNewRule(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && newRule.trim()) {
                e.preventDefault();
                addMutation.mutate(newRule.trim());
              }
            }}
            disabled={addMutation.isPending}
            aria-label="New policy rule"
          />
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={!newRule.trim() || addMutation.isPending}
            onClick={() => addMutation.mutate(newRule.trim())}
          >
            {addMutation.isPending ? "Adding…" : "Add policy"}
          </button>
        </div>
        {addError ? (
          <p className="agent-policies-error" role="alert">
            {addError}
          </p>
        ) : null}
      </div>

      {mutateError ? (
        <p className="agent-policies-error" role="alert">
          {mutateError}
        </p>
      ) : null}

      {/* Policy list */}
      {isLoading ? (
        <p className="agent-policies-empty">Loading policies…</p>
      ) : isError ? (
        <p className="agent-policies-error" role="alert">
          Couldn't load policies. Check your connection and try again.
        </p>
      ) : applicablePolicies.length === 0 ? (
        <p className="agent-policies-empty">
          No active policies apply to @{agentSlug}.
        </p>
      ) : (
        <ul className="agent-policy-list" aria-label="Applicable policies">
          {applicablePolicies.map((policy) => (
            <li key={policy.id}>
              <PolicyRow
                policy={policy}
                agentSlug={agentSlug}
                onUnassign={(id) => unassignMutation.mutate(id)}
                onDeactivate={handleDeactivate}
                isMutating={isMutating}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

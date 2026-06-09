import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type ActionGrant,
  getActionGrants,
  revokeActionGrant,
} from "../../../api/client";
import { showNotice } from "../../ui/Toast";
import { GenericIntegrationLogo, ToolkitBrandLogo } from "./IntegrationLogos";

// ActionGrantsPanel is the revoke surface for scoped action grants
// (deterministic-integrations slice 5b). Each grant is a standing "always allow"
// the human created from an approval card; this panel makes those visible and
// revocable in one place. It renders nothing when there are no active grants, so
// it stays out of the way until grants exist.

function formatGrantedAt(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  return new Date(t).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

function GrantRow({
  grant,
  onRevoke,
  revoking,
}: {
  grant: ActionGrant;
  onRevoke: (id: string) => void;
  revoking: boolean;
}) {
  const brand = grant.platform ? (
    <ToolkitBrandLogo platform={grant.platform} />
  ) : null;
  const granted = formatGrantedAt(grant.granted_at);
  return (
    <li className="grant-row">
      <span className="grant-logo" aria-hidden="true">
        {brand ?? <GenericIntegrationLogo />}
      </span>
      <div className="grant-row-main">
        <span className="grant-scope mono">{grant.action_scope}</span>
        <span className="grant-meta">
          <span className="grant-agent">@{grant.agent_slug}</span>
          <span className="grant-sep" aria-hidden="true">
            ·
          </span>
          <span>{grant.platform}</span>
          {granted ? (
            <>
              <span className="grant-sep" aria-hidden="true">
                ·
              </span>
              <span>since {granted}</span>
            </>
          ) : null}
        </span>
      </div>
      <button
        type="button"
        className="btn btn-sm btn-ghost grant-revoke"
        onClick={() => onRevoke(grant.id)}
        disabled={revoking}
      >
        Revoke
      </button>
    </li>
  );
}

export function ActionGrantsPanel() {
  const queryClient = useQueryClient();
  const grantsQuery = useQuery({
    queryKey: ["action-grants"],
    queryFn: getActionGrants,
    staleTime: 10_000,
  });

  const revokeMutation = useMutation({
    mutationFn: (id: string) => revokeActionGrant(id),
    onSuccess: () => {
      showNotice("Grant revoked. That action will ask again next time.", "info");
      void queryClient.invalidateQueries({ queryKey: ["action-grants"] });
    },
    onError: (err: unknown) => {
      showNotice(
        err instanceof Error ? err.message : "Failed to revoke grant",
        "error",
      );
    },
  });

  const grants = grantsQuery.data?.grants ?? [];
  if (grantsQuery.isLoading || grants.length === 0) return null;

  return (
    <section className="op-category grants-panel" aria-label="Always-allowed actions">
      <div className="grants-panel-head">
        <h3 className="grants-panel-title">Always-allowed actions</h3>
        <p className="grants-panel-sub">
          Actions approved to run without asking again. Revoke any to restore the
          approval prompt.
        </p>
      </div>
      <ul className="grant-list">
        {grants.map((grant) => (
          <GrantRow
            key={grant.id}
            grant={grant}
            onRevoke={(id) => revokeMutation.mutate(id)}
            // Disable only the row being revoked so other grants stay actionable.
            revoking={
              revokeMutation.isPending && revokeMutation.variables === grant.id
            }
          />
        ))}
      </ul>
    </section>
  );
}

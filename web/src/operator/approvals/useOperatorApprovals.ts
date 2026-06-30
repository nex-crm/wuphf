// Operator approval surface — polls the broker for pending human-approval
// requests (e.g. an app's mutating Slack send) and exposes approve/reject.
// This is the human-in-the-loop the safety model requires: the operator never
// self-approves; it surfaces the request so the human clicks Approve.

import { useCallback } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type AgentRequest,
  answerRequest,
  getAllRequests,
} from "../../api/client";

const APPROVALS_POLL_MS = 3000;

/** Requests the operator should surface as an approval card: an APP ACTION
 * needing the human's sign-off (e.g. a mutating Slack/Gmail call). We exclude
 * office-task plan approvals — those belong to the office flow, not the operator
 * surface, and would otherwise nag here. */
function isPendingApproval(r: AgentRequest): boolean {
  const status = (r.status ?? "").toLowerCase();
  if (
    status === "answered" ||
    status === "resolved" ||
    status === "cancelled"
  ) {
    return false;
  }
  const opts = r.options ?? r.choices ?? [];
  const isApproval =
    r.kind === "approval" || opts.some((o) => o.id === "approve");
  if (!isApproval) return false;
  // Drop build/plan approvals (office task flow): "Plan ready for … approve to
  // start execution". The operator only owns app-action approvals.
  const q = (r.question ?? "").toLowerCase();
  if (q.startsWith("plan ready") || q.includes("approve to start execution")) {
    return false;
  }
  return true;
}

export function useOperatorApprovals() {
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["operator-approvals"],
    queryFn: getAllRequests,
    refetchInterval: APPROVALS_POLL_MS,
  });

  const pending = (query.data?.requests ?? []).filter(isPendingApproval);

  const answer = useMutation({
    mutationFn: ({ id, choiceId }: { id: string; choiceId: string }) =>
      answerRequest(id, choiceId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["operator-approvals"] }),
  });

  const approve = useCallback(
    (id: string) => answer.mutate({ id, choiceId: "approve" }),
    [answer],
  );
  const reject = useCallback(
    (id: string) => answer.mutate({ id, choiceId: "reject" }),
    [answer],
  );

  return { pending, approve, reject, answering: answer.isPending };
}

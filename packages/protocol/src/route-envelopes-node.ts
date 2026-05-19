import { formatValidationErrors } from "./receipt-utils.ts";

export {
  approvalDecisionRequestFromJson,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalDecisionResponseToJsonValue,
  approvalGetResponseFromJson,
  approvalGetResponseToJsonValue,
  approvalListResponseFromJson,
  approvalListResponseToJsonValue,
  approvalRequestCreateRequestFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  approvalRequestCreateResponseToJsonValue,
  approvalViewFromJson,
  approvalViewToJsonValue,
  ROUTE_ENVELOPE_SCHEMA_VERSION,
  routeErrorFromJson,
  routeErrorToJsonValue,
  THREAD_ATTENTION_REASON_VALUES,
  THREAD_BOARD_COLUMN_VALUES,
  THREAD_CURRENT_SEAT_VALUES,
  THREAD_EFFECTIVE_STATUS_VALUES,
  threadCreateRequestFromJson,
  threadCreateRequestToJsonValue,
  threadGetResponseToJsonValue,
  threadListResponseToJsonValue,
  threadMutationResponseFromJson,
  threadMutationResponseToJsonValue,
  threadPinnedApprovalsResponseFromJson,
  threadPinnedApprovalsResponseToJsonValue,
  threadSpecEditRequestFromJson,
  threadSpecEditRequestToJsonValue,
  threadStatusChangeRequestFromJson,
  threadStatusChangeRequestToJsonValue,
  threadViewToJsonValue,
  validateApprovalView,
} from "./route-envelopes.ts";

import {
  threadGetResponseFromJson as browserThreadGetResponseFromJson,
  threadListResponseFromJson as browserThreadListResponseFromJson,
  type ThreadGetResponse,
  type ThreadListResponse,
  type ThreadView,
} from "./route-envelopes.ts";
import { validateThreadSpecRevision } from "./thread.ts";
import { threadViewFromJson as browserThreadViewFromJson } from "./thread-route-view.ts";

export function threadViewFromJson(value: unknown): ThreadView {
  const view = browserThreadViewFromJson(value);
  assertNodeThreadView(view);
  return view;
}

export function threadListResponseFromJson(value: unknown): ThreadListResponse {
  const response = browserThreadListResponseFromJson(value);
  for (const thread of response.threads) {
    assertNodeThreadView(thread);
  }
  return response;
}

export function threadGetResponseFromJson(value: unknown): ThreadGetResponse {
  const response = browserThreadGetResponseFromJson(value);
  assertNodeThreadView(response.thread);
  return response;
}

function assertNodeThreadView(view: ThreadView): void {
  const specValidation = validateThreadSpecRevision(view.spec);
  if (!specValidation.ok) {
    throw new Error(formatValidationErrors(specValidation.errors));
  }
}

import type {
  BudgetSetAuditPayload,
  BudgetThresholdCrossedAuditPayload,
  CostEventAuditPayload,
} from "../src/index.ts";
import { costAuditPayloadToBytes, costAuditPayloadToJsonValue } from "../src/index.ts";

declare const costEventPayload: CostEventAuditPayload;
declare const budgetSetPayload: BudgetSetAuditPayload;
declare const crossingPayload: BudgetThresholdCrossedAuditPayload;

costAuditPayloadToJsonValue("cost_event", costEventPayload);
costAuditPayloadToJsonValue("budget_set", budgetSetPayload);
costAuditPayloadToJsonValue("budget_threshold_crossed", crossingPayload);

costAuditPayloadToBytes("cost_event", costEventPayload);
costAuditPayloadToBytes("budget_set", budgetSetPayload);
costAuditPayloadToBytes("budget_threshold_crossed", crossingPayload);

// @ts-expect-error kind and payload must match.
costAuditPayloadToJsonValue("cost_event", budgetSetPayload);

// @ts-expect-error kind and payload must match.
costAuditPayloadToJsonValue("budget_set", crossingPayload);

// @ts-expect-error kind and payload must match.
costAuditPayloadToBytes("budget_threshold_crossed", costEventPayload);

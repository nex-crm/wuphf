import {
  asAgentSlug,
  asMicroUsd,
  type CostLedgerEntry,
  type CostUnits,
  MAX_COST_MODEL_BYTES,
  type MicroUsd,
  type ProviderKind,
  type ReceiptId,
  type RunnerSpawnRequest,
  type TaskId,
} from "@wuphf/protocol";

import { RunnerFailure } from "./cleanup.ts";

export interface ValidatedCostInput {
  readonly request: RunnerSpawnRequest;
  readonly providerKind: ProviderKind;
  readonly defaultModel: string;
  readonly reportedModel?: string | undefined;
  readonly amountMicroUsd: number;
  readonly units: CostUnits;
  readonly occurredAt: Date;
  readonly receiptId: ReceiptId;
  readonly taskId: TaskId;
}

export function trustedCostModel(input: {
  readonly request: RunnerSpawnRequest;
  readonly defaultModel: string;
}): string {
  const candidate = input.request.model ?? input.defaultModel;
  return Buffer.byteLength(candidate, "utf8") <= MAX_COST_MODEL_BYTES
    ? candidate
    : input.defaultModel;
}

export function validatedCostEntry(input: ValidatedCostInput): CostLedgerEntry {
  assertCostUnits(input.units);
  if (!Number.isSafeInteger(input.amountMicroUsd) || input.amountMicroUsd < 0) {
    throw new RunnerFailure(
      "cost amount must be a non-negative safe integer",
      "provider_returned_error",
    );
  }
  if (
    input.request.costCeilingMicroUsd !== undefined &&
    input.amountMicroUsd > input.request.costCeilingMicroUsd
  ) {
    throw new RunnerFailure(
      "cost amount exceeded request costCeilingMicroUsd",
      "cost_ceiling_exceeded",
    );
  }
  return {
    receiptId: input.receiptId,
    agentSlug: asAgentSlug(input.request.agentId),
    taskId: input.taskId,
    providerKind: input.providerKind,
    model: trustedCostModel({
      request: input.request,
      defaultModel: input.defaultModel,
    }),
    amountMicroUsd: asMicroUsd(input.amountMicroUsd) as MicroUsd,
    units: input.units,
    occurredAt: input.occurredAt,
  };
}

export function assertCostUnits(units: CostUnits): void {
  for (const [key, value] of Object.entries(units)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RunnerFailure(
        `${key} must be a non-negative safe integer`,
        "provider_returned_error",
      );
    }
  }
}

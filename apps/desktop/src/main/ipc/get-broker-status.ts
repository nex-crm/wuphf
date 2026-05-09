import type { IpcMainInvokeEvent } from "electron";

import type { BrokerStatus, ErrResponse, GetBrokerStatusResponse } from "../../shared/api-contract.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";

export interface BrokerStatusProvider {
  getStatus(): BrokerStatus;
  getPid(): number | null;
  getRestartCount(): number;
}

export function handleGetBrokerStatus(
  brokerSupervisor: BrokerStatusProvider,
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetBrokerStatusResponse | ErrResponse {
  const validation = validateEmptyRequest(request, "getBrokerStatus");
  if (!validation.valid) {
    return invalidRequest(validation.error);
  }

  return {
    status: brokerSupervisor.getStatus(),
    pid: brokerSupervisor.getPid(),
    restartCount: brokerSupervisor.getRestartCount(),
  };
}

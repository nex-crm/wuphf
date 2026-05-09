import type { IpcMainInvokeEvent } from "electron";

import type { BrokerStatus, GetBrokerStatusResponse } from "../../shared/api-contract.ts";
import { assertEmptyRequest } from "./_guards.ts";

export interface BrokerStatusProvider {
  getStatus(): BrokerStatus;
  getPid(): number | null;
  getRestartCount(): number;
}

export function handleGetBrokerStatus(
  brokerSupervisor: BrokerStatusProvider,
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetBrokerStatusResponse {
  assertEmptyRequest(request, "getBrokerStatus");
  return {
    status: brokerSupervisor.getStatus(),
    pid: brokerSupervisor.getPid(),
    restartCount: brokerSupervisor.getRestartCount(),
  };
}

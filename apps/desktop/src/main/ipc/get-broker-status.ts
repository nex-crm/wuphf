import type { IpcMainInvokeEvent } from "electron";

import type {
  BrokerSnapshot,
  ErrResponse,
  GetBrokerStatusResponse,
} from "../../shared/api-contract.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";

export interface BrokerStatusProvider {
  getSnapshot(): BrokerSnapshot;
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

  return brokerSupervisor.getSnapshot();
}

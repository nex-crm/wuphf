import type { IpcMainInvokeEvent } from "electron";

import {
  type BrokerSnapshot,
  type ErrResponse,
  type GetBrokerStatusResponse,
  IpcChannel,
} from "../../shared/api-contract.ts";
import type { Logger } from "../logger.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";
import { logIpcPayloadRejected } from "./_logging.ts";

export interface BrokerStatusProvider {
  getSnapshot(): BrokerSnapshot;
}

export interface GetBrokerStatusHandlerOptions {
  readonly logger?: Logger;
}

export function createGetBrokerStatusHandler(
  brokerSupervisor: BrokerStatusProvider,
  options: GetBrokerStatusHandlerOptions = {},
): (event: IpcMainInvokeEvent, request: unknown) => GetBrokerStatusResponse | ErrResponse {
  return function getBrokerStatusHandler(
    _event: IpcMainInvokeEvent,
    request: unknown,
  ): GetBrokerStatusResponse | ErrResponse {
    const validation = validateEmptyRequest(request, "getBrokerStatus");
    if (!validation.valid) {
      logIpcPayloadRejected(options.logger, IpcChannel.GetBrokerStatus, "invalid_request");
      return invalidRequest(validation.error);
    }

    return brokerSupervisor.getSnapshot();
  };
}

export function handleGetBrokerStatus(
  brokerSupervisor: BrokerStatusProvider,
  event: IpcMainInvokeEvent,
  request: unknown,
): GetBrokerStatusResponse | ErrResponse {
  return createGetBrokerStatusHandler(brokerSupervisor)(event, request);
}

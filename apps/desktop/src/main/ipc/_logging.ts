import type { IpcChannelName } from "../../shared/api-contract.ts";
import type { Logger, LogPayload } from "../logger.ts";

export function logIpcPayloadRejected(
  logger: Logger | undefined,
  channel: IpcChannelName,
  reason: string,
  payload: LogPayload = {},
): void {
  logger?.warn("ipc_payload_rejected", {
    channel,
    reason,
    ...payload,
  });
}

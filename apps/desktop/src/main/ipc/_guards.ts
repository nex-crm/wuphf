import { type EmptyPayload, type ErrResponse, errResponse } from "../../shared/api-contract.ts";

export function isExactObject(
  value: unknown,
  keys: readonly string[],
): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }

  const actualKeys = Object.keys(value);
  if (actualKeys.length !== keys.length) {
    return false;
  }

  return keys.every((key) => Object.hasOwn(value, key));
}

export function invalidRequest(error: string): ErrResponse {
  return errResponse(error);
}

export function assertEmptyRequest(
  request: unknown,
  channelName: string,
): asserts request is EmptyPayload {
  if (!isExactObject(request, [])) {
    throw new Error(`${channelName} expects an empty request object`);
  }
}

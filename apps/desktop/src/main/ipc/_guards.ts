import { type ErrResponse, errResponse } from "../../shared/api-contract.ts";

export type RequestValidation =
  | { readonly valid: true }
  | { readonly valid: false; readonly error: string };

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

export function validateEmptyRequest(request: unknown, channelName: string): RequestValidation {
  if (!isExactObject(request, [])) {
    return { valid: false, error: `${channelName} expects an empty request object` };
  }

  return { valid: true };
}

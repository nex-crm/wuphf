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

export function assertMaxStringLength(
  value: unknown,
  maxBytes: number,
  field: string,
): RequestValidation {
  if (typeof value !== "string") {
    return { valid: false, error: `${field} must be a string` };
  }

  if (Buffer.byteLength(value, "utf8") > maxBytes) {
    return { valid: false, error: `${field} must be at most ${maxBytes} bytes` };
  }

  return { valid: true };
}

export function validateEmptyRequest(request: unknown, channelName: string): RequestValidation {
  if (!isExactObject(request, [])) {
    return { valid: false, error: `${channelName} expects an empty request object` };
  }

  return { valid: true };
}

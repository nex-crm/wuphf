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

export function invalidRequest(error: string): { readonly ok: false; readonly error: string } {
  return { ok: false, error };
}

export function assertEmptyRequest(request: unknown, channelName: string): void {
  if (!isExactObject(request, [])) {
    throw new Error(`${channelName} expects an empty request object`);
  }
}

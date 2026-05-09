import { type IpcMainInvokeEvent, shell } from "electron";

import type { OpenExternalResponse } from "../../shared/api-contract.ts";
import { invalidRequest, isExactObject } from "./_guards.ts";

const ALLOWED_EXTERNAL_PROTOCOLS = new Set(["https:", "http:", "mailto:"]);
interface ValidOpenExternalRequest {
  readonly url: string;
}

export async function handleOpenExternal(
  _event: IpcMainInvokeEvent,
  request: unknown,
): Promise<OpenExternalResponse> {
  if (!isOpenExternalRequest(request)) {
    return invalidRequest("openExternal expects exactly one string field: url");
  }

  const parsedUrl = parseAllowedExternalUrl(request.url);
  if (!parsedUrl.ok) {
    return parsedUrl;
  }

  try {
    await shell.openExternal(parsedUrl.url);
    return { ok: true };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "Failed to open external URL",
    };
  }
}

function parseAllowedExternalUrl(
  value: string,
): { readonly ok: true; readonly url: string } | { readonly ok: false; readonly error: string } {
  let parsedUrl: URL;
  try {
    parsedUrl = new URL(value);
  } catch {
    return { ok: false, error: "Invalid URL" };
  }

  if (!ALLOWED_EXTERNAL_PROTOCOLS.has(parsedUrl.protocol)) {
    return { ok: false, error: `Unsupported external URL protocol: ${parsedUrl.protocol}` };
  }

  return { ok: true, url: parsedUrl.toString() };
}

function isOpenExternalRequest(request: unknown): request is ValidOpenExternalRequest {
  return (
    isExactObject(request, ["url"]) &&
    typeof (request as { readonly url?: unknown }).url === "string"
  );
}

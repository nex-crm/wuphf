import { type IpcMainInvokeEvent, shell } from "electron";

import {
  type ErrResponse,
  errResponse,
  type OpenExternalResponse,
  okResponse,
} from "../../shared/api-contract.ts";
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
    return okResponse();
  } catch (error) {
    return errResponse(error instanceof Error ? error.message : "Failed to open external URL");
  }
}

function parseAllowedExternalUrl(
  value: string,
): { readonly ok: true; readonly url: string } | ErrResponse {
  let parsedUrl: URL;
  try {
    parsedUrl = new URL(value);
  } catch {
    return errResponse("Invalid URL");
  }

  if (!ALLOWED_EXTERNAL_PROTOCOLS.has(parsedUrl.protocol)) {
    return errResponse(`Unsupported external URL protocol: ${parsedUrl.protocol}`);
  }

  return { ok: true, url: parsedUrl.toString() };
}

function isOpenExternalRequest(request: unknown): request is ValidOpenExternalRequest {
  return (
    isExactObject(request, ["url"]) &&
    typeof (request as { readonly url?: unknown }).url === "string"
  );
}

import { type IpcMainInvokeEvent, shell } from "electron";

import {
  type ErrResponse,
  errResponse,
  type OpenExternalResponse,
  okResponse,
} from "../../shared/api-contract.ts";
import { monotonicNowMs } from "../monotonic-clock.ts";
import { assertMaxStringLength, invalidRequest, isExactObject } from "./_guards.ts";

const ALLOWED_EXTERNAL_PROTOCOLS = new Set(["https:", "http:", "mailto:"]);
const MAX_EXTERNAL_URL_BYTES = 8_192;
const OPEN_EXTERNAL_RATE_LIMIT_MAX_CALLS = 5;
const OPEN_EXTERNAL_RATE_LIMIT_WINDOW_MS = 10_000;

type MonotonicNow = () => number;
type OpenExternalHandler = (
  event: IpcMainInvokeEvent,
  request: unknown,
) => Promise<OpenExternalResponse>;

interface ValidOpenExternalRequest {
  readonly url: string;
}

export interface OpenExternalHandlerOptions {
  readonly monotonicNow?: MonotonicNow;
}

export function createOpenExternalHandler(
  options: OpenExternalHandlerOptions = {},
): OpenExternalHandler {
  const rateLimiter = createRateLimiter({
    maxCalls: OPEN_EXTERNAL_RATE_LIMIT_MAX_CALLS,
    windowMs: OPEN_EXTERNAL_RATE_LIMIT_WINDOW_MS,
    monotonicNow: options.monotonicNow ?? monotonicNowMs,
  });

  return async function openExternalHandler(
    _event: IpcMainInvokeEvent,
    request: unknown,
  ): Promise<OpenExternalResponse> {
    if (!isOpenExternalRequest(request)) {
      return invalidRequest("openExternal expects exactly one string field: url");
    }

    const sizeValidation = assertMaxStringLength(request.url, MAX_EXTERNAL_URL_BYTES, "url");
    if (!sizeValidation.valid) {
      return invalidRequest(sizeValidation.error);
    }

    const parsedUrl = parseAllowedExternalUrl(request.url);
    if (!parsedUrl.ok) {
      return parsedUrl;
    }

    if (!rateLimiter.tryAcquire()) {
      return errResponse("rate_limited");
    }

    try {
      await shell.openExternal(parsedUrl.url);
      return okResponse();
    } catch (error) {
      return errResponse(error instanceof Error ? error.message : "Failed to open external URL");
    }
  };
}

export const handleOpenExternal = createOpenExternalHandler();

function createRateLimiter(config: {
  readonly maxCalls: number;
  readonly windowMs: number;
  readonly monotonicNow: MonotonicNow;
}): { readonly tryAcquire: () => boolean } {
  let callTimesMs: readonly number[] = [];

  return {
    tryAcquire: () => {
      const nowMs = config.monotonicNow();
      const windowStartMs = nowMs - config.windowMs;
      callTimesMs = callTimesMs.filter((callTimeMs) => callTimeMs > windowStartMs);
      if (callTimesMs.length >= config.maxCalls) {
        return false;
      }

      callTimesMs = [...callTimesMs, nowMs];
      return true;
    },
  };
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

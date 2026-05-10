import { type IpcMainInvokeEvent, shell } from "electron";

import {
  errResponse,
  IpcChannel,
  type OpenExternalResponse,
  okResponse,
} from "../../shared/api-contract.ts";
import type { Logger, LogPayload } from "../logger.ts";
import { monotonicNowMs } from "../monotonic-clock.ts";
import { assertMaxStringLength, invalidRequest, isExactObject } from "./_guards.ts";
import { logIpcPayloadRejected } from "./_logging.ts";

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
  readonly logger?: Logger;
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
      logRejection(options.logger, "invalid_request");
      return invalidRequest("openExternal expects exactly one string field: url");
    }

    const sizeValidation = assertMaxStringLength(request.url, MAX_EXTERNAL_URL_BYTES, "url");
    if (!sizeValidation.valid) {
      logRejection(options.logger, "oversized_url", {
        payloadBytes: Buffer.byteLength(request.url, "utf8"),
      });
      return invalidRequest(sizeValidation.error);
    }

    const parsedUrl = parseAllowedExternalUrl(request.url);
    if (!parsedUrl.ok) {
      logRejection(
        options.logger,
        parsedUrl.reason,
        typeof parsedUrl.scheme === "string" ? { scheme: parsedUrl.scheme } : {},
      );
      return errResponse(parsedUrl.error);
    }

    if (!rateLimiter.tryAcquire()) {
      logRejection(options.logger, "rate_limited");
      return errResponse("rate_limited");
    }

    try {
      await shell.openExternal(parsedUrl.url);
      return okResponse();
    } catch (error) {
      return errResponse(error instanceof Error ? error.message : "shell.openExternal rejected");
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

function parseAllowedExternalUrl(value: string):
  | { readonly ok: true; readonly url: string }
  | {
      readonly ok: false;
      readonly error: string;
      readonly reason: string;
      readonly scheme?: string;
    } {
  let parsedUrl: URL;
  try {
    parsedUrl = new URL(value);
  } catch {
    return { ok: false, error: "Invalid URL", reason: "invalid_url" };
  }

  if (!ALLOWED_EXTERNAL_PROTOCOLS.has(parsedUrl.protocol)) {
    return {
      ok: false,
      error: `Unsupported external URL protocol: ${parsedUrl.protocol}`,
      reason: "unsupported_scheme",
      scheme: parsedUrl.protocol,
    };
  }

  return { ok: true, url: parsedUrl.toString() };
}

function logRejection(logger: Logger | undefined, reason: string, payload: LogPayload = {}): void {
  logIpcPayloadRejected(logger, IpcChannel.OpenExternal, reason, payload);
}

function isOpenExternalRequest(request: unknown): request is ValidOpenExternalRequest {
  return (
    isExactObject(request, ["url"]) &&
    typeof (request as { readonly url?: unknown }).url === "string"
  );
}

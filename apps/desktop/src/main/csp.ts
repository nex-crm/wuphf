import type { HeadersReceivedResponse, OnHeadersReceivedListenerDetails, Session } from "electron";

const CSP_HEADER_NAME = "Content-Security-Policy";

export const BASE_RENDERER_CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

// Vite dev mode needs an inline preamble script and a websocket HMR channel.
// Loosen `script-src`, `style-src`, and `connect-src` only when serving the
// dev renderer; the packaged build, `electron-vite preview`, and the
// broker-served renderer path are always strict.
export const DEV_RENDERER_CSP =
  "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws://localhost:5173 http://localhost:5173; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

export interface InstallDynamicRendererCspOptions {
  /**
   * The validated Vite dev-server origin (scheme + host + port, no path).
   * When the request URL's origin matches this value, the listener emits
   * `DEV_RENDERER_CSP`. Every other response — including
   * `electron-vite preview`, the broker-served renderer, and packaged
   * loads — gets `BASE_RENDERER_CSP`.
   *
   * Defaults to `null` (strict CSP for every response). Main process
   * derives this from `ELECTRON_RENDERER_URL` and only sets it when
   * `app.isPackaged === false`; preview does not set
   * `ELECTRON_RENDERER_URL` so it stays strict by construction.
   */
  readonly devRendererOrigin?: string | null;
}

export function installDynamicRendererCsp(
  electronSession: Session,
  readBrokerUrl: () => string | null,
  options: InstallDynamicRendererCspOptions = {},
): void {
  electronSession.webRequest.onHeadersReceived(
    createDynamicRendererCspHeadersReceivedListener(readBrokerUrl, options),
  );
}

export function createDynamicRendererCspHeadersReceivedListener(
  readBrokerUrl: () => string | null,
  options: InstallDynamicRendererCspOptions = {},
) {
  const devRendererOrigin = normalizeDevRendererOrigin(options.devRendererOrigin ?? null);
  return (
    details: OnHeadersReceivedListenerDetails,
    callback: (headersReceivedResponse: HeadersReceivedResponse) => void,
  ): void => {
    const requestOrigin = parseRequestOrigin(details.url);
    const useDev = devRendererOrigin !== null && requestOrigin === devRendererOrigin;
    callback({
      responseHeaders: {
        ...headersWithoutCsp(details.responseHeaders),
        [CSP_HEADER_NAME]: [rendererCspForBrokerUrl(readBrokerUrl(), { isDevRequest: useDev })],
      },
    });
  };
}

export interface RendererCspForBrokerUrlOptions {
  /**
   * `true` when the response is being served from the validated dev
   * renderer origin. Defaults to `false` (strict CSP).
   */
  readonly isDevRequest?: boolean;
}

export function rendererCspForBrokerUrl(
  brokerUrl: string | null,
  options: RendererCspForBrokerUrlOptions = {},
): string {
  const baseCsp = options.isDevRequest === true ? DEV_RENDERER_CSP : BASE_RENDERER_CSP;
  const brokerOrigin = brokerUrl === null ? null : brokerOriginForCsp(brokerUrl);
  if (brokerOrigin === null) return baseCsp;
  return baseCsp.replace("connect-src 'self'", `connect-src 'self' ${brokerOrigin}`);
}

function brokerOriginForCsp(brokerUrl: string): string {
  let parsed: URL;
  try {
    parsed = new URL(brokerUrl);
  } catch {
    throw new Error(`Invalid broker URL for CSP: ${brokerUrl}`);
  }
  if (
    parsed.protocol !== "http:" ||
    parsed.port.length === 0 ||
    !isLoopbackHostname(parsed.hostname) ||
    parsed.origin !== brokerUrl
  ) {
    throw new Error(`Refusing to add non-loopback broker URL to CSP: ${brokerUrl}`);
  }
  return parsed.origin;
}

function isLoopbackHostname(hostname: string): boolean {
  return hostname === "127.0.0.1" || hostname === "localhost" || hostname === "[::1]";
}

function normalizeDevRendererOrigin(raw: string | null): string | null {
  if (raw === null || raw.length === 0) return null;
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return null;
  }
  // Only loopback http origins survive normalization. Anything else means
  // a misconfiguration; fall back to strict CSP rather than honor an
  // attacker-influenced env value.
  if (parsed.protocol !== "http:" || parsed.port.length === 0) return null;
  if (!isLoopbackHostname(parsed.hostname)) return null;
  if (parsed.origin !== raw) return null;
  return parsed.origin;
}

function parseRequestOrigin(rawUrl: string): string | null {
  try {
    return new URL(rawUrl).origin;
  } catch {
    return null;
  }
}

function headersWithoutCsp(
  headers: Readonly<Record<string, readonly string[]>> | undefined,
): Record<string, string[]> {
  if (headers === undefined) return {};
  const next: Record<string, string[]> = {};
  for (const [name, value] of Object.entries(headers)) {
    if (name.toLowerCase() === CSP_HEADER_NAME.toLowerCase()) continue;
    next[name] = [...value];
  }
  return next;
}

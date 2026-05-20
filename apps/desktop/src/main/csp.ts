import type { HeadersReceivedResponse, OnHeadersReceivedListenerDetails, Session } from "electron";

const CSP_HEADER_NAME = "Content-Security-Policy";

export const BASE_RENDERER_CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

// Vite dev mode needs an inline preamble script and a websocket HMR channel.
// Loosen `script-src`, `style-src`, and `connect-src` only when serving the
// dev renderer; the packaged build is always strict.
export const DEV_RENDERER_CSP =
  "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws://localhost:5173 http://localhost:5173; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

export interface InstallDynamicRendererCspOptions {
  readonly isPackaged?: boolean;
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
  const isPackaged = options.isPackaged ?? true;
  return (
    details: OnHeadersReceivedListenerDetails,
    callback: (headersReceivedResponse: HeadersReceivedResponse) => void,
  ): void => {
    callback({
      responseHeaders: {
        ...headersWithoutCsp(details.responseHeaders),
        [CSP_HEADER_NAME]: [rendererCspForBrokerUrl(readBrokerUrl(), { isPackaged })],
      },
    });
  };
}

export function rendererCspForBrokerUrl(
  brokerUrl: string | null,
  options: InstallDynamicRendererCspOptions = {},
): string {
  const isPackaged = options.isPackaged ?? true;
  const baseCsp = isPackaged ? BASE_RENDERER_CSP : DEV_RENDERER_CSP;
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

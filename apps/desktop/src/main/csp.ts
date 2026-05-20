import type { HeadersReceivedResponse, OnHeadersReceivedListenerDetails, Session } from "electron";

const CSP_HEADER_NAME = "Content-Security-Policy";

export const BASE_RENDERER_CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'";

export function installDynamicRendererCsp(
  electronSession: Session,
  readBrokerUrl: () => string | null,
): void {
  electronSession.webRequest.onHeadersReceived(
    createDynamicRendererCspHeadersReceivedListener(readBrokerUrl),
  );
}

export function createDynamicRendererCspHeadersReceivedListener(
  readBrokerUrl: () => string | null,
) {
  return (
    details: OnHeadersReceivedListenerDetails,
    callback: (headersReceivedResponse: HeadersReceivedResponse) => void,
  ): void => {
    callback({
      responseHeaders: {
        ...headersWithoutCsp(details.responseHeaders),
        [CSP_HEADER_NAME]: [rendererCspForBrokerUrl(readBrokerUrl())],
      },
    });
  };
}

export function rendererCspForBrokerUrl(brokerUrl: string | null): string {
  const brokerOrigin = brokerUrl === null ? null : brokerOriginForCsp(brokerUrl);
  if (brokerOrigin === null) return BASE_RENDERER_CSP;
  return BASE_RENDERER_CSP.replace("connect-src 'self'", `connect-src 'self' ${brokerOrigin}`);
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

import { asBrokerUrl, type BrokerUrl, isBrokerUrl } from "@wuphf/protocol";

const DESKTOP_WEBAUTHN_HOST = "localhost";
const DESKTOP_BROKER_HOSTS = new Set(["127.0.0.1", DESKTOP_WEBAUTHN_HOST]);

export function toDesktopBrowserBrokerUrl(brokerUrl: string): BrokerUrl {
  if (!isBrokerUrl(brokerUrl)) {
    throw new Error(`Invalid broker URL for desktop browser load: ${brokerUrl}`);
  }

  const parsed = new URL(brokerUrl);
  if (!DESKTOP_BROKER_HOSTS.has(parsed.hostname)) {
    throw new Error(`Unsupported broker URL host for desktop browser load: ${parsed.hostname}`);
  }

  return asBrokerUrl(`http://${DESKTOP_WEBAUTHN_HOST}:${parsed.port}`);
}

interface ParsedBootstrap {
  readonly token: string;
  readonly brokerUrl: string;
}

// Hand-mirrors the acceptance rules from @wuphf/protocol#apiBootstrapFromJson,
// #asApiToken, and #assertApiBootstrapBrokerUrl without importing the protocol
// package into the renderer bundle; protocol depends on `node:crypto` and is
// sized for the broker subprocess, not the browser-context renderer.
//
// API_TOKEN_RE: copy of `API_TOKEN_RE` in protocol. Base64url alphabet
// only — bounded length, no `+`/`/`/`.`/`~` (those don't round-trip through
// `?token=` query strings unchanged).
const API_TOKEN_RE = /^[A-Za-z0-9_-]{16,512}$/;

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

// Copy of protocol's requiredStringField descriptor guard. Keep this inline:
// importing @wuphf/protocol would pull Node-only crypto dependencies into the
// sandboxed renderer.
function requiredBootstrapStringField(
  record: Readonly<Record<string, unknown>>,
  key: "token" | "broker_url",
  path: string,
): string {
  if (!hasOwn(record, key)) {
    throw new Error(`${path}: is required`);
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${path}: must be a data property`);
  }
  if (typeof descriptor.value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return descriptor.value;
}

export function parseBootstrap(value: unknown): ParsedBootstrap {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("api-token response is not an object");
  }
  const record = value as Readonly<Record<string, unknown>>;
  for (const key of Object.keys(record)) {
    if (key !== "token" && key !== "broker_url") {
      throw new Error(`api-token response has unknown key: ${key}`);
    }
  }
  const token = requiredBootstrapStringField(record, "token", "api-token response: token");
  const brokerUrl = requiredBootstrapStringField(
    record,
    "broker_url",
    "api-token response: broker_url",
  );
  if (!API_TOKEN_RE.test(token)) {
    throw new Error("api-token response: token does not match the API token shape");
  }
  if (brokerUrl.length === 0) {
    throw new Error("api-token response: broker_url must be a non-empty string");
  }
  let parsed: URL;
  try {
    parsed = new URL(brokerUrl);
  } catch {
    throw new Error("api-token response: broker_url is not a valid URL");
  }
  if (parsed.protocol !== "http:") {
    throw new Error("api-token response: broker_url must use http://");
  }
  if (parsed.port === "") {
    throw new Error("api-token response: broker_url must include an explicit port");
  }
  const portNumber = Number(parsed.port);
  if (!Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) {
    throw new Error("api-token response: broker_url port must be 1..65535");
  }
  if (!isLoopbackHostname(parsed.hostname)) {
    throw new Error("api-token response: broker_url host must be loopback");
  }
  // Shape lock: BrokerUrl IS the broker origin in bare canonical form (no
  // trailing slash). Downstream code does `${bootstrap.brokerUrl}/api/health`
  // — a trailing-slash form would produce `http://h:p//api/health` (double
  // slash). Raw-vs-origin equality also rejects percent-encoded dot segments
  // that URL normalizes to `/`. Mirror the protocol codec's
  // assertApiBootstrapBrokerUrl byte-for-byte.
  if (
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    brokerUrl !== parsed.origin
  ) {
    throw new Error(
      "api-token response: broker_url must be a bare loopback origin with no trailing slash, userinfo, path, query, or fragment",
    );
  }
  return { token, brokerUrl };
}

// `URL.hostname` returns bracketed IPv6 (`[::1]` for `http://[::1]:1234`),
// while the bare-form check protocol does on the SAME parsed value sees
// the bracketed form. Accept both bracketed and bare loopback IPv6 so
// renderer parity holds across the v0 IPv6 path the protocol codec
// supports.
function isLoopbackHostname(hostname: string): boolean {
  if (hostname === "127.0.0.1" || hostname === "localhost") return true;
  if (hostname === "::1" || hostname === "[::1]") return true;
  return false;
}

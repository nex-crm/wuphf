import type { HeadersReceivedResponse, OnHeadersReceivedListenerDetails } from "electron";
import { describe, expect, it } from "vitest";

import {
  BASE_RENDERER_CSP,
  createDynamicRendererCspHeadersReceivedListener,
  DEV_RENDERER_CSP,
  rendererCspForBrokerUrl,
} from "../../src/main/csp.ts";

describe("dynamic renderer CSP injection", () => {
  it("sets connect-src to self when the broker has not published a URL", () => {
    const response = driveCspCallback({
      brokerUrl: null,
      responseHeaders: { "Content-Type": ["text/html"] },
    });

    expect(response.responseHeaders).toEqual({
      "Content-Type": ["text/html"],
      "Content-Security-Policy": [BASE_RENDERER_CSP],
    });
  });

  it("sets a CSP header when the response has no existing headers", () => {
    const listener = createDynamicRendererCspHeadersReceivedListener(() => null);

    expect(invokeListener(listener, undefined).responseHeaders).toEqual({
      "Content-Security-Policy": [BASE_RENDERER_CSP],
    });
  });

  it("appends the current broker origin to connect-src", () => {
    const brokerUrl = "http://127.0.0.1:54321";
    const response = driveCspCallback({
      brokerUrl,
      responseHeaders: {
        "content-security-policy": ["default-src *"],
        "X-Test": ["kept"],
      },
    });

    expect(response.responseHeaders).toEqual({
      "X-Test": ["kept"],
      "Content-Security-Policy": [rendererCspForBrokerUrl(brokerUrl)],
    });
    expect(response.responseHeaders?.["Content-Security-Policy"]).toEqual([
      "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' http://127.0.0.1:54321; img-src 'self' data:; base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none'; worker-src 'none'",
    ]);
  });

  it("accepts loopback host aliases only", () => {
    expect(rendererCspForBrokerUrl("http://localhost:54321")).toContain(
      "connect-src 'self' http://localhost:54321",
    );
    expect(rendererCspForBrokerUrl("http://[::1]:54321")).toContain(
      "connect-src 'self' http://[::1]:54321",
    );
    expect(() => rendererCspForBrokerUrl("http://127.0.0.1")).toThrow(
      "Refusing to add non-loopback broker URL to CSP",
    );
    expect(() => rendererCspForBrokerUrl("http://127.0.0.1:54321/")).toThrow(
      "Refusing to add non-loopback broker URL to CSP",
    );
  });

  it("re-derives the CSP when the broker restarts on a new port", () => {
    let brokerUrl: string | null = "http://127.0.0.1:54321";
    const listener = createDynamicRendererCspHeadersReceivedListener(() => brokerUrl);

    const first = invokeListener(listener);
    brokerUrl = null;
    const duringRestart = invokeListener(listener);
    brokerUrl = "http://127.0.0.1:54322";
    const second = invokeListener(listener);

    expect(first.responseHeaders?.["Content-Security-Policy"]).toEqual([
      rendererCspForBrokerUrl("http://127.0.0.1:54321"),
    ]);
    expect(duringRestart.responseHeaders?.["Content-Security-Policy"]).toEqual([BASE_RENDERER_CSP]);
    expect(second.responseHeaders?.["Content-Security-Policy"]).toEqual([
      rendererCspForBrokerUrl("http://127.0.0.1:54322"),
    ]);
  });

  it("refuses non-loopback broker origins", () => {
    expect(() => rendererCspForBrokerUrl("https://example.com:443")).toThrow(
      "Refusing to add non-loopback broker URL to CSP",
    );
  });

  it("refuses malformed broker URLs", () => {
    expect(() => rendererCspForBrokerUrl("not a url")).toThrow("Invalid broker URL for CSP");
  });

  it("returns the dev CSP variant only when explicitly asked", () => {
    expect(rendererCspForBrokerUrl(null, { isDevRequest: true })).toBe(DEV_RENDERER_CSP);
    expect(DEV_RENDERER_CSP).toContain("'unsafe-inline'");
    expect(DEV_RENDERER_CSP).toContain("'unsafe-eval'");
    expect(DEV_RENDERER_CSP).toContain("ws://localhost:5173");
  });

  it("appends the broker origin to the dev CSP connect-src", () => {
    const csp = rendererCspForBrokerUrl("http://127.0.0.1:54321", { isDevRequest: true });
    expect(csp).toContain("connect-src 'self' http://127.0.0.1:54321 ws://localhost:5173");
  });

  it("uses dev CSP only for responses served from the validated dev origin", () => {
    const listener = createDynamicRendererCspHeadersReceivedListener(
      () => "http://127.0.0.1:54321",
      { devRendererOrigin: "http://localhost:5173" },
    );

    // Response served from the dev server -> dev CSP.
    const devResponse = invokeListener(listener, {}, "http://localhost:5173/main.tsx");
    expect(devResponse.responseHeaders?.["Content-Security-Policy"]?.[0]).toContain(
      "'unsafe-inline'",
    );

    // Response served from the broker (e.g. `electron-vite preview` or
    // packaged-style load against the broker's static handler) MUST get
    // the strict CSP — `!app.isPackaged` is not enough on its own.
    const brokerResponse = invokeListener(listener, {}, "http://127.0.0.1:54321/index.html");
    expect(brokerResponse.responseHeaders?.["Content-Security-Policy"]?.[0]).not.toContain(
      "'unsafe-inline'",
    );
    expect(brokerResponse.responseHeaders?.["Content-Security-Policy"]?.[0]).toContain(
      "script-src 'self';",
    );
  });

  it("refuses dev CSP when devRendererOrigin is null", () => {
    const listener = createDynamicRendererCspHeadersReceivedListener(
      () => "http://127.0.0.1:54321",
      { devRendererOrigin: null },
    );
    const response = invokeListener(listener, {}, "http://localhost:5173/main.tsx");
    expect(response.responseHeaders?.["Content-Security-Policy"]?.[0]).not.toContain(
      "'unsafe-inline'",
    );
  });

  it("rejects a non-loopback devRendererOrigin and falls back to strict", () => {
    const listener = createDynamicRendererCspHeadersReceivedListener(() => null, {
      devRendererOrigin: "http://evil.example.com:80",
    });
    const response = invokeListener(listener, {}, "http://evil.example.com:80/main.tsx");
    expect(response.responseHeaders?.["Content-Security-Policy"]).toEqual([BASE_RENDERER_CSP]);
  });
});

type CspListener = ReturnType<typeof createDynamicRendererCspHeadersReceivedListener>;

function driveCspCallback(args: {
  readonly brokerUrl: string | null;
  readonly responseHeaders: Record<string, string[]>;
}): HeadersReceivedResponse {
  const listener = createDynamicRendererCspHeadersReceivedListener(() => args.brokerUrl);
  return invokeListener(listener, args.responseHeaders);
}

function invokeListener(
  listener: CspListener,
  responseHeaders: Record<string, string[]> | undefined = {},
  url: string = "http://localhost:5173/",
): HeadersReceivedResponse {
  let response: HeadersReceivedResponse | null = null;
  listener(createDetails(responseHeaders, url), (value) => {
    response = value;
  });
  if (response === null) {
    throw new Error("Expected CSP listener to call back synchronously");
  }
  return response;
}

function createDetails(
  responseHeaders: Record<string, string[]> | undefined,
  url: string,
): OnHeadersReceivedListenerDetails {
  const base = {
    id: 1,
    url,
    method: "GET",
    resourceType: "mainFrame",
    referrer: "",
    timestamp: 1,
    statusLine: "HTTP/1.1 200 OK",
    statusCode: 200,
  } satisfies Omit<OnHeadersReceivedListenerDetails, "responseHeaders">;
  return responseHeaders === undefined ? base : { ...base, responseHeaders };
}

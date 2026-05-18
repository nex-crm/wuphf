import { describe, expect, it } from "vitest";

import { toDesktopBrowserBrokerUrl } from "../src/main/broker-url.ts";

describe("toDesktopBrowserBrokerUrl", () => {
  it("maps loopback broker URLs to the browser-valid localhost origin", () => {
    expect(toDesktopBrowserBrokerUrl("http://127.0.0.1:7891")).toBe("http://localhost:7891");
    expect(toDesktopBrowserBrokerUrl("http://localhost:7891")).toBe("http://localhost:7891");
  });

  it("rejects malformed and unsupported broker URLs", () => {
    expect(() => toDesktopBrowserBrokerUrl("not a url")).toThrow(/Invalid broker URL/);
    expect(() => toDesktopBrowserBrokerUrl("http://[::1]:7891")).toThrow(
      /Unsupported broker URL host/,
    );
  });
});

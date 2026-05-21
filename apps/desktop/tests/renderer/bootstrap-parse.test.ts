import { describe, expect, it } from "vitest";

import { parseBootstrap } from "../../src/renderer/bootstrap.ts";

const VALID_TOKEN = "A".repeat(16);
const VALID_BROKER_URL = "http://127.0.0.1:54321";

describe("parseBootstrap", () => {
  it("accepts canonical loopback bootstrap records", () => {
    expect(parseBootstrap({ token: VALID_TOKEN, broker_url: VALID_BROKER_URL })).toEqual({
      token: VALID_TOKEN,
      brokerUrl: VALID_BROKER_URL,
    });

    expect(parseBootstrap({ token: VALID_TOKEN, broker_url: "http://[::1]:54321" })).toEqual({
      token: VALID_TOKEN,
      brokerUrl: "http://[::1]:54321",
    });
  });

  it("rejects malformed bootstrap token fields", () => {
    expect(() => parseBootstrap({ token: "short", broker_url: VALID_BROKER_URL })).toThrow(
      "api-token response: token does not match the API token shape",
    );
    expect(() => parseBootstrap({ token: 12, broker_url: VALID_BROKER_URL })).toThrow(
      "api-token response: token: must be a string",
    );
  });

  it("rejects unknown keys and non-object shapes", () => {
    expect(() =>
      parseBootstrap({ token: VALID_TOKEN, broker_url: VALID_BROKER_URL, extra: true }),
    ).toThrow("api-token response has unknown key: extra");
    expect(() => parseBootstrap(null)).toThrow("api-token response is not an object");
  });

  it("rejects broker URLs that are not bare loopback HTTP origins", () => {
    const cases: ReadonlyArray<readonly [unknown, string]> = [
      ["", "api-token response: broker_url must be a non-empty string"],
      ["not a url", "api-token response: broker_url is not a valid URL"],
      ["https://127.0.0.1:54321", "api-token response: broker_url must use http://"],
      ["http://127.0.0.1", "api-token response: broker_url must include an explicit port"],
      ["http://127.0.0.1:0", "api-token response: broker_url port must be 1..65535"],
      ["http://example.com:54321", "api-token response: broker_url host must be loopback"],
      ["http://127.0.0.1:54321/", "api-token response: broker_url must be a bare loopback origin"],
      [
        `http://${"user"}:${"pass"}@127.0.0.1:54321`,
        "api-token response: broker_url must be a bare loopback origin",
      ],
      [
        "http://127.0.0.1:54321?x=1",
        "api-token response: broker_url must be a bare loopback origin",
      ],
      [
        "http://127.0.0.1:54321#frag",
        "api-token response: broker_url must be a bare loopback origin",
      ],
    ];

    for (const [brokerUrl, message] of cases) {
      expect(() => parseBootstrap({ token: VALID_TOKEN, broker_url: brokerUrl })).toThrow(message);
    }
  });
});

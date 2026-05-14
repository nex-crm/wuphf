import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  agentProviderRoutingFromJson,
  agentProviderRoutingToJsonValue,
} from "../src/agent-provider-routing.ts";

interface AgentProviderRoutingAcceptedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expected: {
    readonly canonicalSerialization: string;
  };
}

interface AgentProviderRoutingRejectedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expectedError: string;
}

interface AgentProviderRoutingVectorsFixture {
  readonly schemaVersion: 1;
  readonly comment: string;
  readonly accepted: readonly AgentProviderRoutingAcceptedVector[];
  readonly rejected: readonly AgentProviderRoutingRejectedVector[];
}

const fixture = loadAgentProviderRoutingVectors();

describe("agent-provider-routing conformance vectors", () => {
  it("uses fixture schemaVersion 1", () => {
    expect(fixture.schemaVersion).toBe(1);
  });

  for (const vector of fixture.accepted) {
    it(`accepts ${vector.name}`, () => {
      const parsed = agentProviderRoutingFromJson(vector.input);
      expect(JSON.stringify(agentProviderRoutingToJsonValue(parsed))).toBe(
        vector.expected.canonicalSerialization,
      );
    });
  }

  for (const vector of fixture.rejected) {
    it(`rejects ${vector.name}`, () => {
      const message = captureErrorMessage(() => agentProviderRoutingFromJson(vector.input));
      expect(message).toContain(vector.expectedError);
    });
  }
});

function loadAgentProviderRoutingVectors(): AgentProviderRoutingVectorsFixture {
  const parsed: unknown = JSON.parse(
    readFileSync(
      new URL("../testdata/agent-provider-routing-vectors.json", import.meta.url),
      "utf8",
    ),
  );
  const record = requireRecord(parsed, "fixture");
  assertKnownFixtureKeys(record, "fixture", ["schemaVersion", "comment", "accepted", "rejected"]);
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", "fixture"),
    comment: requiredString(record, "comment", "fixture"),
    accepted: requiredArray(record, "accepted", "fixture").map((vector, index) =>
      parseAcceptedVector(vector, `fixture.accepted.${index}`),
    ),
    rejected: requiredArray(record, "rejected", "fixture").map((vector, index) =>
      parseRejectedVector(vector, `fixture.rejected.${index}`),
    ),
  };
}

function parseAcceptedVector(value: unknown, path: string): AgentProviderRoutingAcceptedVector {
  const record = requireRecord(value, path);
  assertKnownFixtureKeys(record, path, ["name", "input", "expected"]);
  const expected = requireRecord(requiredField(record, "expected", path), `${path}.expected`);
  assertKnownFixtureKeys(expected, `${path}.expected`, ["canonicalSerialization"]);
  return {
    name: requiredString(record, "name", path),
    input: requiredField(record, "input", path),
    expected: {
      canonicalSerialization: requiredString(
        expected,
        "canonicalSerialization",
        `${path}.expected`,
      ),
    },
  };
}

function parseRejectedVector(value: unknown, path: string): AgentProviderRoutingRejectedVector {
  const record = requireRecord(value, path);
  assertKnownFixtureKeys(record, path, ["name", "input", "expectedError"]);
  return {
    name: requiredString(record, "name", path),
    input: requiredField(record, "input", path),
    expectedError: requiredString(record, "expectedError", path),
  };
}

function captureErrorMessage(fn: () => unknown): string {
  try {
    fn();
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
  throw new Error("expected function to throw");
}

function requireRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value as Readonly<Record<string, unknown>>;
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown {
  if (!Object.hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${path}.${key}: is required`);
  }
  return record[key];
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}.${key}: must be a string`);
  }
  return value;
}

function requiredArray(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): readonly unknown[] {
  const value = requiredField(record, key, path);
  if (!Array.isArray(value)) {
    throw new Error(`${path}.${key}: must be an array`);
  }
  return value;
}

function requiredSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): 1 {
  const value = requiredField(record, key, path);
  if (value !== 1) {
    throw new Error(`${path}.${key}: must be 1`);
  }
  return 1;
}

function assertKnownFixtureKeys(
  record: Readonly<Record<string, unknown>>,
  path: string,
  allowed: readonly string[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.includes(key)) {
      throw new Error(`${path}/${key}: is not allowed`);
    }
  }
}

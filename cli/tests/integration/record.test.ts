import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { runNex, nexEnv } from "./helpers.js";
import { startMockServer } from "./mock-server.js";

let env: Record<string, string>;
let closeMock: () => void;

before(() => {
  const mock = startMockServer();
  env = nexEnv(mock.url);
  closeMock = mock.close;
});

after(() => closeMock());

describe("record commands", () => {
  it("record list — returns records for an object", async () => {
    const { stdout, exitCode } = await runNex(["record", "list", "company"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.data, "should have data array");
    assert.equal(data.data.length, 2);
    assert.equal(data.data[0].id, "rec-1");
  });

  it("record get — returns a single record", async () => {
    const { stdout, exitCode } = await runNex(["record", "get", "rec-1"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "rec-1");
    assert.equal(data.type, "company");
  });

  it("record create — creates a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "create", "company", "--data", '{"name":"TestCo"}'],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.id, "should have an id");
    assert.equal(data.type, "company");
  });

  it("record upsert — upserts a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "upsert", "company", "--match", "domains", "--data", '{"name":"Acme","domains":"acme.com"}'],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.id, "should have an id");
  });

  it("record update — updates a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "update", "rec-1", "--data", '{"name":"Updated Corp"}'],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "rec-1");
  });

  it("record delete — deletes a record", async () => {
    const { stdout, exitCode } = await runNex(["record", "delete", "rec-1"], { env });
    assert.equal(exitCode, 0);
  });

  it("record timeline — returns timeline events", async () => {
    const { stdout, exitCode } = await runNex(["record", "timeline", "rec-1"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.events, "should have events");
    assert.equal(data.events.length, 1);
    assert.equal(data.events[0].summary, "Record created");
  });
});

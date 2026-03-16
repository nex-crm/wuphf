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

describe("object commands", () => {
  it("object list — returns all objects", async () => {
    const { stdout, exitCode } = await runNex(["object", "list"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.data, "should have data array");
    assert.equal(data.data.length, 2);
    assert.equal(data.data[0].slug, "company");
    assert.equal(data.data[1].slug, "person");
  });

  it("object get — returns a single object", async () => {
    const { stdout, exitCode } = await runNex(["object", "get", "company"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.slug, "company");
    assert.equal(data.name, "Company");
  });

  it("object create — creates an object", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "create", "--name", "Deal", "--slug", "deal", "--type", "deal"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.id, "should have an id");
    assert.equal(data.slug, "deal");
  });

  it("object update — updates an object", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "update", "company", "--name", "Companies Updated"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.slug, "company");
  });

  it("object delete — deletes an object", async () => {
    const { stdout, exitCode } = await runNex(["object", "delete", "company"], { env });
    assert.equal(exitCode, 0);
  });
});

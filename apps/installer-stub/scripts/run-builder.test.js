const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const { scrubbedEnv, setDefaultCache } = require("./run-builder.js");

// M4 regression — `setDefaultCache` previously mutated process.env directly,
// leaking ELECTRON_CACHE / ELECTRON_BUILDER_CACHE into subsequent commands in
// the same Node process. The fix moved it inside scrubbedEnv() so mutation is
// bounded to the env object handed to the spawned child.

function snapshotEnvKeys(prefix) {
  const result = {};
  for (const [k, v] of Object.entries(process.env)) {
    if (k.startsWith(prefix)) {
      result[k] = v;
    }
  }
  return result;
}

function testSetDefaultCacheIsBoundedToTheGivenEnv() {
  const beforeProcess = snapshotEnvKeys("ELECTRON_CACHE_TEST_M4");
  assert.deepEqual(beforeProcess, {}, "test pre-condition: marker key not in process.env");

  // Use a per-run subdirectory under the platform temp dir so the test is
  // portable to Windows (`/tmp` does not exist there) and so we can remove
  // the directory that `setDefaultCache` will mkdir.
  const markerDir = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-m4-"));
  const markerPath = path.join(markerDir, "marker");

  try {
    const env = {};
    setDefaultCache(env, "ELECTRON_CACHE_TEST_M4", markerPath);

    assert.equal(env.ELECTRON_CACHE_TEST_M4, markerPath, "given env was populated");
    assert.equal(
      process.env.ELECTRON_CACHE_TEST_M4,
      undefined,
      "M4 regression: setDefaultCache must not leak into process.env",
    );
  } finally {
    fs.rmSync(markerDir, { recursive: true, force: true });
  }
}

function testScrubbedEnvDoesNotMutateProcessEnv() {
  const before = {
    ELECTRON_CACHE: process.env.ELECTRON_CACHE,
    ELECTRON_BUILDER_CACHE: process.env.ELECTRON_BUILDER_CACHE,
    CUSTOM_APP_BUILDER_PATH: process.env.CUSTOM_APP_BUILDER_PATH,
  };

  // Plant bun-style lifecycle vars to verify the scrub strips them
  // from the returned env without touching process.env.
  process.env.npm_config_test_marker = "should-not-leak-into-child";
  process.env.BUN_TEST_MARKER = "should-not-leak-into-child";

  try {
    const env = scrubbedEnv();

    assert.equal(env.npm_config_test_marker, undefined, "npm_ vars must be scrubbed");
    assert.equal(env.BUN_TEST_MARKER, undefined, "BUN_ vars must be scrubbed");
    assert.ok(env.ELECTRON_CACHE, "ELECTRON_CACHE populated in returned env");
    assert.ok(env.ELECTRON_BUILDER_CACHE, "ELECTRON_BUILDER_CACHE populated in returned env");

    assert.equal(
      process.env.ELECTRON_CACHE,
      before.ELECTRON_CACHE,
      "scrubbedEnv must not mutate parent process.env.ELECTRON_CACHE",
    );
    assert.equal(
      process.env.ELECTRON_BUILDER_CACHE,
      before.ELECTRON_BUILDER_CACHE,
      "scrubbedEnv must not mutate parent process.env.ELECTRON_BUILDER_CACHE",
    );
    assert.equal(
      process.env.CUSTOM_APP_BUILDER_PATH,
      before.CUSTOM_APP_BUILDER_PATH,
      "scrubbedEnv must not mutate parent process.env.CUSTOM_APP_BUILDER_PATH",
    );
  } finally {
    delete process.env.npm_config_test_marker;
    delete process.env.BUN_TEST_MARKER;
  }
}

testSetDefaultCacheIsBoundedToTheGivenEnv();
testScrubbedEnvDoesNotMutateProcessEnv();

console.log("run-builder self-test OK");

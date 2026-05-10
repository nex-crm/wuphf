const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const sourceScript = path.join(__dirname, "normalize-package-version.js");

function writeFixture(root) {
  const appRoot = path.join(root, "apps", "installer-stub");
  const scriptsDir = path.join(appRoot, "scripts");

  fs.mkdirSync(scriptsDir, { recursive: true });
  fs.copyFileSync(sourceScript, path.join(scriptsDir, "normalize-package-version.js"));
  fs.writeFileSync(
    path.join(appRoot, "package.json"),
    `${JSON.stringify(
      {
        name: "@wuphf/installer-stub-fixture",
        version: "0.0.0",
        wuphfBuildChannel: "dev",
      },
      null,
      2,
    )}\n`,
  );

  return {
    appRoot,
    script: path.join(scriptsDir, "normalize-package-version.js"),
  };
}

function runNode(script, env) {
  return spawnSync(process.execPath, [script], {
    encoding: "utf8",
    env: {
      ...process.env,
      ...env,
    },
  });
}

function testEmptyVersionFails() {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-normalize-version-"));

  try {
    const fixture = writeFixture(tempRoot);
    const result = runNode(fixture.script, {
      WUPHF_BUILD_VERSION: "",
      WUPHF_BUILD_CHANNEL: "stable",
    });

    assert.equal(result.status, 1);
    assert.match(result.stderr, /WUPHF_BUILD_VERSION is empty; refusing to rewrite package\.json/);
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
}

function testValidVersionRewritesPackageJson() {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-normalize-version-"));

  try {
    const fixture = writeFixture(tempRoot);
    const result = runNode(fixture.script, {
      WUPHF_BUILD_VERSION: "1.2.3-beta.1",
      WUPHF_BUILD_CHANNEL: "beta",
    });

    assert.equal(result.status, 0, result.stderr);

    const pkg = JSON.parse(fs.readFileSync(path.join(fixture.appRoot, "package.json"), "utf8"));
    assert.equal(pkg.version, "1.2.3-beta.1");
    assert.equal(pkg.wuphfBuildChannel, "beta");
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
}

testEmptyVersionFails();
testValidVersionRewritesPackageJson();
console.log("normalize-package-version self-test OK");

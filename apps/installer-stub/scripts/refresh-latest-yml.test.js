const { spawnSync } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { expect, test } = require("bun:test");
const yaml = require("js-yaml");

const appRoot = path.resolve(__dirname, "..");
const refreshScript = path.join(__dirname, "refresh-latest-yml.js");
const fixtureManifest = path.join(__dirname, "fixtures", "refresh-latest-yml", "latest-mac.yml");

function sha512Base64(bytes) {
  return crypto.createHash("sha512").update(bytes).digest("base64");
}

test("refreshes sha512 and size for every latest-mac.yml files[] artifact", () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-refresh-latest-"));
  const distDir = path.join(tempRoot, "dist");

  try {
    fs.mkdirSync(distDir, { recursive: true });

    const zipBytes = Buffer.from("zip bytes after notarization and stapling");
    const dmgBytes = Buffer.from("dmg bytes after notarization and stapling");
    const zipName = "wuphf-installer-stub-0.0.0-mac-universal.zip";
    const dmgName = "wuphf-installer-stub-0.0.0-mac-universal.dmg";

    fs.writeFileSync(path.join(distDir, zipName), zipBytes);
    fs.writeFileSync(path.join(distDir, dmgName), dmgBytes);
    fs.copyFileSync(fixtureManifest, path.join(distDir, "latest-mac.yml"));

    const result = spawnSync(
      process.execPath,
      [refreshScript, path.join(distDir, "latest-mac.yml")],
      {
        cwd: appRoot,
        encoding: "utf8",
      },
    );

    expect(result.status).toBe(0);

    const manifest = yaml.load(fs.readFileSync(path.join(distDir, "latest-mac.yml"), "utf8"));
    expect(manifest.sha512).toBe(sha512Base64(zipBytes));
    expect(manifest.size).toBe(zipBytes.byteLength);
    expect(manifest.files).toEqual([
      {
        url: zipName,
        sha512: sha512Base64(zipBytes),
        size: zipBytes.byteLength,
      },
      {
        url: dmgName,
        sha512: sha512Base64(dmgBytes),
        size: dmgBytes.byteLength,
      },
    ]);
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

test("fails when a latest-mac.yml files[] artifact is missing", () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-refresh-latest-"));
  const distDir = path.join(tempRoot, "dist");

  try {
    fs.mkdirSync(distDir, { recursive: true });

    const zipName = "wuphf-installer-stub-0.0.0-mac-universal.zip";
    const dmgName = "wuphf-installer-stub-0.0.0-mac-universal.dmg";

    fs.writeFileSync(path.join(distDir, zipName), Buffer.from("zip bytes"));
    fs.copyFileSync(fixtureManifest, path.join(distDir, "latest-mac.yml"));

    const result = spawnSync(
      process.execPath,
      [refreshScript, path.join(distDir, "latest-mac.yml")],
      {
        cwd: appRoot,
        encoding: "utf8",
      },
    );

    expect(result.status).toBe(1);
    expect(result.stderr).toContain(
      `latest-mac.yml files[] entry points at missing artifact: ${dmgName}`,
    );
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const yaml = require("js-yaml");

const manifestPath = process.argv[2];

function fail(message) {
  console.error(message);
  process.exit(1);
}

if (!manifestPath) {
  fail("usage: refresh-latest-yml.js <dist/latest*.yml>");
}

const absoluteManifestPath = path.resolve(manifestPath);
if (!fs.existsSync(absoluteManifestPath)) {
  fail(`Missing manifest: ${manifestPath}`);
}

const manifest = yaml.load(fs.readFileSync(absoluteManifestPath, "utf8"));
if (!manifest || typeof manifest !== "object" || Array.isArray(manifest)) {
  fail(`${manifestPath} is not a YAML object`);
}

const artifactName = manifest.path;
if (typeof artifactName !== "string" || artifactName.trim() === "") {
  fail(`${manifestPath} has empty or missing path field`);
}

const artifactPath = path.resolve(path.dirname(absoluteManifestPath), artifactName);
if (!artifactPath.startsWith(`${path.dirname(absoluteManifestPath)}${path.sep}`)) {
  fail(`${manifestPath} path escapes dist directory: ${artifactName}`);
}

if (!fs.existsSync(artifactPath)) {
  fail(`${manifestPath} points at missing artifact: ${artifactName}`);
}

const artifactBytes = fs.readFileSync(artifactPath);
const sha512 = crypto.createHash("sha512").update(artifactBytes).digest("base64");
const size = artifactBytes.byteLength;

manifest.sha512 = sha512;
manifest.size = size;

if (Array.isArray(manifest.files)) {
  for (const file of manifest.files) {
    if (!file || typeof file !== "object") {
      continue;
    }

    const entryPath = file.url ?? file.path;
    if (
      entryPath === artifactName ||
      path.basename(entryPath ?? "") === path.basename(artifactName)
    ) {
      file.sha512 = sha512;
      file.size = size;
    }
  }
}

fs.writeFileSync(
  absoluteManifestPath,
  yaml.dump(manifest, { lineWidth: -1, noRefs: true, sortKeys: false }),
);
console.log(`Refreshed ${manifestPath} for ${artifactName}.`);

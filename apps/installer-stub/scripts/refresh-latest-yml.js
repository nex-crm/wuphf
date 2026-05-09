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

const distDir = path.dirname(absoluteManifestPath);

function resolveDistArtifact(entryPath) {
  if (path.isAbsolute(entryPath) || entryPath.includes("..")) {
    fail(`${manifestPath} path escapes dist directory: ${entryPath}`);
  }

  const artifactBasename = path.basename(entryPath);
  const artifactPath = path.resolve(distDir, artifactBasename);
  if (!artifactPath.startsWith(`${distDir}${path.sep}`)) {
    fail(`${manifestPath} path escapes dist directory: ${entryPath}`);
  }

  return { artifactBasename, artifactPath };
}

function artifactMetadata(artifactPath) {
  const artifactBytes = fs.readFileSync(artifactPath);
  return {
    sha512: crypto.createHash("sha512").update(artifactBytes).digest("base64"),
    size: artifactBytes.byteLength,
  };
}

const { artifactBasename, artifactPath } = resolveDistArtifact(artifactName);
if (!fs.existsSync(artifactPath)) {
  fail(`${manifestPath} points at missing artifact: ${artifactName}`);
}

const { sha512, size } = artifactMetadata(artifactPath);
const refreshedArtifacts = new Set([artifactBasename]);

manifest.sha512 = sha512;
manifest.size = size;

if (Array.isArray(manifest.files)) {
  for (const file of manifest.files) {
    if (!file || typeof file !== "object") {
      continue;
    }

    const entryPath = file.url ?? file.path;
    if (typeof entryPath !== "string" || entryPath.trim() === "") {
      continue;
    }

    const entryArtifact = resolveDistArtifact(entryPath);
    if (!fs.existsSync(entryArtifact.artifactPath)) {
      console.warn(`${manifestPath} files[] entry points at missing artifact: ${entryPath}`);
      continue;
    }

    const entryMetadata = artifactMetadata(entryArtifact.artifactPath);
    file.sha512 = entryMetadata.sha512;
    file.size = entryMetadata.size;
    refreshedArtifacts.add(entryArtifact.artifactBasename);
  }
}

for (const entry of fs.readdirSync(distDir, { withFileTypes: true })) {
  if (!entry.isFile()) {
    continue;
  }

  if (![".AppImage", ".deb", ".dmg", ".exe", ".zip"].includes(path.extname(entry.name))) {
    continue;
  }

  if (!refreshedArtifacts.has(entry.name)) {
    console.warn(`${manifestPath} files[] is missing dist artifact: ${entry.name}`);
  }
}

fs.writeFileSync(
  absoluteManifestPath,
  yaml.dump(manifest, { lineWidth: -1, noRefs: true, sortKeys: false }),
);
console.log(`Refreshed ${manifestPath} for ${artifactName}.`);

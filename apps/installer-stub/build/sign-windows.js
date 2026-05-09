const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const REQUIRED_AZURE_ENV = [
  "AZURE_TENANT_ID",
  "AZURE_CLIENT_ID",
  "AZURE_CLIENT_SECRET",
  "AZURE_SIGNING_ACCOUNT_NAME",
  "AZURE_CERT_PROFILE_NAME",
  "AZURE_ENDPOINT",
];

function env(name) {
  return process.env[name] || "";
}

function missingEnv(names) {
  return names.filter((name) => env(name).trim() === "");
}

function requireProductionEnv() {
  const missing = missingEnv(REQUIRED_AZURE_ENV);
  if (missing.length > 0 && env("WUPHF_RELEASE_MODE") === "production") {
    throw new Error(`missing Azure Trusted Signing environment: ${missing.join(", ")}`);
  }
  return missing;
}

function getTargetPath(configuration) {
  return configuration?.path || configuration?.file || configuration?.filePath || "";
}

function writeAzureMetadata() {
  const metadataPath = path.join(os.tmpdir(), `wuphf-azure-sign-${process.pid}.json`);
  const metadata = {
    Endpoint: env("AZURE_ENDPOINT"),
    CodeSigningAccountName: env("AZURE_SIGNING_ACCOUNT_NAME"),
    CertificateProfileName: env("AZURE_CERT_PROFILE_NAME"),
  };

  fs.writeFileSync(metadataPath, JSON.stringify(metadata, null, 2));
  return metadataPath;
}

async function signWindows(configuration) {
  const targetPath = getTargetPath(configuration);
  const missing = requireProductionEnv();

  if (process.platform !== "win32") {
    console.log("Windows signing skipped on non-Windows runner");
    return;
  }

  if (missing.length > 0) {
    console.log(`Windows signing skipped in PR mode; missing: ${missing.join(", ")}`);
    return;
  }

  if (env("WUPHF_WINDOWS_SIGN_MODE") === "post-build") {
    console.log("Windows signing deferred to Azure/trusted-signing-action post-build step");
    return;
  }

  if (!targetPath) {
    throw new Error("electron-builder did not provide a Windows signing target path");
  }

  const dlibPath = env("AZURE_TRUSTED_SIGNING_DLIB_PATH");
  if (!dlibPath) {
    throw new Error("AZURE_TRUSTED_SIGNING_DLIB_PATH is required for signtool Azure DLib signing");
  }

  const metadataPath = writeAzureMetadata();
  const signtoolPath = env("SIGNTOOL_PATH") || "signtool.exe";
  const result = spawnSync(
    signtoolPath,
    [
      "sign",
      "/v",
      "/debug",
      "/fd",
      "SHA256",
      "/tr",
      "http://timestamp.acs.microsoft.com",
      "/td",
      "SHA256",
      "/dlib",
      dlibPath,
      "/dmdf",
      metadataPath,
      targetPath,
    ],
    { stdio: "inherit" },
  );

  fs.rmSync(metadataPath, { force: true });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    throw new Error(`signtool.exe failed with exit code ${result.status}`);
  }
}

module.exports = signWindows;
module.exports.default = signWindows;

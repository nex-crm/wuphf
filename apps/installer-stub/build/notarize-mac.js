const path = require("node:path");

function env(name) {
  return process.env[name] || "";
}

async function notarizeMac(context) {
  if (process.platform !== "darwin") {
    return;
  }

  if (env("WUPHF_RELEASE_MODE") !== "production") {
    console.log("notarization skipped outside production release mode");
    return;
  }

  if (env("WUPHF_NOTARIZE_WITH_HOOK") !== "true") {
    console.log("notarization skipped (electron-builder will use built-in)");
    return;
  }

  let notarize;
  try {
    ({ notarize } = require("@electron/notarize"));
  } catch {
    console.log("notarization skipped (electron-builder will use built-in)");
    return;
  }

  const appName = context.packager.appInfo.productFilename;
  const appPath = path.join(context.appOutDir, `${appName}.app`);

  await notarize({
    appBundleId: "ai.nex.wuphf.installer-stub",
    appPath,
    appleId: env("APPLE_ID"),
    appleIdPassword: env("APPLE_APP_SPECIFIC_PASSWORD"),
    teamId: env("APPLE_TEAM_ID"),
    tool: "notarytool",
  });
}

module.exports = notarizeMac;
module.exports.default = notarizeMac;

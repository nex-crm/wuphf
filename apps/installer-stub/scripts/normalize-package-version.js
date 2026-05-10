const fs = require("node:fs");
const path = require("node:path");

const packagePath = path.resolve(__dirname, "..", "package.json");
const version = process.env.WUPHF_BUILD_VERSION;

if (!version) {
  console.error("WUPHF_BUILD_VERSION is empty; refusing to rewrite package.json");
  process.exit(1);
}

const pkg = JSON.parse(fs.readFileSync(packagePath, "utf8"));
pkg.version = version;
pkg.wuphfBuildChannel = process.env.WUPHF_BUILD_CHANNEL || "dev";

fs.writeFileSync(packagePath, `${JSON.stringify(pkg, null, 2)}\n`);

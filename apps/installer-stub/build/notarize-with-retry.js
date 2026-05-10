const Module = require("node:module");

const retryDelaysMs = [60_000, 300_000, 900_000];
const retryDelayScaleEnv = process.env.WUPHF_NOTARY_RETRY_DELAY_SCALE;
const originalLoad = Module._load;

if (retryDelayScaleEnv && process.env.WUPHF_RELEASE_MODE === "production") {
  throw new Error("WUPHF_NOTARY_RETRY_DELAY_SCALE is only allowed outside production mode");
}

function errorText(error) {
  return [error?.message, error?.output, error?.stdout, error?.stderr, error?.stack, String(error)]
    .filter(Boolean)
    .join("\n");
}

function isTransientNotaryError(error) {
  const text = errorText(error);
  return /(?:\b(?:ECONNRESET|ECONNREFUSED|ETIMEDOUT|EAI_AGAIN|ENOTFOUND|EHOSTUNREACH|ENETUNREACH|EPIPE)\b|NSURLErrorDomain Code=-(?:1001|1005|1009)|\b(?:network|socket hang up|timed out|timeout|temporary|temporarily|try again|service unavailable|gateway timeout|internal server error)\b|\b(?:HTTP|status(?: code)?)\s*:?\s*5\d\d\b)/i.test(
    text,
  );
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function retryDelayWithJitter(ms) {
  const retryDelayScale = Number.parseFloat(retryDelayScaleEnv || "1");
  const scaled =
    Number.isFinite(retryDelayScale) && retryDelayScale >= 0 ? ms * retryDelayScale : ms;
  const jitter = 0.8 + Math.random() * 0.4;
  return Math.round(scaled * jitter);
}

function wrapNotarize(moduleExports) {
  if (moduleExports.__wuphfNotarizeRetryWrapped || typeof moduleExports.notarize !== "function") {
    return moduleExports;
  }

  const originalNotarize = moduleExports.notarize;
  moduleExports.notarize = async function notarizeWithRetry(...args) {
    for (let attempt = 0; attempt <= retryDelaysMs.length; attempt += 1) {
      try {
        return await originalNotarize.apply(this, args);
      } catch (error) {
        const hasRetryLeft = attempt < retryDelaysMs.length;
        if (!hasRetryLeft || !isTransientNotaryError(error)) {
          throw error;
        }

        const delayMs = retryDelayWithJitter(retryDelaysMs[attempt]);
        const nextAttempt = attempt + 2;
        const maxAttempts = retryDelaysMs.length + 1;
        console.warn(
          `notarytool transient failure; retrying in ${(delayMs / 60_000).toFixed(1)} minute(s) (${nextAttempt}/${maxAttempts})`,
        );
        console.warn(errorText(error));
        await sleep(delayMs);
      }
    }
  };

  Object.defineProperty(moduleExports, "__wuphfNotarizeRetryWrapped", {
    value: true,
    enumerable: false,
  });

  return moduleExports;
}

Module._load = function loadWithNotarizeRetry(request, ...args) {
  const loaded = originalLoad.call(this, request, ...args);
  if (request === "@electron/notarize") {
    return wrapNotarize(loaded);
  }

  return loaded;
};

module.exports = {
  isTransientNotaryError,
  retryDelayWithJitter,
  retryDelaysMs,
};

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import * as api from "./upgrade";

describe("upgrade api client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("getUpgradeCheck calls the broker upgrade-check contract", async () => {
    const response: api.UpgradeCheckResponse = {
      current: "0.83.0",
      latest: "0.84.0",
      upgrade_available: true,
      is_dev_build: false,
      upgrade_command: "npm install -g wuphf@latest",
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getUpgradeCheck()).resolves.toEqual(response);
    expect(getSpy).toHaveBeenCalledWith("/upgrade-check");
  });

  it("getUpgradeChangelog passes the version range as query params", async () => {
    const response: api.UpgradeChangelogResponse = {
      commits: [
        {
          type: "fix",
          scope: "web",
          description: "keep banner contract typed",
          pr: "123",
          sha: "abc1234",
          breaking: false,
        },
      ],
    };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getUpgradeChangelog("0.83.0", "0.84.0")).resolves.toEqual(
      response,
    );
    expect(getSpy).toHaveBeenCalledWith("/upgrade-changelog", {
      from: "0.83.0",
      to: "0.84.0",
    });
  });

  it("runUpgrade posts with the broker-side timeout window", async () => {
    const response: api.UpgradeRunResult = {
      ok: true,
      install_method: "global",
      command: "npm install -g wuphf@latest",
    };
    const postSpy = vi
      .spyOn(client, "postWithTimeout")
      .mockResolvedValue(response);

    await expect(api.runUpgrade()).resolves.toEqual(response);
    expect(postSpy).toHaveBeenCalledWith("/upgrade/run", {}, 130_000);
  });
});

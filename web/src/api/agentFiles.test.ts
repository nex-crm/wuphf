import { beforeEach, describe, expect, it, vi } from "vitest";

const getMock = vi.hoisted(() => vi.fn());
const postMock = vi.hoisted(() => vi.fn());

vi.mock("./client", () => ({
  get: getMock,
  post: postMock,
}));

import { agentFilePath, readAgentFile, writeAgentFile } from "./agentFiles";

describe("agentFiles client", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("builds canonical agent file paths", () => {
    expect(agentFilePath("ceo", "SOUL")).toBe("agents/ceo/SOUL.md");
    expect(agentFilePath("growth-ops", "OPERATIONS")).toBe(
      "agents/growth-ops/OPERATIONS.md",
    );
  });

  it("reads a file via GET /agent-files/read with the path query", async () => {
    getMock.mockResolvedValue({
      path: "agents/ceo/SOUL.md",
      content: "# SOUL",
      sha: "abc",
      exists: true,
    });
    const res = await readAgentFile("agents/ceo/SOUL.md");
    expect(getMock).toHaveBeenCalledWith("/agent-files/read", {
      path: "agents/ceo/SOUL.md",
    });
    expect(res.sha).toBe("abc");
  });

  it("returns the OK envelope on a successful write", async () => {
    postMock.mockResolvedValue({
      path: "agents/ceo/SOUL.md",
      commit_sha: "def456",
      bytes_written: 42,
    });
    const res = await writeAgentFile({
      path: "agents/ceo/SOUL.md",
      content: "# SOUL\nv2",
      commitMessage: "tighten",
      expectedSha: "abc123",
    });
    expect(postMock).toHaveBeenCalledWith("/agent-files/write", {
      path: "agents/ceo/SOUL.md",
      content: "# SOUL\nv2",
      commit_message: "tighten",
      expected_sha: "abc123",
    });
    expect("commit_sha" in res && res.commit_sha).toBe("def456");
  });

  it("parses a 409 error body into a typed conflict", async () => {
    // The shared post() helper surfaces non-2xx as Error(text); for 409 the text
    // is the JSON conflict envelope.
    postMock.mockRejectedValue(
      new Error(
        JSON.stringify({
          error: "file changed since it was opened",
          current_sha: "newsha",
          current_content: "# SOUL\nsomeone else edited",
        }),
      ),
    );
    const res = await writeAgentFile({
      path: "agents/ceo/SOUL.md",
      content: "# SOUL\nmy edit",
      commitMessage: "x",
      expectedSha: "stale",
    });
    expect("conflict" in res).toBe(true);
    if ("conflict" in res) {
      expect(res.current_sha).toBe("newsha");
      expect(res.current_content).toContain("someone else edited");
    }
  });

  it("rethrows a non-conflict error", async () => {
    postMock.mockRejectedValue(new Error("human session required"));
    await expect(
      writeAgentFile({
        path: "agents/ceo/SOUL.md",
        content: "x",
        commitMessage: "x",
        expectedSha: "",
      }),
    ).rejects.toThrow("human session required");
  });
});

/// <reference types="node" />

import { describe, expect, it } from "vitest";

import {
  extractRichArtifactIds,
  stripStandaloneRichArtifactReferenceLines,
} from "./richArtifactReferences";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

interface TutorialManifest {
  version: number;
  scenarios: TutorialScenario[];
}

interface TutorialScenario {
  slug: string;
  title: string;
  summary: string;
  actorSlug: string;
  sourceMarkdownPath: string;
  targetWikiPath: string;
  htmlPath: string;
  sourcePath: string;
  wikiPath: string;
  chatPath: string;
  expectedChatArtifactId: string;
  expectedTerms: string[];
}

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, "../../..");
const TUTORIAL_ROOT = path.join(
  REPO_ROOT,
  "docs",
  "tutorials",
  "rich-html-artifacts",
);

function readTutorialFile(relPath: string): string {
  return readFileSync(path.join(TUTORIAL_ROOT, relPath), "utf8");
}

const manifest = JSON.parse(
  readTutorialFile("scenarios.json"),
) as TutorialManifest;

describe("rich artifact tutorial fixtures", () => {
  it("ships multiple ICP scenarios", () => {
    expect(manifest.version).toBe(1);
    expect(manifest.scenarios.length).toBeGreaterThanOrEqual(2);
  });

  for (const scenario of manifest.scenarios) {
    it(`${scenario.slug}: chat marker renders as a rich artifact reference`, () => {
      const chat = readTutorialFile(scenario.chatPath);

      expect(extractRichArtifactIds(chat)).toEqual([
        scenario.expectedChatArtifactId,
      ]);
      expect(chat).toMatch(
        new RegExp(
          `^\\s*visual-artifact:${scenario.expectedChatArtifactId}\\s*$`,
          "m",
        ),
      );

      const stripped = stripStandaloneRichArtifactReferenceLines(chat);
      expect(stripped).not.toContain(scenario.expectedChatArtifactId);
      expect(extractRichArtifactIds(stripped)).toEqual([]);
      expect(stripped).toContain("HTML");
    });

    it(`${scenario.slug}: HTML artifact is interactive and keeps expected review cues`, () => {
      const html = readTutorialFile(scenario.htmlPath);

      expect(html).toContain("<script>");
      expect(html).toContain("addEventListener");
      for (const term of scenario.expectedTerms) {
        expect(html).toContain(term);
      }
    });
  }
});

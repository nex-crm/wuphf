import { describe, expect, it } from "vitest";

import type { WikiFSTreeNode } from "../../../api/wiki";
import {
  baseName,
  dropToMove,
  findNode,
  flattenTree,
  isLeaf,
  parentDir,
} from "./treeModel";

const tree: WikiFSTreeNode[] = [
  {
    name: "people",
    path: "team/people",
    type: "dir",
    title: "People",
    children: [
      {
        name: "nazz.md",
        path: "team/people/nazz.md",
        type: "page",
        title: "Nazz",
      },
      {
        name: "customers",
        path: "team/people/customers",
        type: "dir",
        title: "Customers",
        children: [
          {
            name: "acme.md",
            path: "team/people/customers/acme.md",
            type: "page",
            title: "Acme",
          },
        ],
      },
    ],
  },
  {
    name: "playbooks",
    path: "team/playbooks",
    type: "dir",
    title: "Playbooks",
    children: [
      {
        name: "churn.md",
        path: "team/playbooks/churn.md",
        type: "page",
        title: "Churn",
      },
    ],
  },
];

describe("treeModel helpers", () => {
  it("parentDir / baseName split a path", () => {
    expect(parentDir("team/people/nazz.md")).toBe("team/people");
    expect(baseName("team/people/nazz.md")).toBe("nazz.md");
    expect(parentDir("solo")).toBe("");
    expect(baseName("solo")).toBe("solo");
  });

  it("isLeaf is true for non-dir nodes", () => {
    expect(isLeaf({ ...tree[0], type: "dir" })).toBe(false);
    expect(isLeaf({ ...tree[0], type: "page" })).toBe(true);
    expect(isLeaf({ ...tree[0], type: "website" })).toBe(true);
  });

  it("findNode locates nested nodes by path", () => {
    expect(findNode(tree, "team/people/customers/acme.md")?.title).toBe("Acme");
    expect(findNode(tree, "team/missing.md")).toBeNull();
  });

  it("flattenTree only descends into expanded folders", () => {
    const collapsed = flattenTree(tree, () => false).map((r) => r.node.path);
    expect(collapsed).toEqual(["team/people", "team/playbooks"]);

    const allOpen = flattenTree(tree, () => true).map((r) => r.node.path);
    expect(allOpen).toEqual([
      "team/people",
      "team/people/nazz.md",
      "team/people/customers",
      "team/people/customers/acme.md",
      "team/playbooks",
      "team/playbooks/churn.md",
    ]);
  });

  it("flattenTree records depth and parent path", () => {
    const rows = flattenTree(tree, () => true);
    const acme = rows.find(
      (r) => r.node.path === "team/people/customers/acme.md",
    );
    expect(acme?.depth).toBe(2);
    expect(acme?.parentPath).toBe("team/people/customers");
    const people = rows.find((r) => r.node.path === "team/people");
    expect(people?.depth).toBe(0);
    expect(people?.parentPath).toBeNull();
  });
});

function mustFind(path: string): WikiFSTreeNode {
  const node = findNode(tree, path);
  if (!node) throw new Error(`fixture missing node ${path}`);
  return node;
}

describe("dropToMove", () => {
  const playbooksDir = mustFind("team/playbooks");
  const customersDir = mustFind("team/people/customers");
  const peopleDir = mustFind("team/people");
  const churnPage = mustFind("team/playbooks/churn.md");

  it("maps a page dropped into a folder to {from,to}", () => {
    expect(dropToMove("team/people/nazz.md", playbooksDir)).toEqual({
      from: "team/people/nazz.md",
      to: "team/playbooks/nazz.md",
    });
  });

  it("resolves a drop onto a leaf to that leaf's folder", () => {
    expect(dropToMove("team/people/nazz.md", churnPage)).toEqual({
      from: "team/people/nazz.md",
      to: "team/playbooks/nazz.md",
    });
  });

  it("returns null when the page already lives in the destination", () => {
    expect(dropToMove("team/playbooks/churn.md", playbooksDir)).toBeNull();
  });

  it("returns null when dropping a node onto itself", () => {
    expect(dropToMove("team/people", peopleDir)).toBeNull();
  });

  it("refuses to move a folder into its own descendant", () => {
    expect(dropToMove("team/people", customersDir)).toBeNull();
  });
});

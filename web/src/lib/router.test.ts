import { describe, expect, it } from "vitest";

import { pathDeepLinkToHashURL } from "./router";

// Regression for the deep-link routing bug: the broker's spaFileServer serves
// index.html for any path-form deep link, but the router runs on hash history,
// so a direct load of `/apps/app_<id>` (or any path-form URL) booted the home
// composer instead of the linked surface. pathDeepLinkToHashURL is the boot
// bridge that rewrites a path-form deep link into the canonical hash form the
// router reads. These pin the mapping at the layer the bug lived (URL parse),
// not a downstream symptom.

describe("pathDeepLinkToHashURL (path deep link → hash form)", () => {
  it("rewrites a path-form custom-app deep link into the hash form", () => {
    expect(
      pathDeepLinkToHashURL({
        pathname: "/apps/app_135210fa47bd596e",
        search: "",
        hash: "",
      }),
    ).toBe("/#/apps/app_135210fa47bd596e");
  });

  it("rewrites other path-form deep links (tasks, wiki) the same way", () => {
    expect(
      pathDeepLinkToHashURL({
        pathname: "/tasks/OFFICE-41",
        search: "",
        hash: "",
      }),
    ).toBe("/#/tasks/OFFICE-41");
    expect(
      pathDeepLinkToHashURL({
        pathname: "/wiki/team/people/x.md",
        search: "",
        hash: "",
      }),
    ).toBe("/#/wiki/team/people/x.md");
  });

  it("carries the query string into the hash route", () => {
    expect(
      pathDeepLinkToHashURL({
        pathname: "/wiki/lookup",
        search: "?q=onboarding",
        hash: "",
      }),
    ).toBe("/#/wiki/lookup?q=onboarding");
  });

  it("treats a bare `#` / `#/` as no client route and still bridges", () => {
    expect(
      pathDeepLinkToHashURL({ pathname: "/apps/app_x", search: "", hash: "#" }),
    ).toBe("/#/apps/app_x");
    expect(
      pathDeepLinkToHashURL({
        pathname: "/apps/app_x",
        search: "",
        hash: "#/",
      }),
    ).toBe("/#/apps/app_x");
  });

  it("leaves the root path untouched (already the home composer)", () => {
    expect(
      pathDeepLinkToHashURL({ pathname: "/", search: "", hash: "" }),
    ).toBeNull();
  });

  it("never clobbers an existing hash deep link", () => {
    // Canonical hash form: pathname is "/" so there is nothing to bridge.
    expect(
      pathDeepLinkToHashURL({
        pathname: "/",
        search: "",
        hash: "#/apps/app_x",
      }),
    ).toBeNull();
    // Defensive: even if both a path and a real hash route are present, the
    // hash is the source of truth and must win.
    expect(
      pathDeepLinkToHashURL({
        pathname: "/apps/app_x",
        search: "",
        hash: "#/tasks/OFFICE-1",
      }),
    ).toBeNull();
  });
});

import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { ErrorBox, categorizeError, getSuggestions } from "../../../src/tui/components/error-box.js";
import type { ErrorCategory } from "../../../src/tui/components/error-box.js";
import { SuccessBox } from "../../../src/tui/components/success-box.js";

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

// ─── ErrorBox ───

describe("ErrorBox", () => {
  it("renders error message", () => {
    const { lastFrame } = render(
      <ErrorBox message="Something went wrong" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Something went wrong"), "should show error message");
  });

  it("shows error header", () => {
    const { lastFrame } = render(
      <ErrorBox message="fail" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Error"), "should show Error header");
  });

  it("shows suggestions for auth category", () => {
    const { lastFrame } = render(
      <ErrorBox message="No API key" category="auth" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("nex init"), "should suggest nex init for auth errors");
    assert.ok(frame.includes("NEX_API_KEY"), "should suggest env var for auth errors");
  });

  it("shows suggestions for rate-limit category", () => {
    const { lastFrame } = render(
      <ErrorBox message="Too many requests" category="rate-limit" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("retry"), "should suggest retry for rate limits");
  });

  it("shows suggestions for network category", () => {
    const { lastFrame } = render(
      <ErrorBox message="Connection failed" category="network" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("internet connection"), "should suggest checking connection");
  });

  it("shows suggestions for server category", () => {
    const { lastFrame } = render(
      <ErrorBox message="500 Internal" category="server" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("retry"), "should suggest retry for server errors");
  });

  it("shows default suggestions for unknown category", () => {
    const { lastFrame } = render(
      <ErrorBox message="Mystery error" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("--verbose"), "should suggest verbose flag");
  });

  it("uses custom suggestions when provided", () => {
    const { lastFrame } = render(
      <ErrorBox
        message="Custom fail"
        suggestions={["Try doing X", "Or try Y"]}
      />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Try doing X"), "should show custom suggestion 1");
    assert.ok(frame.includes("Or try Y"), "should show custom suggestion 2");
  });

  it("shows Suggestions label", () => {
    const { lastFrame } = render(
      <ErrorBox message="fail" category="auth" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Suggestions"), "should show Suggestions label");
  });
});

// ─── categorizeError ───

describe("categorizeError", () => {
  it("detects auth errors by name", () => {
    const err = new Error("bad");
    err.name = "AuthError";
    assert.equal(categorizeError(err), "auth");
  });

  it("detects auth errors by message", () => {
    const err = new Error("No API key configured");
    assert.equal(categorizeError(err), "auth");
  });

  it("detects rate limit errors", () => {
    const err = new Error("Rate limited");
    err.name = "RateLimitError";
    assert.equal(categorizeError(err), "rate-limit");
  });

  it("detects network errors", () => {
    const err = new Error("fetch failed");
    assert.equal(categorizeError(err), "network");
  });

  it("detects ECONNREFUSED as network", () => {
    const err = new Error("connect ECONNREFUSED 127.0.0.1:443");
    assert.equal(categorizeError(err), "network");
  });

  it("detects server errors", () => {
    const err = new Error("Nex API error 500");
    err.name = "ServerError";
    assert.equal(categorizeError(err), "server");
  });

  it("returns unknown for unrecognized errors", () => {
    const err = new Error("something bizarre");
    assert.equal(categorizeError(err), "unknown");
  });
});

// ─── getSuggestions ───

describe("getSuggestions", () => {
  it("returns non-empty array for every category", () => {
    const categories: ErrorCategory[] = ["auth", "rate-limit", "network", "server", "unknown"];
    for (const cat of categories) {
      const tips = getSuggestions(cat);
      assert.ok(tips.length > 0, `${cat} should have suggestions`);
    }
  });
});

// ─── SuccessBox ───

describe("SuccessBox", () => {
  it("renders success message", () => {
    const { lastFrame } = render(
      <SuccessBox message="Record created" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Record created"), "should show success message");
  });

  it("shows checkmark indicator", () => {
    const { lastFrame } = render(
      <SuccessBox message="Done" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("\u2714"), "should show checkmark");
  });

  it("shows detail when provided", () => {
    const { lastFrame } = render(
      <SuccessBox message="Saved" detail="ID: rec_abc123" />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("rec_abc123"), "should show detail text");
  });

  it("omits detail line when not provided", () => {
    const { lastFrame } = render(
      <SuccessBox message="OK" />,
    );
    const frame = strip(lastFrame() ?? "");
    const lines = frame.split("\n").filter((l) => strip(l).trim().length > 0);
    // Should only have border lines + the message line, no detail line
    assert.ok(!frame.includes("undefined"), "should not show undefined");
  });
});

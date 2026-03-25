/**
 * Entry point for TUI mode.
 */

import React from "react";
import { render } from "ink";
import { App } from "./app.js";

export function startTui(): void {
  render(<App />);
}

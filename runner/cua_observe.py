#!/usr/bin/env python3
"""cua_observe.py — OBSERVE capture for the demo call (C5, slice 1).

While the operator demonstrates a workflow (narrating over the realtime voice
call), this polls the FRONTMOST window every few seconds and captures its REAL
structure — the component tree (AX), the HTML-level selectors (DOM via the page
tool), and the visible text — diffing across ticks to infer the workflow steps.
This is how the build AI reads the actual page instead of guessing from
screenshots.

The recorder (start_recording) is deliberately NOT used: it only logs
cua-driver's OWN action calls, but here the human acts manually, so the event
log comes from snapshot diffs. cua-driver runs in the background (no focus
steal), so this never interrupts the operator, and it lives off the realtime
voice path so it adds no call latency. See docs/specs/operator-cua-migration.md.

Emits the FULL snapshot per tick (so nothing is lost if the process is killed),
a `navigate` event whenever the screen changes, and a final summary record. The
broker streams these and assembles the handoff for the build.
"""

import argparse
import os
import signal
import time

from cua_common import (
    _EXCLUDED_ROLES,
    _TEXT_ROLES,
    cua,
    emit,
    find_window,
    frontmost_app_name,
    redact_label,
)

INTERVAL = float(os.environ.get("WUPHF_OBSERVE_INTERVAL", "1.8"))
MAX_COMPONENTS = 60
TEXT_EXCERPT = 800
# Short, bounded timeouts so a slow window never stalls a tick (latency guard).
AX_TIMEOUT = 12
PAGE_TIMEOUT = 8
# The `page` get_text read only works on browser-family windows; everywhere else
# we rely on the AX component digest, which works on any app.
#
# NOTE: real HTML selectors (id/name/href) + the page URL would need
# `page execute_javascript`, which on macOS requires a one-time "allow JS from
# Apple Events" opt-in + a Chrome restart. query_dom without it just re-returns
# the AX tree, so we skip it here and capture AX components + visible text. The
# execute_javascript upgrade (real DOM + URL) is a follow-up, gated on that opt-in.
_BROWSERS = {
    "Google Chrome",
    "Brave Browser",
    "Microsoft Edge",
    "Safari",
    "Arc",
    "Chromium",
    "Orion",
    "Vivaldi",
}

_stop = False


def _on_signal(*_):
    global _stop
    _stop = True


def components(pid, window_id):
    """The AX component digest: roles + labels, menu bar excluded, secrets redacted."""
    state = cua(
        "get_window_state",
        {"pid": pid, "window_id": window_id, "capture_mode": "ax", "max_elements": 300},
        timeout=AX_TIMEOUT,
    )
    out = []
    if isinstance(state, dict):
        for e in state.get("elements", []):
            role = e.get("role", "")
            if role in _EXCLUDED_ROLES:
                continue
            label, _ = redact_label(role, e.get("label") or e.get("title") or "")
            if not label and role not in _TEXT_ROLES:
                continue
            out.append({"role": role.replace("AX", ""), "label": (label or "(empty field)")[:80]})
            if len(out) >= MAX_COMPONENTS:
                break
    return out


def visible_text(pid, window_id):
    res = cua(
        "page",
        {"action": "get_text", "pid": pid, "window_id": window_id},
        timeout=PAGE_TIMEOUT,
    )
    text = res if isinstance(res, str) else (res.get("text", "") if isinstance(res, dict) else "")
    return " ".join(text.split())[:TEXT_EXCERPT]


def snapshot(tick):
    """Capture the current frontmost window's structure. None if nothing on screen.

    AX components work on any app; the DOM/text `page` reads only apply to a
    browser window, so we skip them elsewhere (and never block on them)."""
    app = frontmost_app_name()
    if not app:
        return None
    win = find_window(app)
    if not win:
        return None
    pid, window_id, title = win
    snap = {
        "tick": tick,
        "ts": round(time.time(), 1),
        "app": app,
        "title": title,
        "components": components(pid, window_id),
    }
    if app in _BROWSERS:
        snap["text_excerpt"] = visible_text(pid, window_id)
    return snap


def run(interval, max_ticks, duration):
    emit({"type": "status", "status": "observing", "interval": interval})
    started = round(time.time(), 1)
    apps_seen = []
    events = []
    prev = None
    tick = 0
    while not _stop:
        if max_ticks and tick >= max_ticks:
            break
        if duration and (time.time() - started) >= duration:
            break
        snap = snapshot(tick)
        if snap:
            if prev is None or snap["app"] != prev["app"] or snap["title"] != prev["title"]:
                ev = {"tick": tick, "type": "navigate", "app": snap["app"], "title": snap["title"]}
                events.append(ev)
                emit({"type": "event", **ev})
            if snap["app"] not in apps_seen:
                apps_seen.append(snap["app"])
            # Full snapshot per tick so a hard kill still leaves the consumer the data.
            emit({"type": "snapshot", **snap})
            prev = snap
            tick += 1
        slept = 0.0
        while slept < interval and not _stop:
            time.sleep(0.2)
            slept += 0.2
    emit({
        "type": "record",
        "started": started,
        "ended": round(time.time(), 1),
        "ticks": tick,
        "apps_seen": apps_seen,
        "events": events,
    })


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--interval", type=float, default=INTERVAL)
    ap.add_argument("--max-ticks", type=int, default=0, help="stop after N snapshots (0 = until killed)")
    ap.add_argument("--duration", type=float, default=0, help="stop after N seconds (0 = until killed)")
    args = ap.parse_args()
    signal.signal(signal.SIGINT, _on_signal)
    signal.signal(signal.SIGTERM, _on_signal)
    run(args.interval, args.max_ticks, args.duration)


if __name__ == "__main__":
    main()

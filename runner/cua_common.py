#!/usr/bin/env python3
"""cua_common.py — shared cua-driver helpers for the runner scripts.

Both the EXECUTE runner (cua_exec.py) and the OBSERVE capture (cua_observe.py)
talk to cua-driver the same way: shell out to its `call` CLI and parse the JSON.
They also share the same trust rules for the accessibility tree — it is untrusted
page content, so secret-named labels are redacted and the macOS app menu bar is
excluded. Keeping that in one place means a fix to either applies to both.
"""

import json
import os
import re
import subprocess
import sys

CUA = os.path.expanduser(os.environ.get("WUPHF_CUA_DRIVER", "~/.local/bin/cua-driver"))


def emit(obj):
    """Write one JSON event line to stdout (the broker proxies these as SSE)."""
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def cua(tool, args=None, timeout=30):
    """Invoke a cua-driver tool via its `call` CLI; return parsed JSON (or text).

    A timeout returns an `_error` marker rather than raising, so a slow tool (e.g.
    query_dom on a heavy page) degrades the snapshot gracefully instead of
    stalling the capture loop — keeping the observe path off any latency budget.
    """
    try:
        proc = subprocess.run(
            [CUA, "call", tool, json.dumps(args or {})],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        return {"_error": f"{tool} timed out after {timeout}s"}
    out = proc.stdout.strip()
    try:
        return json.loads(out)
    except Exception:
        return out or proc.stderr.strip()


# Labels matching these never cross the model boundary — they may hold or name
# secrets. We send the role only, with a redacted placeholder.
_SENSITIVE = re.compile(
    r"password|passcode|secret|token|api[\s_-]?key|ssn|social security|credit|card|cvv|"
    r"security code|otp|2fa|seed phrase|private key",
    re.I,
)
_TEXT_ROLES = ("AXTextField", "AXTextArea", "AXSearchField", "AXComboBox")

# The macOS app menu bar (Chrome ▸ File ▸ …) shows up in the AX walk but is NOT
# page content — never include it.
_EXCLUDED_ROLES = {
    "AXMenuBar",
    "AXMenuBarItem",
    "AXMenuItem",
    "AXMenu",
    "AXMenuButton",
}


def redact_label(role, raw_label):
    """Return (label, is_secure). Secret-named or secure fields get a placeholder
    so no field text crosses to the model — only structure does."""
    label = str(raw_label or "").strip()
    if role == "AXSecureTextField" or _SENSITIVE.search(label):
        return f"[redacted {role.replace('AX', '') or 'field'}]", True
    return label, False


def list_windows():
    wins = cua("list_windows")
    return wins if isinstance(wins, list) else wins.get("windows", []) if isinstance(wins, dict) else []


def list_apps():
    apps = cua("list_apps")
    return apps if isinstance(apps, list) else apps.get("apps", []) if isinstance(apps, dict) else []


def frontmost_app_name():
    for a in list_apps():
        if isinstance(a, dict) and (a.get("frontmost") or a.get("is_frontmost") or a.get("active")):
            return a.get("name")
    return None


def find_window(app_name):
    """The largest on-screen window for an app — its main content window."""
    cands = [
        w
        for w in list_windows()
        if isinstance(w, dict)
        and w.get("app_name") == app_name
        and w.get("is_on_screen")
        and w.get("bounds", {}).get("height", 0) > 300
    ]
    if not cands:
        return None
    cands.sort(key=lambda w: -w["bounds"]["height"] * w["bounds"]["width"])
    return cands[0]["pid"], cands[0]["window_id"], cands[0].get("title", "")


def window_by_id(window_id):
    for w in list_windows():
        if isinstance(w, dict) and w.get("window_id") == window_id:
            return w.get("pid"), window_id, w.get("title", "")
    return None


def frontmost_window():
    """The largest on-screen content window of whatever app is frontmost — the
    thing the operator is currently demonstrating."""
    app = frontmost_app_name()
    if not app:
        return None
    return find_window(app)

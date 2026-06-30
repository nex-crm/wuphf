#!/usr/bin/env python3
"""cua_exec.py — the cua execution runner (C1).

Drive the operator's REAL browser/desktop to accomplish a goal, using cua-driver
(the native, background, AX-first computer-use driver) as the "computer" and an
OpenAI model as the planner. cua-driver's screenshot path is finicky and it
recommends element_index over pixels, so the loop is AX-tree-based: read the
window's accessibility tree, let the model pick an element to click / text to
type, execute via cua-driver, repeat. This works on backgrounded windows and
needs no screenshot. See docs/specs/operator-cua-migration.md.

Emits one JSON line per step on stdout (the ExecSession shape the Run modal
speaks): {type:"status"|"action"|"done"|"error", ...}. The broker proxies these
as SSE.

Standalone:
  OPENAI_API_KEY=sk-... python3 runner/cua_exec.py --app "Google Chrome" \
      --goal "On this page, open the 'Booking a car' help section"
"""

import argparse
import json
import os
import subprocess
import sys
import urllib.request

CUA = os.path.expanduser("~/.local/bin/cua-driver")
MODEL = os.environ.get("WUPHF_CUA_PLANNER_MODEL", "gpt-4o")
MAX_STEPS = int(os.environ.get("WUPHF_CUA_MAX_STEPS", "15"))
MAX_ELEMENTS = 120  # bound the AX tree we send to the model


def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


def cua(tool, args=None):
    """Invoke a cua-driver tool via its `call` CLI; return parsed JSON (or text)."""
    proc = subprocess.run(
        [CUA, "call", tool, json.dumps(args or {})],
        capture_output=True,
        text=True,
        timeout=60,
    )
    out = proc.stdout.strip()
    try:
        return json.loads(out)
    except Exception:
        return out or proc.stderr.strip()


def find_window(app_name):
    """Find the largest on-screen window for an app — its main content window."""
    wins = cua("list_windows")
    wins = wins if isinstance(wins, list) else wins.get("windows", [])
    cands = [
        w
        for w in wins
        if isinstance(w, dict)
        and w.get("app_name") == app_name
        and w.get("is_on_screen")
        and w.get("bounds", {}).get("height", 0) > 300
    ]
    if not cands:
        return None
    cands.sort(key=lambda w: -w["bounds"]["height"] * w["bounds"]["width"])
    return cands[0]["pid"], cands[0]["window_id"], cands[0].get("title", "")


def snapshot(pid, window_id):
    """AX snapshot: return the actionable elements (index, role, label, value)."""
    state = cua(
        "get_window_state",
        {
            "pid": pid,
            "window_id": window_id,
            "capture_mode": "ax",
            "max_elements": 400,
        },
    )
    if not isinstance(state, dict):
        return [], str(state)
    elements = state.get("elements", [])
    actionable = []
    for e in elements:
        idx = e.get("element_index")
        if idx is None:
            continue
        role = e.get("role", "")
        label = e.get("label") or e.get("title") or e.get("value") or ""
        if not str(label).strip() and role not in (
            "AXTextField",
            "AXTextArea",
            "AXSearchField",
        ):
            continue
        actionable.append(
            {
                "i": idx,
                "role": role.replace("AX", ""),
                "label": str(label)[:80],
            }
        )
        if len(actionable) >= MAX_ELEMENTS:
            break
    return actionable, ""


TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "click",
            "description": "Click a UI element by its element_index (the 'i' from the elements list).",
            "parameters": {
                "type": "object",
                "properties": {
                    "i": {"type": "integer", "description": "element_index to click"},
                    "reason": {"type": "string"},
                },
                "required": ["i", "reason"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "type_text",
            "description": "Type text into a focused text field. Click the field first.",
            "parameters": {
                "type": "object",
                "properties": {
                    "i": {"type": "integer", "description": "element_index of the field"},
                    "text": {"type": "string"},
                    "reason": {"type": "string"},
                },
                "required": ["i", "text", "reason"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "press_key",
            "description": "Press a single key (e.g. Enter, Tab, Escape).",
            "parameters": {
                "type": "object",
                "properties": {
                    "key": {"type": "string"},
                    "reason": {"type": "string"},
                },
                "required": ["key", "reason"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "done",
            "description": "The goal is complete. Report what was accomplished.",
            "parameters": {
                "type": "object",
                "properties": {"result": {"type": "string"}},
                "required": ["result"],
            },
        },
    },
]


def plan(api_key, messages):
    body = json.dumps(
        {"model": MODEL, "messages": messages, "tools": TOOLS, "tool_choice": "required"}
    ).encode()
    req = urllib.request.Request(
        "https://api.openai.com/v1/chat/completions",
        data=body,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read())


def run(goal, app_name, api_key, dry_run=False):
    found = find_window(app_name)
    if not found:
        emit({"type": "error", "message": f"No on-screen window for {app_name}"})
        return
    pid, window_id, title = found
    emit({"type": "status", "status": "running", "detail": f"{app_name}: {title}"})

    sys_prompt = (
        "You drive a real application by choosing ONE action per turn from the tools. "
        "Each turn you get the window's actionable UI elements as a list of {i, role, label}. "
        "Pick the element whose role+label best advances the goal. To enter text, click the "
        "field, then type_text into it. Call done when the goal is achieved. Be decisive; "
        "do not repeat the same action."
    )
    messages = [
        {"role": "system", "content": sys_prompt},
        {"role": "user", "content": f"Goal: {goal}"},
    ]

    for _ in range(MAX_STEPS):
        elements, err = snapshot(pid, window_id)
        if err:
            emit({"type": "error", "message": f"snapshot: {err}"})
            return
        messages.append(
            {
                "role": "user",
                "content": "Current actionable elements:\n"
                + json.dumps(elements, separators=(",", ":")),
            }
        )
        emit({"type": "status", "status": "thinking"})
        resp = plan(api_key, messages)
        msg = resp["choices"][0]["message"]
        calls = msg.get("tool_calls") or []
        if not calls:
            emit({"type": "done", "result": msg.get("content") or "Stopped."})
            return
        call = calls[0]
        name = call["function"]["name"]
        a = json.loads(call["function"]["arguments"] or "{}")
        messages.append(msg)

        if name == "done":
            emit({"type": "done", "result": a.get("result", "Done.")})
            return

        # In dry-run we surface the chosen action but never touch the app.
        if name == "click":
            tgt = next((e for e in elements if e["i"] == a["i"]), {})
            label = f"Click {tgt.get('role','')} “{tgt.get('label','')}” [{a['i']}]"
            if not dry_run:
                cua("click", {"pid": pid, "window_id": window_id, "element_index": a["i"]})
        elif name == "type_text":
            label = f"Type “{a['text']}” into [{a['i']}]"
            if not dry_run:
                cua("click", {"pid": pid, "window_id": window_id, "element_index": a["i"]})
                cua(
                    "type_text",
                    {"pid": pid, "window_id": window_id, "element_index": a["i"], "text": a["text"]},
                )
        elif name == "press_key":
            label = f"Press {a['key']}"
            if not dry_run:
                cua("press_key", {"pid": pid, "key": a["key"]})
        else:
            label = name

        emit(
            {
                "type": "action",
                "label": label,
                "reasoning": a.get("reason", ""),
                "tool": name,
                "dry_run": dry_run,
            }
        )
        if dry_run:
            emit({"type": "done", "result": "Dry run — first action shown, nothing executed."})
            return
        messages.append(
            {
                "role": "tool",
                "tool_call_id": call["id"],
                "content": f"executed: {label}",
            }
        )

    emit({"type": "done", "result": "Reached the step limit."})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--goal", required=True)
    ap.add_argument("--app", default="Google Chrome")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    key = os.environ.get("OPENAI_API_KEY")
    if not key:
        cfg = os.path.expanduser("/private/tmp/wuphf-operator-home/.wuphf/config.json")
        try:
            key = json.load(open(cfg)).get("openai_api_key")
        except Exception:
            pass
    if not key:
        emit({"type": "error", "message": "OPENAI_API_KEY not set"})
        sys.exit(1)
    try:
        run(args.goal, args.app, key, dry_run=args.dry_run)
    except Exception as e:
        emit({"type": "error", "message": str(e)})
        sys.exit(1)


if __name__ == "__main__":
    main()

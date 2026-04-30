#!/usr/bin/env python3
"""Skill end-to-end evaluation harness.

Drives the /skills surface through a series of contract scenarios against
a running broker and asserts pass/fail per the skill lifecycle spec.

Scenarios:
  1. list-empty   — GET /skills returns an empty or baseline list on a
                    fresh-state broker.
  2. create       — POST /skills with action=create succeeds and the skill
                    appears in GET /skills.
  3. duplicate    — A second POST with the same name returns 409.
  4. invoke       — POST /skills/{name}/invoke returns 200 and increments
                    usage_count.
  5. archive      — DELETE /skills removes the skill from GET /skills.

HOME isolation
--------------
The harness optionally rewrites HOME before spawning any subprocess so that
a broker launched as a side-effect of the eval doesn't pollute the real
~/.wuphf directory.  The guard prevents the familiar path-doubling bug that
occurs when the caller's shell already has HOME pointed at the dev-home
directory:

  BAD (doubles when HOME is already ~/.wuphf-dev-home):
      home = os.path.join(os.getenv("HOME"), ".wuphf-dev-home")

  GOOD (idempotent):
      home = _dev_home()   # see below — checks suffix before appending

Usage:
  python3 scripts/skill_eval/eval.py \\
      --broker http://localhost:7899 \\
      --token-file /tmp/wuphf-broker-token-7899 \\
      --home                      # enable HOME isolation (dev broker)
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


# ---------- HOME isolation ----------


_DEV_HOME_SUFFIX = "/.wuphf-dev-home"


def _dev_home() -> str:
    """Return the dev-isolated HOME path without doubling.

    If HOME already ends with ``/.wuphf-dev-home`` (e.g. the caller is
    running inside the wuphf-dev shell alias that sets
    ``HOME=~/.wuphf-dev-home``) we return it unchanged.  Otherwise we
    append the suffix so the broker subprocess writes into the isolated
    directory instead of the real ``~/.wuphf``.
    """
    home = os.path.normpath(os.environ.get("HOME", os.path.expanduser("~")))
    if home.endswith(_DEV_HOME_SUFFIX):
        return home
    return home + _DEV_HOME_SUFFIX


def apply_home_isolation() -> str:
    """Set HOME to the dev-isolated path and return the resolved value."""
    isolated = _dev_home()
    os.environ["HOME"] = isolated
    return isolated


# ---------- HTTP helpers ----------


def http_request(
    method: str,
    url: str,
    *,
    token: str,
    body: dict[str, Any] | None = None,
    timeout: float = 15.0,
) -> tuple[int, dict[str, Any] | str]:
    data = None
    if body is not None:
        data = json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            try:
                return resp.status, json.loads(raw)
            except json.JSONDecodeError:
                return resp.status, raw
    except urllib.error.HTTPError as e:
        try:
            payload = e.read().decode()
        except Exception:
            payload = str(e)
        try:
            return e.code, json.loads(payload)
        except (json.JSONDecodeError, ValueError):
            return e.code, payload


# ---------- Result types ----------


@dataclass
class ScenarioResult:
    id: str
    title: str
    ok: bool
    detail: str = ""


@dataclass
class SkillEvalReport:
    scenarios: list[ScenarioResult] = field(default_factory=list)


# ---------- State reset ----------


_EVAL_SKILL_NAME = "skill-eval-probe"


def reset_skill_state(broker: str, token: str) -> None:
    """Delete the eval probe skill if it was left behind by a previous run."""
    http_request(
        "DELETE",
        f"{broker}/skills",
        token=token,
        body={"name": _EVAL_SKILL_NAME},
    )


# ---------- Scenarios ----------


def scenario_list_empty(broker: str, token: str) -> ScenarioResult:
    code, body = http_request("GET", f"{broker}/skills", token=token)
    if code != 200:
        return ScenarioResult(
            id="skill-01-list",
            title="GET /skills succeeds",
            ok=False,
            detail=f"status={code}",
        )
    if not isinstance(body, dict):
        return ScenarioResult(
            id="skill-01-list",
            title="GET /skills succeeds",
            ok=False,
            detail="non-JSON response body",
        )
    skills = body.get("skills", [])
    probe = next((s for s in skills if s.get("name") == _EVAL_SKILL_NAME), None)
    if probe is not None:
        return ScenarioResult(
            id="skill-01-list",
            title="GET /skills succeeds",
            ok=False,
            detail=f"stale probe skill present; reset_skill_state failed",
        )
    return ScenarioResult(
        id="skill-01-list",
        title="GET /skills succeeds",
        ok=True,
        detail=f"{len(skills)} skill(s) in list",
    )


def scenario_create(broker: str, token: str) -> ScenarioResult:
    code, body = http_request(
        "POST",
        f"{broker}/skills",
        token=token,
        body={
            "action": "create",
            "name": _EVAL_SKILL_NAME,
            "title": "Skill Eval Probe",
            "description": "Transient skill created by the skill_eval harness.",
            "content": "Step 1: probe. Step 2: assert. Step 3: clean up.",
            "created_by": "eval-harness",
            "channel": "general",
            "tags": ["eval"],
        },
    )
    ok = code in (200, 201)
    return ScenarioResult(
        id="skill-02-create",
        title=f"POST /skills action=create",
        ok=ok,
        detail=f"status={code}" if not ok else "",
    )


def scenario_duplicate(broker: str, token: str) -> ScenarioResult:
    code, _ = http_request(
        "POST",
        f"{broker}/skills",
        token=token,
        body={
            "action": "create",
            "name": _EVAL_SKILL_NAME,
            "title": "Duplicate",
            "content": "duplicate",
            "created_by": "eval-harness",
        },
    )
    ok = code == 409
    return ScenarioResult(
        id="skill-03-duplicate",
        title="Duplicate name returns 409",
        ok=ok,
        detail=f"want 409, got {code}" if not ok else "",
    )


def scenario_invoke(broker: str, token: str) -> ScenarioResult:
    code, body = http_request(
        "POST",
        f"{broker}/skills/{urllib.parse.quote(_EVAL_SKILL_NAME)}/invoke",
        token=token,
        body={"invoked_by": "eval-harness", "channel": "general"},
    )
    if code != 200:
        return ScenarioResult(
            id="skill-04-invoke",
            title=f"POST /skills/{_EVAL_SKILL_NAME}/invoke",
            ok=False,
            detail=f"status={code}",
        )
    usage = 0
    if isinstance(body, dict):
        skill_obj = body.get("skill") or {}
        raw = skill_obj.get("usage_count")
        try:
            usage = int(raw)
        except (TypeError, ValueError):
            usage = 0
    ok = usage >= 1
    return ScenarioResult(
        id="skill-04-invoke",
        title=f"POST /skills/{_EVAL_SKILL_NAME}/invoke",
        ok=ok,
        detail=f"usage_count={usage}" if not ok else f"usage_count={usage}",
    )


def scenario_archive(broker: str, token: str) -> ScenarioResult:
    code, _ = http_request(
        "DELETE",
        f"{broker}/skills",
        token=token,
        body={"name": _EVAL_SKILL_NAME},
    )
    if code != 200:
        return ScenarioResult(
            id="skill-05-archive",
            title="DELETE /skills (archive)",
            ok=False,
            detail=f"status={code}",
        )
    # Confirm it is gone from GET.
    list_code, list_body = http_request("GET", f"{broker}/skills", token=token)
    if list_code != 200 or not isinstance(list_body, dict):
        return ScenarioResult(
            id="skill-05-archive",
            title="DELETE /skills (archive)",
            ok=False,
            detail=f"verification GET failed (status={list_code})",
        )
    skills = list_body.get("skills", [])
    probe = next((s for s in skills if s.get("name") == _EVAL_SKILL_NAME), None)
    ok = probe is None
    return ScenarioResult(
        id="skill-05-archive",
        title="DELETE /skills (archive)",
        ok=ok,
        detail="archived skill still visible in GET /skills" if not ok else "",
    )


# ---------- Reporting ----------


def print_report(report: SkillEvalReport) -> None:
    print()
    print("=" * 78)
    print("SKILL EVAL — PER-SCENARIO BREAKDOWN")
    print("=" * 78)
    for sc in report.scenarios:
        sign = "PASS" if sc.ok else "FAIL"
        print(f"  [{sign}] {sc.id:<32}  {sc.title}")
        if sc.detail:
            print(f"         {sc.detail}")


# ---------- Main ----------


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--broker", default="http://localhost:7899")
    ap.add_argument("--token-file", default="/tmp/wuphf-broker-token-7899")
    ap.add_argument(
        "--home",
        action="store_true",
        help=(
            "Isolate HOME to ~/.wuphf-dev-home before running.  Safe to pass "
            "even when HOME already points there — the guard prevents doubling."
        ),
    )
    args = ap.parse_args()

    parsed_url = urllib.parse.urlparse(args.broker)
    if parsed_url.scheme not in ("http", "https"):
        print(
            f"error: --broker must use http or https scheme, got {args.broker!r}",
            file=sys.stderr,
        )
        return 2

    if args.home:
        isolated = apply_home_isolation()
        print(f"[skill-eval] HOME isolation active: {isolated}")

    token_path = Path(args.token_file)
    if not token_path.exists():
        print(f"token file not found: {token_path}", file=sys.stderr)
        return 2
    token = token_path.read_text().strip()

    # Reset any state left by a previous run before executing scenarios so
    # each invocation starts from a deterministic baseline.
    print(f"[skill-eval] resetting state before run…")
    reset_skill_state(args.broker, token)

    report = SkillEvalReport()
    report.scenarios.append(scenario_list_empty(args.broker, token))
    report.scenarios.append(scenario_create(args.broker, token))
    report.scenarios.append(scenario_duplicate(args.broker, token))
    report.scenarios.append(scenario_invoke(args.broker, token))
    report.scenarios.append(scenario_archive(args.broker, token))

    print_report(report)

    passed = sum(1 for sc in report.scenarios if sc.ok)
    total = len(report.scenarios)
    print()
    print("=" * 78)
    print("ACCEPTANCE GATES")
    print("=" * 78)
    print(f"  skill_lifecycle: {passed}/{total} ({'PASS' if passed == total else 'FAIL'})")

    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())

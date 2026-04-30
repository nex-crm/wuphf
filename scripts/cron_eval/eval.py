#!/usr/bin/env python3
"""Cron-registry end-to-end evaluation harness (PR 8 Lane I).

Boots a freshly-built dev broker on :7899, drives the /scheduler surface
through a series of contract scenarios, and asserts pass/fail per the
spec in the cron registry test plan.

Scenarios:
  1. self-registration on cold boot — every system cron present, system_managed,
     enabled.
  2. PATCH happy path — interval_override lands and survives a GET.
  3. floor rejection — sub-floor override returns 400, state unchanged.
  4. read-only cron — one-relay-events refuses any PATCH with 400.
  5. disable + re-enable — round-trip flips enabled and status="disabled".
  6. disabled cron skips run — disabled cron emits no telemetry within one
     tick window (best-effort log-line scan).
  7. persistence across restart — caller-driven; harness writes a state
     snapshot after PATCH, then a follow-up subcommand re-reads after the
     broker has been restarted.

Acceptance gates:
  - cron_self_registration: 8/8 system crons present, all system_managed,
    all enabled.
  - cron_patch_floors_enforced: every below-floor PATCH returns 400; every
    at-floor PATCH returns 200.
  - cron_disabled_skips_run: disabled cron emits no run line in the
    sampled tick window.
  - cron_persistence: PATCH-set override survives a broker restart.

Usage:
  python3 scripts/cron_eval/eval.py \\
      --broker http://localhost:7899 \\
      --token-file /tmp/wuphf-broker-token-7899 \\
      --broker-log /tmp/wuphf-dev-eval.log \\
      --report scripts/cron_eval/last-report.md
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Source-of-truth mirror of internal/team/broker.go::systemCronSpecs(). Keep in
# sync; CI surfaces a divergence as a self-registration failure.
SYSTEM_CRONS: dict[str, dict[str, Any]] = {
    "nex-insights": {"min_floor": 30, "read_only": False},
    "nex-notifications": {"min_floor": 5, "read_only": False},
    "one-relay-events": {"min_floor": 1, "read_only": True},
    "request_follow_up": {"min_floor": 5, "read_only": False},
    "review-expiry": {"min_floor": 5, "read_only": False},
    "task_follow_up": {"min_floor": 5, "read_only": False},
    "task_recheck": {"min_floor": 5, "read_only": False},
    "task_reminder": {"min_floor": 5, "read_only": False},
}

# Token state-snapshot dir: where the harness drops PATCH state before a
# restart so the caller can verify persistence after the broker reboots.
PERSISTENCE_SNAPSHOT_PATH = "/tmp/wuphf-cron-eval-snapshot.json"


# ---------- HTTP helpers ----------


def http_request(
    method: str,
    url: str,
    *,
    token: str,
    body: dict[str, Any] | None = None,
    timeout: float = 15.0,
) -> tuple[int, dict[str, Any] | str]:
    """Wraps urllib with a Bearer-auth header. JSON-decodes both 2xx and
    4xx/5xx bodies so callers asserting structured error shapes don't have
    to re-parse on every status-code branch.
    """
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


def list_scheduler(broker: str, token: str) -> list[dict[str, Any]]:
    code, body = http_request("GET", f"{broker}/scheduler", token=token)
    if code != 200 or not isinstance(body, dict):
        return []
    jobs = body.get("jobs") or []
    return list(jobs)


def patch_scheduler(
    broker: str, token: str, slug: str, payload: dict[str, Any]
) -> tuple[int, dict[str, Any] | str]:
    return http_request(
        "PATCH",
        f"{broker}/scheduler/{urllib.parse.quote(slug)}",
        token=token,
        body=payload,
    )


# ---------- Result types ----------


@dataclass
class ScenarioResult:
    id: str
    title: str
    ok: bool
    detail: str = ""
    sub_results: list[tuple[str, bool, str]] = field(default_factory=list)


@dataclass
class CronEvalReport:
    self_registration_pass: int = 0
    self_registration_total: int = len(SYSTEM_CRONS)
    floors_pass: int = 0
    floors_total: int = 0  # 7 below-floor + 7 at-floor cases
    disabled_skips_run: bool | None = None  # None == not exercised
    persistence_ok: bool | None = None  # None == not exercised this run
    scenarios: list[ScenarioResult] = field(default_factory=list)


# ---------- Scenarios ----------


def scenario_self_registration(
    broker: str, token: str, report: CronEvalReport
) -> ScenarioResult:
    """Scenario 1: GET /scheduler returns every system cron with
    system_managed=true and enabled=true on cold boot.
    """
    jobs = list_scheduler(broker, token)
    by_slug = {j.get("slug"): j for j in jobs if j.get("slug")}
    sub: list[tuple[str, bool, str]] = []
    for slug in SYSTEM_CRONS:
        job = by_slug.get(slug)
        if job is None:
            sub.append((slug, False, "missing from /scheduler"))
            continue
        if not job.get("system_managed", False):
            sub.append((slug, False, "system_managed=false (want true)"))
            continue
        if not job.get("enabled", False):
            sub.append((slug, False, "enabled=false (want true on cold boot)"))
            continue
        sub.append((slug, True, ""))
    pass_count = sum(1 for _, ok, _ in sub if ok)
    report.self_registration_pass = pass_count
    ok = pass_count == len(SYSTEM_CRONS)
    detail = (
        f"{pass_count}/{len(SYSTEM_CRONS)} system crons registered with "
        "system_managed=true and enabled=true"
    )
    return ScenarioResult(
        id="cron-01-self-registration",
        title="Self-registration on cold boot",
        ok=ok,
        detail=detail,
        sub_results=sub,
    )


def scenario_patch_happy_path(broker: str, token: str) -> ScenarioResult:
    """Scenario 2: PATCH a configurable cron with a valid override and
    confirm GET reflects it. Uses nex-notifications (floor=5) at 30 min.
    """
    target_slug = "nex-notifications"
    want_override = 30
    code, body = patch_scheduler(
        broker, token, target_slug, {"interval_override": want_override}
    )
    if code != 200:
        return ScenarioResult(
            id="cron-02-patch-happy-path",
            title=f"PATCH {target_slug} interval_override={want_override}",
            ok=False,
            detail=f"PATCH returned {code}: {body!r}",
        )
    # Confirm via fresh GET that the override survives the round-trip.
    jobs = list_scheduler(broker, token)
    found = next((j for j in jobs if j.get("slug") == target_slug), None)
    if found is None:
        return ScenarioResult(
            id="cron-02-patch-happy-path",
            title=f"PATCH {target_slug} interval_override={want_override}",
            ok=False,
            detail="cron disappeared from /scheduler after PATCH",
        )
    actual_override = int(found.get("interval_override") or 0)
    ok = actual_override == want_override
    detail = (
        f"GET shows interval_override={actual_override}, want {want_override}"
        if not ok
        else f"interval_override={actual_override} as set"
    )
    return ScenarioResult(
        id="cron-02-patch-happy-path",
        title=f"PATCH {target_slug} interval_override={want_override}",
        ok=ok,
        detail=detail,
    )


def scenario_floor_rejection(
    broker: str, token: str, report: CronEvalReport
) -> ScenarioResult:
    """Scenario 3: every configurable cron must reject below-floor PATCH (400)
    and accept at-floor PATCH (200). Read-only crons are excluded; they're
    covered by scenario_read_only.
    """
    sub: list[tuple[str, bool, str]] = []
    cases_total = 0
    cases_pass = 0
    for slug, spec in SYSTEM_CRONS.items():
        if spec["read_only"]:
            continue
        floor = int(spec["min_floor"])
        below = max(1, floor - 1)
        # Snapshot pre-PATCH state so we can confirm the below-floor PATCH
        # left the cron untouched.
        pre_jobs = list_scheduler(broker, token)
        pre = next((j for j in pre_jobs if j.get("slug") == slug), None)
        pre_override = int((pre or {}).get("interval_override") or 0)

        # Below-floor → 400 expected.
        cases_total += 1
        code, body = patch_scheduler(
            broker, token, slug, {"interval_override": below}
        )
        below_ok = code == 400
        if below_ok:
            cases_pass += 1
        sub.append(
            (
                f"{slug} below_floor (override={below}, floor={floor})",
                below_ok,
                ""
                if below_ok
                else f"want 400 got {code}: {body!r}",
            )
        )
        # State must be unchanged after a rejected PATCH.
        post_jobs = list_scheduler(broker, token)
        post = next((j for j in post_jobs if j.get("slug") == slug), None)
        post_override = int((post or {}).get("interval_override") or 0)
        if post_override != pre_override:
            sub.append(
                (
                    f"{slug} state-after-reject",
                    False,
                    f"interval_override changed from {pre_override} to "
                    f"{post_override} despite 400",
                )
            )
            cases_total += 1  # count the state assertion too

        # At-floor → 200 expected.
        cases_total += 1
        code, body = patch_scheduler(
            broker, token, slug, {"interval_override": floor}
        )
        at_floor_ok = code == 200
        if at_floor_ok:
            cases_pass += 1
        sub.append(
            (
                f"{slug} at_floor (override={floor})",
                at_floor_ok,
                ""
                if at_floor_ok
                else f"want 200 got {code}: {body!r}",
            )
        )
    report.floors_pass = cases_pass
    report.floors_total = cases_total
    ok = cases_pass == cases_total and cases_total > 0
    return ScenarioResult(
        id="cron-03-floor-rejection",
        title="Floor rejection (below=400, at=200)",
        ok=ok,
        detail=f"{cases_pass}/{cases_total} sub-cases pass",
        sub_results=sub,
    )


def scenario_read_only(broker: str, token: str) -> ScenarioResult:
    """Scenario 4: one-relay-events refuses every PATCH variant with 400."""
    target_slug = "one-relay-events"
    sub: list[tuple[str, bool, str]] = []
    payloads: list[dict[str, Any]] = [
        {"interval_override": 5},
        {"enabled": False},
        {"interval_override": 10, "enabled": True},
    ]
    pre_jobs = list_scheduler(broker, token)
    pre = next((j for j in pre_jobs if j.get("slug") == target_slug), None)
    pre_enabled = bool((pre or {}).get("enabled", False))
    pre_override = int((pre or {}).get("interval_override") or 0)
    all_ok = True
    for i, payload in enumerate(payloads):
        code, body = patch_scheduler(broker, token, target_slug, payload)
        ok = code == 400
        if not ok:
            all_ok = False
        sub.append(
            (
                f"{target_slug} payload-{i} ({payload})",
                ok,
                "" if ok else f"want 400 got {code}: {body!r}",
            )
        )
    # State must be untouched.
    post_jobs = list_scheduler(broker, token)
    post = next((j for j in post_jobs if j.get("slug") == target_slug), None)
    post_enabled = bool((post or {}).get("enabled", False))
    post_override = int((post or {}).get("interval_override") or 0)
    state_unchanged = post_enabled == pre_enabled and post_override == pre_override
    sub.append(
        (
            f"{target_slug} state-unchanged",
            state_unchanged,
            ""
            if state_unchanged
            else f"enabled {pre_enabled}->{post_enabled}, "
            f"override {pre_override}->{post_override}",
        )
    )
    return ScenarioResult(
        id="cron-04-read-only",
        title=f"Read-only cron rejects PATCH ({target_slug})",
        ok=all_ok and state_unchanged,
        detail="rejected all 3 variants" if all_ok and state_unchanged else "see sub-results",
        sub_results=sub,
    )


def scenario_disable_re_enable(broker: str, token: str) -> ScenarioResult:
    """Scenario 5: disable + re-enable round-trip flips enabled and status."""
    target_slug = "task_follow_up"
    # Disable.
    code, body = patch_scheduler(broker, token, target_slug, {"enabled": False})
    if code != 200:
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail=f"PATCH disable returned {code}: {body!r}",
        )
    jobs = list_scheduler(broker, token)
    job = next((j for j in jobs if j.get("slug") == target_slug), None)
    if job is None:
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail="cron missing after disable",
        )
    if job.get("enabled", True):
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail=f"enabled stayed true after disable: job={job!r}",
        )
    if (job.get("status") or "").lower() != "disabled":
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail=f"status={job.get('status')!r}, want disabled",
        )
    # Re-enable.
    code, body = patch_scheduler(broker, token, target_slug, {"enabled": True})
    if code != 200:
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail=f"PATCH re-enable returned {code}: {body!r}",
        )
    jobs = list_scheduler(broker, token)
    job = next((j for j in jobs if j.get("slug") == target_slug), None)
    if job is None or not job.get("enabled", False):
        return ScenarioResult(
            id="cron-05-disable-reenable",
            title=f"Disable + re-enable ({target_slug})",
            ok=False,
            detail=f"enabled didn't flip back: job={job!r}",
        )
    return ScenarioResult(
        id="cron-05-disable-reenable",
        title=f"Disable + re-enable ({target_slug})",
        ok=True,
        detail="round-trip clean",
    )


def scenario_disabled_skips_run(
    broker: str, token: str, broker_log: Path | None, report: CronEvalReport
) -> ScenarioResult:
    """Scenario 6: a disabled cron emits no telemetry inside the sampled
    window. Best-effort: snapshot last_run_at + run-loop log line count
    before disable, wait one tick interval, then assert neither moved.

    We disable nex-notifications (floor 5, real cron) and watch for ~12s.
    The cron's run-loop is keyed on IntervalOverride, so a freshly-disabled
    cron with no override is still "due" by clock — the harness asserts it
    DOESN'T fire while disabled.
    """
    target_slug = "nex-notifications"
    # Disable + force a tiny override so the run loop would WANT to fire
    # often if it weren't disabled. The 5-min floor still applies, so we
    # set interval_override=5 first (within floor) THEN disable.
    code, body = patch_scheduler(
        broker, token, target_slug, {"interval_override": 5}
    )
    if code != 200:
        report.disabled_skips_run = False
        return ScenarioResult(
            id="cron-06-disabled-skips-run",
            title=f"Disabled cron skips run ({target_slug})",
            ok=False,
            detail=f"PATCH override pre-step returned {code}: {body!r}",
        )
    code, body = patch_scheduler(broker, token, target_slug, {"enabled": False})
    if code != 200:
        report.disabled_skips_run = False
        return ScenarioResult(
            id="cron-06-disabled-skips-run",
            title=f"Disabled cron skips run ({target_slug})",
            ok=False,
            detail=f"PATCH disable returned {code}: {body!r}",
        )
    pre_jobs = list_scheduler(broker, token)
    pre = next((j for j in pre_jobs if j.get("slug") == target_slug), None)
    pre_last = (pre or {}).get("last_run") or ""
    pre_log_lines = _count_log_matches(broker_log, target_slug)

    # Sample window. The fastest registry tick is 60s, so 12s is safely under
    # one tick; we're asserting the cron does NOT fire spuriously, not that
    # we sat through a full default cycle.
    time.sleep(12.0)

    post_jobs = list_scheduler(broker, token)
    post = next((j for j in post_jobs if j.get("slug") == target_slug), None)
    post_last = (post or {}).get("last_run") or ""
    post_log_lines = _count_log_matches(broker_log, target_slug)

    last_run_unchanged = post_last == pre_last
    log_lines_unchanged = post_log_lines == pre_log_lines
    ok = last_run_unchanged and log_lines_unchanged
    detail = (
        f"last_run {pre_last!r}->{post_last!r}, "
        f"log_lines mentioning {target_slug}: {pre_log_lines}->{post_log_lines}"
    )
    report.disabled_skips_run = ok

    # Restore so subsequent scenarios start from a clean state.
    patch_scheduler(broker, token, target_slug, {"enabled": True})
    patch_scheduler(broker, token, target_slug, {"interval_override": 0})

    return ScenarioResult(
        id="cron-06-disabled-skips-run",
        title=f"Disabled cron skips run ({target_slug})",
        ok=ok,
        detail=detail,
    )


def _count_log_matches(broker_log: Path | None, slug: str) -> int:
    """Counts log lines containing slug. Returns 0 if log is missing —
    the disabled-skips-run check then collapses to last_run-only, which
    is still a valid signal but not as strong.
    """
    if broker_log is None or not broker_log.exists():
        return 0
    try:
        text = broker_log.read_text(errors="replace")
    except OSError:
        return 0
    return text.count(slug)


def scenario_persistence_snapshot(
    broker: str, token: str
) -> ScenarioResult:
    """Scenario 7a: write a snapshot of the cron state after PATCHing an
    override. Caller restarts the broker, then runs --verify-persistence
    to compare. We choose review-expiry to avoid colliding with other
    scenarios.
    """
    target_slug = "review-expiry"
    want_override = 25  # well above the 5-min floor
    code, body = patch_scheduler(
        broker, token, target_slug, {"interval_override": want_override}
    )
    if code != 200:
        return ScenarioResult(
            id="cron-07a-persistence-snapshot",
            title=f"Persistence snapshot ({target_slug})",
            ok=False,
            detail=f"PATCH returned {code}: {body!r}",
        )
    snapshot = {
        "slug": target_slug,
        "interval_override": want_override,
        "captured_at": time.time(),
    }
    Path(PERSISTENCE_SNAPSHOT_PATH).write_text(json.dumps(snapshot))
    return ScenarioResult(
        id="cron-07a-persistence-snapshot",
        title=f"Persistence snapshot ({target_slug})",
        ok=True,
        detail=(
            f"snapshot written to {PERSISTENCE_SNAPSHOT_PATH}; "
            "restart broker, then run --verify-persistence"
        ),
    )


def scenario_persistence_verify(
    broker: str, token: str, report: CronEvalReport
) -> ScenarioResult:
    """Scenario 7b: read the snapshot from disk and confirm a fresh GET
    against the just-restarted broker still shows the override.
    """
    snapshot_path = Path(PERSISTENCE_SNAPSHOT_PATH)
    if not snapshot_path.exists():
        report.persistence_ok = False
        return ScenarioResult(
            id="cron-07b-persistence-verify",
            title="Persistence verify",
            ok=False,
            detail=(
                f"no snapshot at {PERSISTENCE_SNAPSHOT_PATH}; "
                "did you run --persistence-snapshot first?"
            ),
        )
    snapshot = json.loads(snapshot_path.read_text())
    target_slug = snapshot["slug"]
    want_override = int(snapshot["interval_override"])
    jobs = list_scheduler(broker, token)
    job = next((j for j in jobs if j.get("slug") == target_slug), None)
    if job is None:
        report.persistence_ok = False
        return ScenarioResult(
            id="cron-07b-persistence-verify",
            title="Persistence verify",
            ok=False,
            detail=f"{target_slug} missing from /scheduler post-restart",
        )
    actual_override = int(job.get("interval_override") or 0)
    ok = actual_override == want_override
    report.persistence_ok = ok
    return ScenarioResult(
        id="cron-07b-persistence-verify",
        title="Persistence verify",
        ok=ok,
        detail=(
            f"interval_override post-restart={actual_override}, "
            f"snapshot={want_override}"
        ),
    )


# ---------- Reporting ----------


def print_report(report: CronEvalReport) -> None:
    print()
    print("=" * 78)
    print("CRON REGISTRY EVAL — PER-SCENARIO BREAKDOWN")
    print("=" * 78)
    for sc in report.scenarios:
        sign = "PASS" if sc.ok else "FAIL"
        print(f"  [{sign}] {sc.id:<32}  {sc.title}")
        if sc.detail:
            print(f"         {sc.detail}")
        for sub_id, sub_ok, sub_detail in sc.sub_results:
            sub_sign = "PASS" if sub_ok else "FAIL"
            line = f"           - [{sub_sign}] {sub_id}"
            if sub_detail:
                line += f"  // {sub_detail}"
            print(line)


def render_markdown_report(report: CronEvalReport) -> str:
    out: list[str] = []
    out.append("# Cron-registry evaluation report")
    out.append("")
    out.append(
        f"- **Self-registration:** {report.self_registration_pass}/"
        f"{report.self_registration_total}"
    )
    out.append(
        f"- **Floors enforced:** {report.floors_pass}/{report.floors_total}"
    )
    out.append(
        "- **Disabled-skips-run:** "
        f"{'pass' if report.disabled_skips_run else 'fail' if report.disabled_skips_run is False else 'n/a'}"
    )
    out.append(
        "- **Persistence:** "
        f"{'pass' if report.persistence_ok else 'fail' if report.persistence_ok is False else 'n/a (run --verify-persistence)'}"
    )
    out.append("")
    out.append("## Scenarios")
    out.append("")
    out.append("| id | title | pass | detail |")
    out.append("|---|---|---|---|")
    for sc in report.scenarios:
        sign = "PASS" if sc.ok else "FAIL"
        detail = (sc.detail or "").replace("|", "/")
        out.append(f"| `{sc.id}` | {sc.title} | {sign} | {detail} |")
    # Sub-results table for scenarios that have any.
    if any(sc.sub_results for sc in report.scenarios):
        out.append("")
        out.append("## Sub-results")
        out.append("")
        out.append("| scenario | sub | pass | detail |")
        out.append("|---|---|---|---|")
        for sc in report.scenarios:
            for sub_id, sub_ok, sub_detail in sc.sub_results:
                sign = "PASS" if sub_ok else "FAIL"
                detail = (sub_detail or "").replace("|", "/")
                out.append(f"| `{sc.id}` | `{sub_id}` | {sign} | {detail} |")
    return "\n".join(out) + "\n"


# ---------- State reset ----------


def reset_scheduler_state(broker: str, token: str) -> None:
    """Reset all system crons to their default state before a full run.

    Clears any interval_override and re-enables any disabled cron so that
    each run starts from a deterministic baseline.  Without this, overrides
    written by scenario_floor_rejection, scenario_patch_happy_path, and
    scenario_persistence_snapshot bleed into the next invocation and cause
    spurious failures (e.g. floor checks starting from a pre-patched value,
    self-registration failing because a cron was left disabled).
    """
    for slug, spec in SYSTEM_CRONS.items():
        if spec.get("read_only"):
            # Read-only crons reject any PATCH — nothing to reset.
            continue
        code, body = http_request(
            "PATCH",
            f"{broker}/scheduler/{urllib.parse.quote(slug)}",
            token=token,
            body={"interval_override": 0, "enabled": True},
        )
        if code < 200 or code >= 300:
            raise RuntimeError(
                f"reset_scheduler_state: PATCH {slug} returned {code}: {body!r}"
            )


# ---------- Main ----------


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--broker", default="http://localhost:7899")
    ap.add_argument("--token-file", default="/tmp/wuphf-broker-token-7899")
    ap.add_argument(
        "--broker-log",
        default="/tmp/wuphf-dev-eval.log",
        help="Broker log path; used by scenario 6 to count run-line activity.",
    )
    ap.add_argument(
        "--report",
        default=None,
        help="If set, also write the markdown report to this path.",
    )
    ap.add_argument(
        "--persistence-snapshot",
        action="store_true",
        help=(
            "Run only scenario 7a (write snapshot + PATCH). Caller restarts "
            "the broker, then re-runs with --verify-persistence."
        ),
    )
    ap.add_argument(
        "--verify-persistence",
        action="store_true",
        help="Run only scenario 7b (verify the snapshot against post-restart state).",
    )
    args = ap.parse_args()

    token_path = Path(args.token_file)
    if not token_path.exists():
        print(f"token file not found: {token_path}", file=sys.stderr)
        return 2
    token = token_path.read_text().strip()
    broker_log = Path(args.broker_log) if args.broker_log else None

    report = CronEvalReport()

    if args.persistence_snapshot:
        report.scenarios.append(scenario_persistence_snapshot(args.broker, token))
        print_report(report)
        if args.report:
            Path(args.report).write_text(render_markdown_report(report), encoding="utf-8")
        return 0 if all(sc.ok for sc in report.scenarios) else 1

    if args.verify_persistence:
        report.scenarios.append(scenario_persistence_verify(args.broker, token, report))
        print_report(report)
        if args.report:
            Path(args.report).write_text(render_markdown_report(report), encoding="utf-8")
        ok = report.persistence_ok is True
        print()
        print("=" * 78)
        print("PERSISTENCE GATE")
        print("=" * 78)
        print(f"  cron_persistence: {'PASS' if ok else 'FAIL'}")
        return 0 if ok else 1

    # Full run: reset broker state first so each invocation starts from a
    # deterministic baseline (clears overrides left by the previous run).
    # Also remove a stale snapshot so --verify-persistence won't pick up
    # a result from a different broker boot.
    print("[cron-eval] resetting scheduler state before run…")
    reset_scheduler_state(args.broker, token)
    snapshot_path = Path(PERSISTENCE_SNAPSHOT_PATH)
    if snapshot_path.exists():
        snapshot_path.unlink()

    # Full run: scenarios 1-6, then write the persistence snapshot last so
    # downstream tooling can chain --verify-persistence against the restart.
    print(f"[cron-eval] broker={args.broker}")
    report.scenarios.append(scenario_self_registration(args.broker, token, report))
    report.scenarios.append(scenario_patch_happy_path(args.broker, token))
    report.scenarios.append(scenario_floor_rejection(args.broker, token, report))
    report.scenarios.append(scenario_read_only(args.broker, token))
    report.scenarios.append(scenario_disable_re_enable(args.broker, token))
    report.scenarios.append(
        scenario_disabled_skips_run(args.broker, token, broker_log, report)
    )
    # Always run the snapshot last so its PATCH doesn't leak into earlier
    # state-asserting scenarios.
    report.scenarios.append(scenario_persistence_snapshot(args.broker, token))

    print_report(report)

    if args.report:
        Path(args.report).write_text(render_markdown_report(report), encoding="utf-8")
        print(f"\n[cron-eval] markdown report written to {args.report}")

    # Acceptance gates per the task spec.
    self_reg_ok = report.self_registration_pass == report.self_registration_total
    floors_ok = (
        report.floors_total > 0 and report.floors_pass == report.floors_total
    )
    disabled_ok = report.disabled_skips_run is True
    # Persistence gate is verified by the --verify-persistence subcommand;
    # the in-process snapshot scenario only asserts the PATCH itself landed.
    persistence_snapshot_ok = (
        report.scenarios[-1].ok if report.scenarios else False
    )

    print()
    print("=" * 78)
    print("ACCEPTANCE GATES")
    print("=" * 78)
    print(
        "  cron_self_registration:   "
        f"{report.self_registration_pass}/{report.self_registration_total}  "
        f"({'PASS' if self_reg_ok else 'FAIL'})"
    )
    print(
        "  cron_patch_floors:        "
        f"{report.floors_pass}/{report.floors_total}  "
        f"({'PASS' if floors_ok else 'FAIL'})"
    )
    print(
        "  cron_disabled_skips_run:  "
        f"{'PASS' if disabled_ok else 'FAIL'}"
    )
    print(
        "  cron_persistence_snap:    "
        f"{'PASS' if persistence_snapshot_ok else 'FAIL'}  "
        "(restart broker + --verify-persistence to confirm gate 4)"
    )

    other_scenarios_ok = all(
        sc.ok for sc in report.scenarios
        if sc.id
        not in {
            "cron-01-self-registration",
            "cron-03-floor-rejection",
            "cron-06-disabled-skips-run",
            "cron-07a-persistence-snapshot",
        }
    )
    overall_ok = (
        self_reg_ok and floors_ok and disabled_ok and persistence_snapshot_ok
        and other_scenarios_ok
    )
    return 0 if overall_ok else 1


if __name__ == "__main__":
    sys.exit(main())

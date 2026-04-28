#!/usr/bin/env python3
"""Skill-pipeline evaluation harness.

Drops a labelled corpus of synthetic wiki articles into the dev wiki, runs
/skills/compile, and scores the outcome on two axes:

  1. Correctness: did each scenario land in the expected bucket
     (promoted / skipped / guard_rejected)?
  2. Quality: for every scenario the pipeline promoted, score the resulting
     SKILL.md against an Anthropic-frontmatter rubric and a body-shape
     heuristic.

Usage:
  HOME=$HOME/.wuphf-dev-home python3 scripts/skill_eval/eval.py \\
      --broker http://localhost:7899 \\
      --token-file /tmp/wuphf-broker-token-7899
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

EVAL_PREFIX = "eval-"
WIKI_PLAYBOOKS_REL = "wiki/team/playbooks"
WIKI_SKILLS_REL = "wiki/team/skills"
SKILL_SHAPE_HEADERS = (
    "## steps",
    "## how to",
    "## procedure",
    "## workflow",
    "## instructions",
    "## process",
    "## recipe",
    "## runbook",
    "## when this fires",
)


@dataclass
class Scenario:
    id: str
    category: str
    expected: str
    rationale: str
    filename: str
    content: str

    @property
    def slug(self) -> str:
        # The scanner derives the skill slug from frontmatter `name`; we mirror
        # that when present so we can find the resulting SKILL.md.
        m = re.search(r"^name:\s*(.+)$", self.content, re.M)
        return m.group(1).strip() if m else ""


@dataclass
class QualityScore:
    checks: dict[str, bool] = field(default_factory=dict)
    notes: list[str] = field(default_factory=list)

    @property
    def score(self) -> int:
        return sum(1 for v in self.checks.values() if v)

    @property
    def total(self) -> int:
        return len(self.checks)

    @property
    def pct(self) -> float:
        return 100.0 * self.score / self.total if self.total else 0.0


@dataclass
class ScenarioResult:
    scenario: Scenario
    actual: str  # promoted | skipped | guard_rejected | error
    correct: bool
    quality: QualityScore | None = None
    on_disk_path: Path | None = None
    detail: str = ""


# ---------- HTTP helpers ----------


def http_request(
    method: str,
    url: str,
    *,
    token: str,
    body: dict[str, Any] | None = None,
    timeout: float = 30.0,
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
        return e.code, payload


# ---------- Quality rubric ----------

ANTHROPIC_DESC_MIN = 10
ANTHROPIC_DESC_MAX = 200
ANTHROPIC_NAME_RE = re.compile(r"^[a-z0-9][a-z0-9-]*$")
PLACEHOLDER_TOKENS = ("lorem ipsum", "TODO:", "TBD:", "FIXME:", "<your ", "...placeholder")


def split_frontmatter(text: str) -> tuple[dict[str, Any] | None, str]:
    if not text.startswith("---\n"):
        return None, text
    end = text.find("\n---\n", 4)
    if end == -1:
        return None, text
    raw_fm = text[4:end]
    body = text[end + 5 :]
    fm = parse_yaml_lite(raw_fm)
    return fm, body


def parse_yaml_lite(raw: str) -> dict[str, Any]:
    """Tiny YAML reader: top-level key/value + nested key/value via indent.
    Good enough for our SKILL.md frontmatter, which is shallow + line-oriented.
    Avoids importing PyYAML so the harness has no extra deps.
    """
    out: dict[str, Any] = {}
    stack: list[tuple[int, dict[str, Any]]] = [(0, out)]
    last_list_key: str | None = None
    last_list_indent = -1

    for raw_line in raw.splitlines():
        line = raw_line.rstrip()
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip())
        stripped = line.lstrip()

        # List item under the most recent list key
        if stripped.startswith("- "):
            if last_list_key is not None and indent > last_list_indent:
                # find target dict for that list_key
                target_dict = stack[-1][1]
                lst = target_dict.setdefault(last_list_key, [])
                if isinstance(lst, list):
                    lst.append(stripped[2:].strip())
                continue

        # Pop stack entries deeper than current indent
        while stack and indent < stack[-1][0]:
            stack.pop()
        if not stack:
            stack = [(0, out)]

        if ":" not in stripped:
            continue
        k, _, v = stripped.partition(":")
        key = k.strip()
        val = v.strip()
        target = stack[-1][1]
        if not val:
            new_dict: dict[str, Any] = {}
            target[key] = new_dict
            stack.append((indent + 2, new_dict))
            last_list_key = key
            last_list_indent = indent
        else:
            # strip wrapping quotes
            if (val.startswith('"') and val.endswith('"')) or (
                val.startswith("'") and val.endswith("'")
            ):
                val = val[1:-1]
            target[key] = val
            last_list_key = None
            last_list_indent = -1
    return out


def _grounding_keywords(text: str) -> set[str]:
    return {w.lower() for w in re.findall(r"[A-Za-z][A-Za-z0-9-]{4,}", text)}


def score_skill_file(path: Path, source_text: str) -> QualityScore:
    """Score the on-disk SKILL.md against the rubric.

    `source_text` is the original wiki article body (pre-promotion). We use
    it to grade hallucination + grounding, since description claims should
    be supported by the source.
    """
    q = QualityScore()
    raw = path.read_text(encoding="utf-8")
    fm, body = split_frontmatter(raw)

    # Frontmatter parses
    q.checks["frontmatter_parses"] = fm is not None
    if fm is None:
        q.notes.append("frontmatter did not parse")
        return q

    name = (fm.get("name") or "").strip()
    desc = (fm.get("description") or "").strip()
    meta = fm.get("metadata") or {}
    wuphf = meta.get("wuphf") if isinstance(meta, dict) else {}
    if not isinstance(wuphf, dict):
        wuphf = {}

    # Required Anthropic fields
    q.checks["has_name"] = bool(name)
    q.checks["has_description"] = bool(desc)
    q.checks["name_kebab"] = bool(name and ANTHROPIC_NAME_RE.match(name))
    q.checks["desc_len_in_range"] = ANTHROPIC_DESC_MIN <= len(desc) <= ANTHROPIC_DESC_MAX

    # Provenance + safety metadata
    q.checks["has_metadata_wuphf"] = bool(wuphf)
    q.checks["has_trigger"] = bool(wuphf.get("trigger"))
    q.checks["has_created_by"] = bool(wuphf.get("created_by"))
    q.checks["has_safety_scan"] = bool(wuphf.get("safety_scan"))
    q.checks["has_source_articles"] = bool(wuphf.get("source_articles"))

    # Body shape
    body_lower = body.lower()
    q.checks["body_has_skill_header"] = any(h in body_lower for h in SKILL_SHAPE_HEADERS)
    q.checks["body_has_list_or_steps"] = bool(
        re.search(r"^\s*\d+\.\s", body, re.M) or re.search(r"^\s*[-*]\s", body, re.M)
    )
    q.checks["body_min_length"] = len(body.strip()) >= 100

    # Hallucination + placeholder
    q.checks["no_placeholder_text"] = not any(t.lower() in raw.lower() for t in PLACEHOLDER_TOKENS)

    # Grounding: at least one significant keyword from the description appears
    # in the body or the source article. Catches "skill says X, body says Y".
    desc_keys = _grounding_keywords(desc)
    body_keys = _grounding_keywords(body)
    src_keys = _grounding_keywords(source_text)
    if desc_keys:
        overlap = desc_keys & (body_keys | src_keys)
        q.checks["description_grounded_in_body"] = (len(overlap) / len(desc_keys)) >= 0.3
    else:
        q.checks["description_grounded_in_body"] = False

    return q


# ---------- Runner ----------


def load_scenarios(path: Path) -> list[Scenario]:
    raw = json.loads(path.read_text())
    return [Scenario(**s) for s in raw["scenarios"]]


def seed_corpus(playbooks_dir: Path, scenarios: list[Scenario], run_id: str) -> None:
    """Write each scenario into the wiki playbooks/ dir.

    The scanner keeps a per-process SHA cache and skips re-classification
    when content hashes match a prior pass. We append a hidden HTML comment
    carrying a unique run id so each invocation hashes to a fresh value and
    the eval is repeatable without restarting the broker.
    """
    playbooks_dir.mkdir(parents=True, exist_ok=True)
    nonce = f"\n<!-- eval-run: {run_id} -->\n"
    for sc in scenarios:
        body = sc.content
        if not body.endswith("\n"):
            body += "\n"
        (playbooks_dir / sc.filename).write_text(body + nonce, encoding="utf-8")


def cleanup_corpus(playbooks_dir: Path, skills_dir: Path) -> None:
    """Remove every eval-* fixture from playbooks/ and any skills the scanner
    promoted from this corpus run.
    """
    for d in (playbooks_dir, skills_dir):
        if not d.exists():
            continue
        for p in d.glob(f"{EVAL_PREFIX}*.md"):
            p.unlink()
    # Promoted SKILL.md files use the slug, not the eval- prefix. We track
    # those by name in the runner before invoking cleanup.


def cleanup_promoted_slugs(skills_dir: Path, slugs: list[str]) -> None:
    for slug in slugs:
        if not slug:
            continue
        candidate = skills_dir / f"{slug}.md"
        if candidate.exists():
            candidate.unlink()
        sub = skills_dir / slug
        if sub.exists() and sub.is_dir():
            shutil.rmtree(sub)


def reject_skill(broker: str, token: str, name: str, reason: str) -> None:
    http_request(
        "POST",
        f"{broker}/skills/{urllib.parse.quote(name)}/reject",
        token=token,
        body={"reason": reason},
    )


def list_skills(broker: str, token: str) -> list[dict[str, Any]]:
    code, body = http_request("GET", f"{broker}/skills", token=token)
    if code != 200 or not isinstance(body, dict):
        return []
    return list(body.get("skills") or [])


def trigger_compile(broker: str, token: str) -> dict[str, Any]:
    # Compile may run a Stage B LLM synthesis pass on top of the fast-path
    # scan, so allow generous time for live LLM round-trips.
    code, body = http_request(
        "POST", f"{broker}/skills/compile", token=token, body={}, timeout=180.0
    )
    if isinstance(body, dict):
        return {**body, "_status": code}
    return {"_status": code, "_raw": body}


def evaluate(
    broker: str,
    token: str,
    home: Path,
    scenarios: list[Scenario],
    keep_artifacts: bool,
) -> list[ScenarioResult]:
    playbooks = home / WIKI_PLAYBOOKS_REL
    skills = home / WIKI_SKILLS_REL
    promoted_slugs: list[str] = []

    run_id = f"{int(time.time())}-{os.getpid()}"
    print(f"[eval] seeding {len(scenarios)} scenarios into {playbooks}  run_id={run_id}")
    cleanup_corpus(playbooks, skills)  # belt-and-braces
    # Tombstones survive prior runs and would mask promotion in repeats.
    # The eval is intended to be idempotent against the corpus, so we drop
    # any eval-* slugs from .rejected.md before each pass.
    expected_slugs = {sc.slug for sc in scenarios if sc.slug}
    rejected_md = skills / ".rejected.md"
    if rejected_md.exists() and expected_slugs:
        text = rejected_md.read_text(encoding="utf-8")
        kept_lines: list[str] = []
        skip_block = False
        block_indent = -1
        for line in text.splitlines(keepends=True):
            stripped = line.strip()
            if stripped.startswith("- slug:"):
                slug_val = stripped.split(":", 1)[1].strip()
                if slug_val in expected_slugs:
                    skip_block = True
                    block_indent = len(line) - len(line.lstrip())
                    continue
                skip_block = False
            elif skip_block:
                indent = len(line) - len(line.lstrip()) if line.strip() else block_indent + 1
                if line.strip() and indent <= block_indent:
                    skip_block = False
                else:
                    continue
            if not skip_block:
                kept_lines.append(line)
        rejected_md.write_text("".join(kept_lines), encoding="utf-8")
    seed_corpus(playbooks, scenarios, run_id)

    print("[eval] triggering /skills/compile")
    compile_result = trigger_compile(broker, token)
    print(f"[eval] compile response: {json.dumps(compile_result, indent=2)}")

    # Give the WikiWorker a moment to flush writes before we read SKILL.md.
    time.sleep(0.5)

    skills_after = list_skills(broker, token)
    by_name = {s["name"]: s for s in skills_after}

    # Build a slug→error map from the compile response so we can label
    # guard_rejected accurately.
    error_slugs: dict[str, str] = {}
    for err in compile_result.get("errors", []) or []:
        error_slugs[err.get("slug", "")] = err.get("reason", "")

    results: list[ScenarioResult] = []
    for sc in scenarios:
        slug = sc.slug
        actual = "skipped"
        detail = ""
        on_disk: Path | None = None

        if slug and slug in by_name:
            actual = "promoted"
            on_disk = skills / f"{slug}.md"
            promoted_slugs.append(slug)
        elif slug and any(slug in s for s in error_slugs.keys()):
            # match by exact slug or substring (compile errors use the file slug)
            actual = "guard_rejected"
            for k, reason in error_slugs.items():
                if slug in k:
                    detail = reason
                    break
        else:
            # check error_slugs by filename-derived slug (no frontmatter case)
            fname_slug = sc.filename.replace(EVAL_PREFIX, "").replace(".md", "")
            if fname_slug in error_slugs:
                actual = "guard_rejected"
                detail = error_slugs[fname_slug]

        correct = actual == sc.expected

        quality: QualityScore | None = None
        if actual == "promoted" and on_disk and on_disk.exists():
            quality = score_skill_file(on_disk, sc.content)

        results.append(
            ScenarioResult(
                scenario=sc,
                actual=actual,
                correct=correct,
                quality=quality,
                on_disk_path=on_disk,
                detail=detail,
            )
        )

    if not keep_artifacts:
        # Reject every promoted eval skill so the broker's in-memory state
        # also forgets about them.
        for slug in promoted_slugs:
            try:
                reject_skill(broker, token, slug, "skill_eval cleanup")
            except Exception as exc:  # pragma: no cover
                print(f"[eval] warn: reject {slug} failed: {exc}")
        cleanup_promoted_slugs(skills, promoted_slugs)
        cleanup_corpus(playbooks, skills)

    return results


# ---------- Reporting ----------


def confusion_matrix(results: list[ScenarioResult]) -> str:
    buckets = ("promoted", "skipped", "guard_rejected")
    counts = {(e, a): 0 for e in buckets for a in (*buckets, "error")}
    for r in results:
        key = (r.scenario.expected, r.actual)
        if key not in counts:
            counts[key] = 0
        counts[key] += 1

    rows = ["expected \\\\ actual    promoted  skipped  guard_rejected  error"]
    for e in buckets:
        line = f"  {e:<18}"
        for a in (*buckets, "error"):
            line += f"{counts.get((e, a), 0):>10}"
        rows.append(line)
    return "\n".join(rows)


def render_markdown_report(results: list[ScenarioResult]) -> str:
    total = len(results)
    correct = sum(1 for r in results if r.correct)
    promoted = [r for r in results if r.actual == "promoted" and r.quality]
    avg_q = (
        sum(r.quality.pct for r in promoted if r.quality) / len(promoted)
        if promoted
        else 0.0
    )
    out: list[str] = []
    out.append("# Skill-pipeline evaluation report")
    out.append("")
    out.append(f"- **Stage A correctness:** {100.0*correct/total:.1f}% ({correct}/{total})")
    if promoted:
        out.append(
            f"- **Promoted-skill quality:** {avg_q:.1f}% mean over {len(promoted)} files"
        )
    out.append("")
    out.append("## Confusion matrix")
    out.append("")
    out.append("```")
    out.append(confusion_matrix(results))
    out.append("```")
    out.append("")
    out.append("## Per-scenario results")
    out.append("")
    out.append("| id | category | expected | actual | pass | rationale |")
    out.append("|---|---|---|---|---|---|")
    for r in results:
        sign = "PASS" if r.correct else "FAIL"
        out.append(
            f"| `{r.scenario.id}` | {r.scenario.category} | "
            f"{r.scenario.expected} | {r.actual} | {sign} | "
            f"{r.scenario.rationale.replace('|', '/')} |"
        )
    if promoted:
        out.append("")
        out.append("## Promoted-skill quality")
        out.append("")
        check_names: list[str] = []
        for r in promoted:
            for k in (r.quality.checks if r.quality else {}).keys():
                if k not in check_names:
                    check_names.append(k)
        out.append("| check | pass rate |")
        out.append("|---|---|")
        for ck in check_names:
            passed = sum(
                1 for r in promoted if r.quality and r.quality.checks.get(ck, False)
            )
            out.append(
                f"| `{ck}` | {passed}/{len(promoted)} ({100.0*passed/len(promoted):.0f}%) |"
            )
    return "\n".join(out) + "\n"


def print_report(results: list[ScenarioResult]) -> None:
    total = len(results)
    correct = sum(1 for r in results if r.correct)
    print()
    print("=" * 78)
    print("STAGE A CORRECTNESS")
    print("=" * 78)
    print(f"overall: {correct}/{total} ({100.0*correct/total:.1f}%)\n")
    print(confusion_matrix(results))

    print()
    print("=" * 78)
    print("PER-SCENARIO BREAKDOWN")
    print("=" * 78)
    for r in results:
        ok = "PASS" if r.correct else "FAIL"
        line = f"  [{ok}] {r.scenario.id:<22}  expected={r.scenario.expected:<14}  actual={r.actual}"
        if not r.correct:
            line += f"  // {r.scenario.rationale}"
        print(line)

    promoted = [r for r in results if r.actual == "promoted" and r.quality]
    if promoted:
        print()
        print("=" * 78)
        print("PROMOTED-SKILL QUALITY (per-skill rubric)")
        print("=" * 78)
        # Aggregate
        check_names: list[str] = []
        for r in promoted:
            for k in (r.quality.checks if r.quality else {}).keys():
                if k not in check_names:
                    check_names.append(k)

        avg_pct = sum(r.quality.pct for r in promoted if r.quality) / len(promoted)
        print(f"avg quality score: {avg_pct:.1f}% ({len(promoted)} skills)\n")

        # Per-check pass rate
        print("check-pass rate across promoted skills:")
        for ck in check_names:
            passed = sum(
                1 for r in promoted if r.quality and r.quality.checks.get(ck, False)
            )
            print(f"  {ck:<32}  {passed}/{len(promoted)}  ({100.0*passed/len(promoted):.0f}%)")

        # Per-skill breakdown
        print("\nper-skill scores:")
        for r in promoted:
            assert r.quality is not None
            print(f"  {r.scenario.id:<22}  {r.quality.score}/{r.quality.total} ({r.quality.pct:.0f}%)")
            failed = [k for k, v in r.quality.checks.items() if not v]
            if failed:
                print(f"    failed: {', '.join(failed)}")

    print()
    print("=" * 78)
    print("HEADLINE")
    print("=" * 78)
    correctness_pct = 100.0 * correct / total
    if promoted:
        avg_q = sum(r.quality.pct for r in promoted if r.quality) / len(promoted)
        print(f"  Stage A correctness:    {correctness_pct:.1f}%  ({correct}/{total})")
        print(f"  Promoted-skill quality: {avg_q:.1f}%  (mean over {len(promoted)} files)")
    else:
        print(f"  Stage A correctness:    {correctness_pct:.1f}%  ({correct}/{total})")
        print("  Promoted-skill quality: n/a (nothing promoted)")


# ---------- Main ----------


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--broker", default="http://localhost:7899")
    ap.add_argument("--token-file", default="/tmp/wuphf-broker-token-7899")
    ap.add_argument(
        "--home",
        default=os.environ.get("WUPHF_EVAL_HOME") or os.path.expanduser("~/.wuphf-dev-home/.wuphf"),
        help="Broker workspace dir (typically ~/.wuphf-dev-home/.wuphf)",
    )
    ap.add_argument(
        "--scenarios",
        default=str(Path(__file__).parent / "scenarios.json"),
    )
    ap.add_argument(
        "--keep",
        action="store_true",
        help="Skip cleanup; leave fixtures + promoted SKILL.md on disk for inspection",
    )
    ap.add_argument(
        "--report",
        default=None,
        help="If set, also write the report (markdown) to this path",
    )
    args = ap.parse_args()

    token_path = Path(args.token_file)
    if not token_path.exists():
        print(f"token file not found: {token_path}", file=sys.stderr)
        return 2
    token = token_path.read_text().strip()

    scenarios = load_scenarios(Path(args.scenarios))
    home = Path(args.home)

    print(f"[eval] broker={args.broker}  home={home}  scenarios={len(scenarios)}")
    results = evaluate(args.broker, token, home, scenarios, keep_artifacts=args.keep)
    print_report(results)

    if args.report:
        Path(args.report).write_text(render_markdown_report(results), encoding="utf-8")
        print(f"\n[eval] markdown report written to {args.report}")

    correct = sum(1 for r in results if r.correct)
    return 0 if correct == len(results) else 1


if __name__ == "__main__":
    sys.exit(main())

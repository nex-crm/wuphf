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
WIKI_REL = "wiki"

# Per-agent catalog ceiling enforced by eval. The broker logs a soft warning
# at 6KB (skillCatalogSoftWarnBytes); the eval treats >=8KB as a hard fail
# because that's where prompt-cache eviction risk starts mattering at scale.
CATALOG_BYTES_HARD_LIMIT = 8 * 1024
CATALOG_BYTES_SOFT_WARN = 6 * 1024
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
    # PR 7: per-agent scoping fields. Default to lead-routable shared so every
    # pre-existing scenario remains valid without a JSON migration.
    expected_owner_agents: list[str] = field(default_factory=list)
    # Where to seed this scenario inside the broker wiki. Defaults to
    # team/playbooks/, but path-inference scenarios use team/agents/<slug>/notebook.
    seed_subdir: str = "team/playbooks"
    # PR 7 task #13: similarity gate outcome the eval expects.
    #   create_new        — proposal lands as a new active/proposed skill (default).
    #   enhance_existing  — proposal MUST trigger errSkillSimilarToExisting at the
    #                       broker layer (not promoted, EnhancementCandidatesTotal
    #                       increments). Scenario is considered correct when
    #                       compile reports the skill as errored / not promoted.
    #   ambiguous         — proposal lands BUT rendered SKILL.md frontmatter
    #                       carries metadata.wuphf.similar_to_existing.
    expected_similarity_outcome: str = "create_new"
    # Visibility scenarios reference an already-promoted skill and simulate a
    # cross-role invoke. invoking_slug is the agent attempting the call;
    # target_skill_slug is the skill being invoked. expected_invoke_status
    # is 200 (lead-bypass / owner) or 403 (cross-role).
    invoking_slug: str = ""
    target_skill_slug: str = ""
    expected_invoke_status: int = 0  # 0 = not a visibility scenario
    expected_delegate_to: list[str] = field(default_factory=list)
    # PR 7 task #13 multi-pass seeding for similarity gate. pass=1 (default)
    # seeds before the first compile; pass=2 seeds after, then a second
    # compile fires. Lets the near-duplicate scenario collide with a real
    # canonical that was promoted in the first pass.
    seed_pass: int = 1

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


@dataclass
class VisibilityResult:
    """Outcome of a cross-role invoke check (PR 7 task #3).

    The eval simulates a non-owner agent calling /skills/{name}/invoke and
    asserts the broker returned the structured 403 + delegate_to body.
    """
    scenario_id: str
    invoker: str
    target: str
    expected_status: int
    actual_status: int
    ok: bool
    detail: str = ""


@dataclass
class VolumeMetrics:
    """How much noise the pipeline created against the human-interview queue.

    The skill compiler creates one `skill_proposal` request per promotion.
    A healthy run creates exactly one request per *expected* promotion and
    drains them on approve / reject. Over-promotion (D8) and duplicate
    proposals (the same slug appearing in the queue more than once) are
    both bad outcomes the user feels as "48 things to action".
    """

    proposals_created: int = 0
    requests_added: int = 0
    expected_promotions: int = 0
    over_promotions: int = 0  # actual_promoted - expected_promotions
    duplicate_proposals: int = 0  # same slug appearing in queue >1 time
    orphan_requests: int = 0  # skill_proposal entries with no corresponding skill
    # PR 7: catalog token-budget metric from /skills/compile/stats.
    # The broker computes max bytes the per-agent prompt-injected catalog
    # would render to; eval enforces an 8KB hard ceiling here.
    catalog_bytes_max: int = 0
    catalog_bytes_soft_warn: bool = False  # True when ≥6KB
    catalog_bytes_hard_fail: bool = False  # True when ≥8KB
    # PR 7 similarity gate: how many enhance_existing diversions happened
    # during this run vs how many scenarios expected one.
    enhancement_candidates_delta: int = 0
    expected_enhancements: int = 0


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
        # Try to parse as JSON so callers asserting structured 4xx bodies
        # (e.g. PR 7 not_owner / disabled) get a dict, not a raw string.
        try:
            return e.code, json.loads(payload)
        except (json.JSONDecodeError, ValueError):
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


def score_skill_file(
    path: Path,
    source_text: str,
    expected_owner_agents: list[str] | None = None,
    expected_similarity_outcome: str = "create_new",
) -> QualityScore:
    """Score the on-disk SKILL.md against the rubric.

    `source_text` is the original wiki article body (pre-promotion). We use
    it to grade hallucination + grounding, since description claims should
    be supported by the source. `expected_owner_agents` is the list the
    scenario declares; the rubric asserts the rendered frontmatter matches.
    `expected_similarity_outcome` is one of create_new / ambiguous; the
    eval asserts metadata.wuphf.similar_to_existing is set iff ambiguous.
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

    # PR 7: per-agent scoping metadata. parse_yaml_lite renders YAML lists as
    # a single-key dict (e.g. {"owner_agents": [...]}) rather than a flat list,
    # so we unwrap one nested level when present. Same shape as how `tags` and
    # `source_articles` come back through this parser.
    expected_owners = sorted([s.strip().lower() for s in (expected_owner_agents or []) if s.strip()])
    raw_owners = wuphf.get("owner_agents")
    if isinstance(raw_owners, dict):
        raw_owners = raw_owners.get("owner_agents") or []
    if raw_owners is None:
        raw_owners = []
    if isinstance(raw_owners, str):
        raw_owners = [raw_owners]
    actual_owners = sorted([str(s).strip().lower() for s in raw_owners if str(s).strip()])
    q.checks["owner_agents_matches_expected"] = actual_owners == expected_owners

    # PR 7 task #13: similarity gate frontmatter shape.
    similar_to = wuphf.get("similar_to_existing")
    # Same nested-dict unwrap as owner_agents — parse_yaml_lite renders
    # `similar_to_existing:\n  slug: ...` as {similar_to_existing: {slug:...}}.
    if isinstance(similar_to, dict) and set(similar_to.keys()) == {"similar_to_existing"}:
        similar_to = similar_to.get("similar_to_existing")
    if expected_similarity_outcome == "ambiguous":
        ok = isinstance(similar_to, dict) and bool(similar_to.get("slug"))
        q.checks["similar_to_existing_set_when_ambiguous"] = ok
    elif expected_similarity_outcome == "create_new":
        q.checks["similar_to_existing_clean_when_new"] = not similar_to

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


def seed_corpus(
    wiki_root: Path,
    scenarios: list[Scenario],
    run_id: str,
    seed_pass: int = 1,
) -> None:
    """Write each scenario into its target wiki subdir.

    PR 7: scenarios choose their own seed_subdir so path-inference scenarios
    (team/agents/<slug>/notebook) land where the Stage A scanner expects them.
    Visibility scenarios reference an already-seeded skill and contribute no
    fixture file, so we skip them here.

    Only scenarios with sc.seed_pass == seed_pass are written. The runner
    seeds pass=1 before the first compile, then pass=2 after, so the
    similarity gate can compare a near-duplicate against a canonical that
    actually landed in b.skills in the first pass.

    The scanner keeps a per-process SHA cache and skips re-classification
    when content hashes match a prior pass. We append a hidden HTML comment
    carrying a unique run id so each invocation hashes to a fresh value and
    the eval is repeatable without restarting the broker.
    """
    nonce = f"\n<!-- eval-run: {run_id}-p{seed_pass} -->\n"
    for sc in scenarios:
        if sc.category == "VISIBILITY":
            continue
        if sc.seed_pass != seed_pass:
            continue
        target_dir = wiki_root / sc.seed_subdir
        target_dir.mkdir(parents=True, exist_ok=True)
        body = sc.content
        if not body.endswith("\n"):
            body += "\n"
        (target_dir / sc.filename).write_text(body + nonce, encoding="utf-8")


def cleanup_corpus(wiki_root: Path, skills_dir: Path) -> None:
    """Remove every eval-* fixture from anywhere under wiki/ and any skills the
    scanner promoted from this corpus run. We rglob from wiki_root so nested
    seed_subdirs (team/agents/<slug>/notebook/...) are swept too.
    """
    if wiki_root.exists():
        for p in wiki_root.rglob(f"{EVAL_PREFIX}*.md"):
            p.unlink()
    if skills_dir.exists():
        for p in skills_dir.glob(f"{EVAL_PREFIX}*.md"):
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


def approve_skill(broker: str, token: str, name: str) -> tuple[int, Any]:
    """POST /skills/{name}/approve to flip status proposed -> active.

    Required before visibility scenarios can exercise the structured 403,
    since handleInvokeSkill returns a plain text 403 for non-active skills
    (the structured shape only fires for not_owner / disabled).
    """
    return http_request(
        "POST",
        f"{broker}/skills/{urllib.parse.quote(name)}/approve",
        token=token,
        body={},
    )


def list_skills(broker: str, token: str) -> list[dict[str, Any]]:
    code, body = http_request("GET", f"{broker}/skills", token=token)
    if code != 200 or not isinstance(body, dict):
        return []
    return list(body.get("skills") or [])


def list_requests(broker: str, token: str) -> list[dict[str, Any]]:
    code, body = http_request("GET", f"{broker}/requests", token=token)
    if code != 200 or not isinstance(body, dict):
        return []
    return list(body.get("requests") or [])


def get_compile_stats(broker: str, token: str) -> dict[str, Any]:
    code, body = http_request("GET", f"{broker}/skills/compile/stats", token=token)
    if code != 200 or not isinstance(body, dict):
        return {}
    return body


def trigger_compile(broker: str, token: str) -> dict[str, Any]:
    # Compile may run a Stage B LLM synthesis pass on top of the fast-path
    # scan, so allow generous time for live LLM round-trips.
    code, body = http_request(
        "POST", f"{broker}/skills/compile", token=token, body={}, timeout=180.0
    )
    if isinstance(body, dict):
        return {**body, "_status": code}
    return {"_status": code, "_raw": body}


def trigger_invoke(
    broker: str, token: str, skill_name: str, my_slug: str
) -> tuple[int, dict[str, Any] | str]:
    """POST /skills/{name}/invoke with invoked_by=my_slug.

    Mirrors what the team_skill_run MCP handler sends (internal/teammcp/skills.go).
    Returns (status_code, body) for the caller to assert against. The broker's
    visibility gate (handleInvokeSkill) returns the structured 403 documented
    in the design doc.
    """
    return http_request(
        "POST",
        f"{broker}/skills/{urllib.parse.quote(skill_name)}/invoke",
        token=token,
        body={"invoked_by": my_slug, "channel": "general"},
    )


def assert_403_delegate_to(
    code: int, body: Any, expected_delegate_to: list[str]
) -> tuple[bool, str]:
    """Assert the broker's structured 403 contract (PR 7 task #3).

    Contract (from internal/team/broker_skill_invocation_integration_test.go +
    internal/teammcp/skills.go::parseStructuredSkillForbidden):
      { "ok": false, "error": "not_owner", "delegate_to": [...], "hint": "..." }
    """
    if code != 403:
        return False, f"want HTTP 403, got {code}"
    if not isinstance(body, dict):
        return False, f"non-dict body: {type(body).__name__}"
    if body.get("error") != "not_owner":
        return False, f"want error=not_owner, got {body.get('error')!r}"
    delegate = body.get("delegate_to")
    if not isinstance(delegate, list):
        return False, "delegate_to is not a list"
    if expected_delegate_to and sorted(delegate) != sorted(expected_delegate_to):
        return False, f"delegate_to={delegate}, want {expected_delegate_to}"
    if not body.get("hint"):
        return False, "hint missing or empty"
    return True, ""


def seed_synthetic_skills_for_budget(
    skills_dir: Path, members: list[str], n: int = 100
) -> list[str]:
    """Write N synthetic SKILL.md files for the token-budget pre-flight.

    Returns the list of slugs created so the runner can clean them up.
    Files land directly in wiki/team/skills/ — they bypass the proposal
    pipeline because the broker's reconcileSkillStatusFromDisk path picks
    them up on next compile / restart. For the eval, we use them only to
    ask /skills/compile/stats for catalog_bytes_per_agent_max with a
    realistic skills count; we do not assert that they were "promoted".

    Owner sets cycle through {members[i:i+1]} plus an empty (lead-routable)
    bucket, giving roughly 5 distinct catalog shapes across N files.
    """
    skills_dir.mkdir(parents=True, exist_ok=True)
    slugs: list[str] = []
    member_count = max(1, len(members))
    for i in range(n):
        slug = f"{EVAL_PREFIX}budget-{i:03d}"
        # Cycle: members + one empty bucket → ~5 shapes if members ≥ 4.
        owner_bucket = i % (member_count + 1)
        if owner_bucket == member_count:
            owners_yaml = "[]"
        else:
            owners_yaml = f"[{members[owner_bucket]}]"
        content = (
            "---\n"
            f"name: {slug}\n"
            f"description: Synthetic eval skill {i:03d} for catalog-byte budget pre-flight\n"
            "metadata:\n"
            "  wuphf:\n"
            f"    trigger: synthetic eval skill {i} for token budget assertions\n"
            "    created_by: eval-harness\n"
            "    safety_scan:\n"
            "      verdict: clean\n"
            "      ts: 2026-04-29T00:00:00Z\n"
            f"    owner_agents: {owners_yaml}\n"
            "    source_articles: []\n"
            "    tags: [synthetic, eval]\n"
            "    status: active\n"
            "---\n\n"
            "## Steps\n\n"
            f"1. Synthetic skill body for budget pre-flight scenario {i}.\n"
            f"2. Has just enough shape to satisfy reconcile + catalog render.\n"
            f"3. Exists only to populate b.skills for /skills/compile/stats.\n"
        )
        (skills_dir / f"{slug}.md").write_text(content, encoding="utf-8")
        slugs.append(slug)
    return slugs


def cleanup_synthetic_budget_skills(skills_dir: Path) -> None:
    if not skills_dir.exists():
        return
    for p in skills_dir.glob(f"{EVAL_PREFIX}budget-*.md"):
        p.unlink()


def _wait_for_skill_files(
    broker: str, token: str, skills_dir: Path, timeout_s: float = 30.0
) -> None:
    """Poll until every active/proposed skill the broker reports has its
    SKILL.md on disk. WikiWorker.Enqueue runs async; reading on-disk SKILL.md
    immediately after /skills/compile returns is racy. Bounded by timeout_s.
    """
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        skills = list_skills(broker, token)
        if not skills:
            return
        missing = [
            s["name"] for s in skills
            if s.get("status") in ("active", "proposed", "disabled")
            and not (skills_dir / f"{s['name']}.md").exists()
        ]
        if not missing:
            return
        time.sleep(0.25)
    skills = list_skills(broker, token)
    still_missing = [
        s["name"] for s in skills
        if s.get("status") in ("active", "proposed", "disabled")
        and not (skills_dir / f"{s['name']}.md").exists()
    ]
    if still_missing:
        print(f"[eval] warn: {len(still_missing)} skills missing on disk after {timeout_s}s: {still_missing[:5]}")


def evaluate(
    broker: str,
    token: str,
    home: Path,
    scenarios: list[Scenario],
    keep_artifacts: bool,
) -> tuple[list[ScenarioResult], VolumeMetrics, list[VisibilityResult]]:
    wiki_root = home / WIKI_REL
    skills = home / WIKI_SKILLS_REL
    promoted_slugs: list[str] = []

    run_id = f"{int(time.time())}-{os.getpid()}"
    fixture_count = sum(1 for sc in scenarios if sc.category != "VISIBILITY")
    print(f"[eval] seeding {fixture_count} fixture scenarios under {wiki_root}  run_id={run_id}")
    cleanup_corpus(wiki_root, skills)  # belt-and-braces
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
    seed_corpus(wiki_root, scenarios, run_id, seed_pass=1)

    # Snapshot proposal counters + interview queue BEFORE compile so we can
    # measure how much noise this run added to both surfaces.
    stats_before = get_compile_stats(broker, token)
    requests_before = list_requests(broker, token)
    skill_props_before = sum(
        1 for r in requests_before if r.get("kind") == "skill_proposal"
    )

    print("[eval] triggering /skills/compile (pass 1)")
    compile_result = trigger_compile(broker, token)
    print(f"[eval] compile pass-1 response: {json.dumps(compile_result, indent=2)}")

    # Give the WikiWorker time to flush writes before we read SKILL.md.
    # WikiWorker.Enqueue is async; with ~17 skills in flight 0.5s is too short.
    # Poll until every promoted skill's on-disk SKILL.md is materialised.
    _wait_for_skill_files(broker, token, home / WIKI_SKILLS_REL, timeout_s=30.0)

    # PR 7 task #13: if any scenario seeds in pass=2, run a second compile.
    # The first pass establishes the canonical skills in b.skills; we then
    # APPROVE every canonical that has a downstream pass-2 collision target
    # (the similarity gate only compares against active skills, not proposed),
    # then seed pass-2 fixtures and recompile.
    has_pass_2 = any(sc.seed_pass == 2 and sc.category != "VISIBILITY" for sc in scenarios)
    compile_result_p2: dict[str, Any] = {}
    if has_pass_2:
        # Approve ONLY canonicals that downstream pass-2 scenarios collide
        # with. The similarity gate compares against active skills only, so
        # the canonical for an enhance_existing scenario must be flipped to
        # active before pass-2's compile fires. We avoid approving every
        # promote scenario en-masse (that produces a flurry of git commits
        # which can race with WikiWorker writes for unrelated skills).
        pass1_skills = list_skills(broker, token)
        pass1_by_name = {s["name"]: s for s in pass1_skills}
        # An enhance_existing scenario in pass 2 implies its canonical lives
        # in pass 1. We don't have an explicit edge in the scenario JSON, so
        # we approve every pass-1 PROMOTE that shares a body-keyword with a
        # pass-2 enhance_existing scenario. In practice: approving the small
        # set that overlaps any near-duplicate body. For the current corpus
        # the only colliding canonical is `chase-overdue-invoice`, so we
        # approve every pass-1 scenario whose slug appears in any pass-2
        # near-duplicate body. Approximation that keeps the noise low.
        pass2_bodies = " ".join(
            sc.content.lower()
            for sc in scenarios
            if sc.seed_pass == 2 and sc.expected_similarity_outcome == "enhance_existing"
        )
        for sc in scenarios:
            if sc.category == "VISIBILITY":
                continue
            if sc.seed_pass != 1 or sc.expected != "promoted":
                continue
            slug = sc.slug
            if not slug or slug not in pass1_by_name:
                continue
            if (pass1_by_name[slug].get("status") or "") == "active":
                continue
            # Only approve when the canonical's slug or one of its body
            # keywords actually appears in a pass-2 scenario's body.
            if slug not in pass2_bodies and not any(
                w in pass2_bodies for w in slug.split("-") if len(w) > 4
            ):
                continue
            code, body = approve_skill(broker, token, slug)
            if code >= 400:
                print(f"[eval] warn: approve {slug} (canonical) failed: {code} {body}")
            else:
                promoted_slugs.append(slug)

        seed_corpus(wiki_root, scenarios, run_id, seed_pass=2)
        print("[eval] triggering /skills/compile (pass 2 — similarity gate)")
        compile_result_p2 = trigger_compile(broker, token)
        print(f"[eval] compile pass-2 response: {json.dumps(compile_result_p2, indent=2)}")
        _wait_for_skill_files(broker, token, home / WIKI_SKILLS_REL, timeout_s=30.0)

    skills_after = list_skills(broker, token)
    by_name = {s["name"]: s for s in skills_after}
    requests_after = list_requests(broker, token)
    stats_after = get_compile_stats(broker, token)

    # Build a slug→error map from BOTH compile responses so guard rejections
    # from either pass are visible to the per-scenario classifier.
    error_slugs: dict[str, str] = {}
    for cr in (compile_result, compile_result_p2):
        for err in cr.get("errors", []) or []:
            slug_key = err.get("slug", "")
            reason = err.get("reason", "")
            if slug_key:
                error_slugs[slug_key] = reason

    # PR 7 task #13 (post 24a2fc06 + 23e4d698): the scanner now catches the
    # similarity sentinel and emits a kind="enhance_skill_proposal" interview
    # request. Map candidate-slug → enhances-slug so the per-scenario
    # classifier can mark the diversion as a similarity outcome rather than
    # an unexplained skip. Both reply_to (candidate) and metadata.enhances_slug
    # (canonical) live on the request object.
    enhance_diversions: dict[str, str] = {}
    for r in requests_after:
        if r.get("kind") != "enhance_skill_proposal":
            continue
        candidate = (r.get("reply_to") or "").strip().lower()
        if not candidate:
            continue
        meta = r.get("metadata") or {}
        enhances = ""
        if isinstance(meta, dict):
            enhances = str(meta.get("enhances_slug") or "").strip().lower()
        enhance_diversions[candidate] = enhances

    results: list[ScenarioResult] = []
    for sc in scenarios:
        # Visibility scenarios are evaluated separately below.
        if sc.category == "VISIBILITY":
            continue

        slug = sc.slug
        actual = "skipped"
        detail = ""
        on_disk: Path | None = None

        # Check whether THIS scenario hit the similarity gate by looking up
        # the candidate slug in the enhance_diversions map (built from the
        # broker's enhance_skill_proposal interview requests).
        fname_slug = sc.filename.replace(EVAL_PREFIX, "").replace(".md", "")
        diverted_to = ""
        if slug and slug.lower() in enhance_diversions:
            diverted_to = enhance_diversions[slug.lower()]
        elif fname_slug and fname_slug.lower() in enhance_diversions:
            diverted_to = enhance_diversions[fname_slug.lower()]
        diverted = bool(slug and slug.lower() in enhance_diversions) or bool(
            fname_slug and fname_slug.lower() in enhance_diversions
        )

        if diverted:
            actual = "skipped"  # similarity-diverted; treat as a skip-with-reason
            detail = f"similarity-diverted to: {diverted_to or '<unknown>'}"
        elif slug and slug in by_name:
            actual = "promoted"
            on_disk = skills / f"{slug}.md"
            promoted_slugs.append(slug)
        elif slug and any(slug in s for s in error_slugs.keys()):
            actual = "guard_rejected"
            for k, reason in error_slugs.items():
                if slug in k:
                    detail = reason
                    break
        else:
            if fname_slug in error_slugs:
                actual = "guard_rejected"
                detail = error_slugs[fname_slug]

        # PR 7: enhance-existing scenarios are correct iff a corresponding
        # enhance_skill_proposal request exists AND the candidate did not
        # promote. The broker counter increment is asserted globally below.
        if sc.expected_similarity_outcome == "enhance_existing":
            correct = diverted and actual == "skipped"
        else:
            correct = actual == sc.expected

        quality: QualityScore | None = None
        if actual == "promoted" and on_disk and on_disk.exists():
            quality = score_skill_file(
                on_disk,
                sc.content,
                expected_owner_agents=sc.expected_owner_agents,
                expected_similarity_outcome=sc.expected_similarity_outcome,
            )

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

    # PR 7: visibility scenarios — invoke a previously-promoted skill as a
    # different agent and assert the structured 403 + delegate_to contract.
    # Skills land as `proposed`, but handleInvokeSkill emits the structured
    # 403 only AFTER the status gate passes — so we approve every visibility
    # target first to flip it to active. The approve happens through the
    # same /skills/{name}/approve endpoint humans use from the Skills app.
    visibility_targets = {
        sc.target_skill_slug for sc in scenarios
        if sc.category == "VISIBILITY" and sc.target_skill_slug
    }
    for target in visibility_targets:
        if target not in by_name:
            continue
        if (by_name[target].get("status") or "") == "active":
            continue
        code, body = approve_skill(broker, token, target)
        if code >= 400:
            print(f"[eval] warn: approve {target} failed: {code} {body}")
        else:
            promoted_slugs.append(target)  # ensure cleanup phase rejects it
    # Refresh by_name so quality scoring sees the post-approve state if needed.
    skills_after = list_skills(broker, token)
    by_name = {s["name"]: s for s in skills_after}

    visibility_results: list[VisibilityResult] = []
    for sc in scenarios:
        if sc.category != "VISIBILITY":
            continue
        target = sc.target_skill_slug or ""
        invoker = sc.invoking_slug or ""
        if not target or not invoker:
            visibility_results.append(
                VisibilityResult(
                    scenario_id=sc.id,
                    invoker=invoker,
                    target=target,
                    expected_status=sc.expected_invoke_status,
                    actual_status=0,
                    ok=False,
                    detail="visibility scenario missing target_skill_slug or invoking_slug",
                )
            )
            continue
        code, body = trigger_invoke(broker, token, target, invoker)
        if sc.expected_invoke_status == 200:
            ok = code == 200
            detail = "" if ok else f"want 200, got {code}: {body!r}"
        else:
            ok, detail = assert_403_delegate_to(code, body, sc.expected_delegate_to)
        visibility_results.append(
            VisibilityResult(
                scenario_id=sc.id,
                invoker=invoker,
                target=target,
                expected_status=sc.expected_invoke_status,
                actual_status=code,
                ok=ok,
                detail=detail,
            )
        )

    # Volume metrics: skill_proposal requests created in this run, plus
    # duplicate / orphan detection across the interview queue.
    expected_promotions = sum(
        1 for sc in scenarios
        if sc.category != "VISIBILITY" and sc.expected == "promoted"
    )
    actual_promoted = sum(1 for r in results if r.actual == "promoted")

    skill_props_after = [r for r in requests_after if r.get("kind") == "skill_proposal"]
    requests_added = max(0, len(skill_props_after) - skill_props_before)
    proposals_delta = max(
        0,
        int(stats_after.get("proposals_created_total", 0))
        - int(stats_before.get("proposals_created_total", 0)),
    )

    # Duplicate detection: how many slugs appear more than once in the
    # pending interview queue. Each promotion should land exactly one
    # request; duplicates mean a re-promotion path created an orphan.
    pending_props = [r for r in skill_props_after if r.get("status") == "pending"]
    slug_counts: dict[str, int] = {}
    for r in pending_props:
        slug = skillSlugFromText(r.get("reply_to") or r.get("title") or "")
        slug_counts[slug] = slug_counts.get(slug, 0) + 1
    duplicate_proposals = sum(c - 1 for c in slug_counts.values() if c > 1)

    # Orphan detection: pending skill_proposal requests with reply_to that
    # doesn't match any current skill. These are the D8-style ghosts.
    skill_names = {s.get("name") for s in skills_after}
    orphan_requests = sum(
        1
        for r in pending_props
        if (r.get("reply_to") or "") and r.get("reply_to") not in skill_names
    )

    # PR 7: catalog token-budget + similarity-counter deltas.
    catalog_max = int(stats_after.get("catalog_bytes_per_agent_max", 0) or 0)
    enhancement_delta = max(
        0,
        int(stats_after.get("enhancement_candidates_total", 0))
        - int(stats_before.get("enhancement_candidates_total", 0)),
    )
    expected_enhancements = sum(
        1 for sc in scenarios if sc.expected_similarity_outcome == "enhance_existing"
    )

    metrics = VolumeMetrics(
        proposals_created=proposals_delta,
        requests_added=requests_added,
        expected_promotions=expected_promotions,
        over_promotions=max(0, actual_promoted - expected_promotions),
        duplicate_proposals=duplicate_proposals,
        orphan_requests=orphan_requests,
        catalog_bytes_max=catalog_max,
        catalog_bytes_soft_warn=catalog_max >= CATALOG_BYTES_SOFT_WARN,
        catalog_bytes_hard_fail=catalog_max >= CATALOG_BYTES_HARD_LIMIT,
        enhancement_candidates_delta=enhancement_delta,
        expected_enhancements=expected_enhancements,
    )

    if not keep_artifacts:
        # Reject every promoted eval skill so the broker's in-memory state
        # also forgets about them. With the D8 fix in place this also drains
        # the matching skill_proposal interview requests.
        for slug in promoted_slugs:
            try:
                reject_skill(broker, token, slug, "skill_eval cleanup")
            except Exception as exc:  # pragma: no cover
                print(f"[eval] warn: reject {slug} failed: {exc}")
        cleanup_promoted_slugs(skills, promoted_slugs)
        cleanup_corpus(wiki_root, skills)

    return results, metrics, visibility_results


def skillSlugFromText(s: str) -> str:
    s = s.strip().lower()
    if s.startswith("approve skill: "):
        s = s[len("approve skill: ") :]
    return s.replace(" ", "-").replace("_", "-")


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


def render_markdown_report(
    results: list[ScenarioResult],
    metrics: VolumeMetrics,
    visibility: list[VisibilityResult] | None = None,
) -> str:
    total = len(results)
    correct = sum(1 for r in results if r.correct)
    promoted = [r for r in results if r.actual == "promoted" and r.quality]
    avg_q = (
        sum(r.quality.pct for r in promoted if r.quality) / len(promoted)
        if promoted
        else 0.0
    )
    visibility = visibility or []
    vis_pass = sum(1 for v in visibility if v.ok)

    out: list[str] = []
    out.append("# Skill-pipeline evaluation report")
    out.append("")
    out.append(f"- **Stage A correctness:** {100.0*correct/total:.1f}% ({correct}/{total})")
    if promoted:
        out.append(
            f"- **Promoted-skill quality:** {avg_q:.1f}% mean over {len(promoted)} files"
        )
    if visibility:
        out.append(
            f"- **Visibility (403 + delegate_to):** "
            f"{100.0*vis_pass/len(visibility):.0f}% ({vis_pass}/{len(visibility)})"
        )
    out.append(
        f"- **Catalog bytes max:** {metrics.catalog_bytes_max} "
        f"(soft warn ≥{CATALOG_BYTES_SOFT_WARN}, hard fail ≥{CATALOG_BYTES_HARD_LIMIT})"
    )
    out.append(
        f"- **Enhance-existing diversions:** {metrics.enhancement_candidates_delta} "
        f"(expected {metrics.expected_enhancements})"
    )
    out.append(
        f"- **Proposal volume:** {metrics.proposals_created} created, "
        f"{metrics.requests_added} interview requests, "
        f"{metrics.over_promotions} over-promotions, "
        f"{metrics.duplicate_proposals} duplicates, "
        f"{metrics.orphan_requests} orphan requests"
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
    out.append("| id | category | expected | actual | sim_outcome | pass | rationale |")
    out.append("|---|---|---|---|---|---|---|")
    for r in results:
        sign = "PASS" if r.correct else "FAIL"
        out.append(
            f"| `{r.scenario.id}` | {r.scenario.category} | "
            f"{r.scenario.expected} | {r.actual} | "
            f"{r.scenario.expected_similarity_outcome} | {sign} | "
            f"{r.scenario.rationale.replace('|', '/')} |"
        )
    if visibility:
        out.append("")
        out.append("## Visibility checks (cross-role invoke)")
        out.append("")
        out.append("| id | invoker | target | want | got | pass | detail |")
        out.append("|---|---|---|---|---|---|---|")
        for v in visibility:
            sign = "PASS" if v.ok else "FAIL"
            out.append(
                f"| `{v.scenario_id}` | `{v.invoker}` | `{v.target}` | "
                f"{v.expected_status} | {v.actual_status} | {sign} | "
                f"{v.detail.replace('|', '/') if v.detail else ''} |"
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


def print_volume_report(metrics: VolumeMetrics) -> None:
    print()
    print("=" * 78)
    print("PROPOSAL VOLUME (interview-queue load)")
    print("=" * 78)
    print(f"  expected promotions:    {metrics.expected_promotions}")
    print(f"  proposals created:      {metrics.proposals_created}")
    print(f"  requests added:         {metrics.requests_added}")
    print(f"  over-promotion:         {metrics.over_promotions}")
    print(f"  duplicate proposals:    {metrics.duplicate_proposals}")
    print(f"  orphan requests:        {metrics.orphan_requests}")
    print(f"  catalog bytes max:      {metrics.catalog_bytes_max}"
          f"  (soft warn ≥{CATALOG_BYTES_SOFT_WARN}, hard fail ≥{CATALOG_BYTES_HARD_LIMIT})")
    print(f"  enhance-existing:       {metrics.enhancement_candidates_delta}/"
          f"{metrics.expected_enhancements} (delta/expected)")
    issues: list[str] = []
    if metrics.over_promotions > 0:
        issues.append(f"  ! over-promoted by {metrics.over_promotions}")
    if metrics.requests_added > metrics.proposals_created:
        issues.append(
            f"  ! requests_added ({metrics.requests_added}) > proposals_created ({metrics.proposals_created})"
        )
    if metrics.duplicate_proposals > 0:
        issues.append(f"  ! {metrics.duplicate_proposals} duplicate proposals in queue")
    if metrics.orphan_requests > 0:
        issues.append(f"  ! {metrics.orphan_requests} orphan requests")
    if metrics.catalog_bytes_hard_fail:
        issues.append(
            f"  ! catalog bytes max {metrics.catalog_bytes_max} ≥ hard limit {CATALOG_BYTES_HARD_LIMIT}"
        )
    elif metrics.catalog_bytes_soft_warn:
        issues.append(
            f"  ~ catalog bytes max {metrics.catalog_bytes_max} ≥ soft warn {CATALOG_BYTES_SOFT_WARN}"
        )
    if metrics.enhancement_candidates_delta != metrics.expected_enhancements:
        issues.append(
            f"  ! enhance-existing delta {metrics.enhancement_candidates_delta} != "
            f"expected {metrics.expected_enhancements}"
        )
    if issues:
        print("\nFLAGS:")
        for issue in issues:
            print(issue)
    else:
        print("\n  no volume flags")


def print_visibility_report(visibility: list[VisibilityResult]) -> None:
    if not visibility:
        return
    print()
    print("=" * 78)
    print("VISIBILITY (cross-role invoke + 403 contract)")
    print("=" * 78)
    passed = sum(1 for v in visibility if v.ok)
    print(f"  pass rate: {passed}/{len(visibility)} ({100.0*passed/len(visibility):.0f}%)\n")
    for v in visibility:
        sign = "PASS" if v.ok else "FAIL"
        line = (
            f"  [{sign}] {v.scenario_id:<22}  "
            f"{v.invoker} -> {v.target}  want={v.expected_status} got={v.actual_status}"
        )
        if not v.ok:
            line += f"  // {v.detail}"
        print(line)


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
    results, metrics, visibility = evaluate(
        args.broker, token, home, scenarios, keep_artifacts=args.keep
    )
    print_report(results)
    print_visibility_report(visibility)
    print_volume_report(metrics)

    # PR 7 owner_agents accuracy: every promoted scenario must have its
    # rendered owner_agents match scenario.expected_owner_agents.
    promoted = [r for r in results if r.actual == "promoted" and r.quality]
    owner_pass = sum(
        1 for r in promoted
        if r.quality and r.quality.checks.get("owner_agents_matches_expected", False)
    )
    owner_total = len(promoted)

    if args.report:
        Path(args.report).write_text(
            render_markdown_report(results, metrics, visibility), encoding="utf-8"
        )
        print(f"\n[eval] markdown report written to {args.report}")

    correct = sum(1 for r in results if r.correct)
    visibility_pass = all(v.ok for v in visibility) if visibility else True
    healthy_volume = (
        metrics.over_promotions == 0
        and metrics.duplicate_proposals == 0
        and metrics.orphan_requests == 0
    )
    catalog_ok = not metrics.catalog_bytes_hard_fail
    enhance_ok = metrics.enhancement_candidates_delta == metrics.expected_enhancements
    owner_ok = owner_total == 0 or owner_pass == owner_total

    print()
    print("=" * 78)
    print("ACCEPTANCE GATE")
    print("=" * 78)
    print(f"  scenario correctness: {correct}/{len(results)}  ({'PASS' if correct == len(results) else 'FAIL'})")
    print(f"  owner_agents match:   {owner_pass}/{owner_total}  ({'PASS' if owner_ok else 'FAIL'})")
    print(f"  visibility 403 shape: {sum(1 for v in visibility if v.ok)}/{len(visibility)}  ({'PASS' if visibility_pass else 'FAIL'})")
    print(f"  catalog bytes < 8KB:  {metrics.catalog_bytes_max}  ({'PASS' if catalog_ok else 'FAIL'})")
    print(f"  enhance delta match:  {metrics.enhancement_candidates_delta}=={metrics.expected_enhancements}  ({'PASS' if enhance_ok else 'FAIL'})")
    print(f"  volume healthy:       over={metrics.over_promotions} dup={metrics.duplicate_proposals} orphan={metrics.orphan_requests}  ({'PASS' if healthy_volume else 'FAIL'})")

    return 0 if (
        correct == len(results)
        and visibility_pass
        and healthy_volume
        and catalog_ok
        and enhance_ok
        and owner_ok
    ) else 1


if __name__ == "__main__":
    sys.exit(main())

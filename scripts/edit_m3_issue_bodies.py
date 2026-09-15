#!/usr/bin/env python3
"""Edit existing M3 GitHub issue bodies from docs/milestones/m3-jupiter.md. Idempotent."""
from __future__ import annotations

import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = "lognorman20/monaco"
ROOT = Path(__file__).resolve().parents[1]
DOC = ROOT / "docs" / "milestones" / "m3-jupiter.md"

SECTIONS = [
    "Context",
    "Problem",
    "Proposal",
    "Acceptance Criteria",
    "Verification",
    "Dependencies",
]


def gh_json(*args: str) -> list | dict:
    proc = subprocess.run(["gh", *args], capture_output=True, text=True, cwd=ROOT)
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr or proc.stdout)
    return json.loads(proc.stdout or "null")


def fetch_m3_issue_map() -> dict[str, int]:
    items = gh_json(
        "issue", "list", "--repo", REPO, "--label", "milestone-m3",
        "--limit", "100", "--json", "number,title",
    )
    out: dict[str, int] = {}
    for it in items:
        m = re.match(r"(M3-T\d+[a-z]?):", it["title"])
        if m:
            out[m.group(1)] = it["number"]
    return out


def parse_tickets(text: str) -> dict[str, str]:
    pattern = re.compile(r"^#### (M3-T\d+[a-z]?): .+\n", re.M)
    matches = list(pattern.finditer(text))
    tickets: dict[str, str] = {}
    for i, m in enumerate(matches):
        mid = m.group(1)
        start = m.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(text)
        raw = text[start:end].strip()
        tickets[mid] = raw
    return tickets


def doc_to_github_body(raw: str, issue_nums: dict[str, int]) -> str:
    body = raw
    for section in SECTIONS:
        body = re.sub(
            rf"^\*\*{re.escape(section)}\*\*\s*$",
            f"## {section}",
            body,
            flags=re.M,
        )

    def link_dep(match: re.Match[str]) -> str:
        tid = match.group(1)
        suffix = match.group(2) or ""
        if tid in issue_nums:
            return f"{tid} (#{issue_nums[tid]}){suffix}"
        return match.group(0)

    body = re.sub(r"(M3-T\d+[a-z]?)([^#\w]|$)", link_dep, body)
    return body.strip() + "\n"


def validate_body(body: str) -> tuple[bool, list[str]]:
    errors: list[str] = []
    for h in SECTIONS:
        if f"## {h}" not in body:
            errors.append(f"missing ## {h}")
    text_no_headings = re.sub(r"^#+\s.*$", "", body, flags=re.M).strip()
    if len(text_no_headings) < 120:
        errors.append(f"body too short ({len(text_no_headings)} chars)")
    ac = body.split("## Acceptance Criteria", 1)[1].split("## Verification", 1)[0]
    bullets = [b for b in re.findall(r"^- \[ \] .+", ac, flags=re.M) if len(b) > 10]
    if len(bullets) < 2:
        errors.append(f"need >=2 AC bullets, found {len(bullets)}")
    return len(errors) == 0, errors


def edit_issue(number: int, body: str) -> None:
    with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as f:
        f.write(body)
        path = f.name
    try:
        proc = subprocess.run(
            ["gh", "issue", "edit", str(number), "--repo", REPO, "--body-file", path],
            capture_output=True,
            text=True,
            cwd=ROOT,
        )
        if proc.returncode != 0:
            raise RuntimeError(f"issue #{number}: {proc.stderr or proc.stdout}")
    finally:
        Path(path).unlink(missing_ok=True)


def main() -> int:
    text = DOC.read_text()
    raw_tickets = parse_tickets(text)
    issue_nums = fetch_m3_issue_map()

    missing_issues = sorted(set(raw_tickets) - set(issue_nums))
    if missing_issues:
        print("ERROR: no GitHub issue for:", ", ".join(missing_issues), file=sys.stderr)
        return 1

    edited: list[int] = []
    skipped: list[int] = []
    for mid in sorted(raw_tickets, key=lambda x: (int(re.search(r"T(\d+)", x).group(1)), x)):
        num = issue_nums[mid]
        body = doc_to_github_body(raw_tickets[mid], issue_nums)
        ok, errs = validate_body(body)
        if not ok:
            print(f"ERROR {mid} #{num}: {errs}", file=sys.stderr)
            return 1

        cur = gh_json("issue", "view", str(num), "--repo", REPO, "--json", "body")["body"]
        if (cur or "").strip() == body.strip():
            skipped.append(num)
            print(f"skip #{num} {mid} (unchanged)")
            continue

        edit_issue(num, body)
        edited.append(num)
        print(f"edited #{num} {mid}")

    print(f"\nedited={len(edited)} skipped={len(skipped)} total={len(raw_tickets)}")
    print("edited_numbers:", ",".join(str(n) for n in sorted(edited)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

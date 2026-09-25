#!/usr/bin/env python3
"""Create GitHub milestones/issues from milestone ticket metadata. Idempotent."""
from __future__ import annotations

import json
import re
import subprocess
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

REPO = "lognorman20/monaco"
ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs" / "archive" / "milestones"

MILESTONES = [
    ("M0 — Scaffold", "Runnable monorepo skeleton"),
    ("M1 — Auth and wallets", "Privy SMS + email/password, member + treasury wallets"),
    ("M2 — Deposits", "Deposit → sweep → 1:1 credit"),
    ("M3 — Jupiter", "Buy/sell tokenized stocks via backend"),
    ("M4 — Domain", "Groups, votes, NAV, redeem, boards"),
    ("M5 — Mobile UI", "Product iOS UI"),
]

LABELS = [
    ("milestone-m0", "D73A4A", "M0 scaffold work"),
    ("milestone-m1", "D73A4A", "M1 auth and wallets"),
    ("milestone-m2", "D73A4A", "M2 deposits"),
    ("milestone-m3", "D73A4A", "M3 Jupiter"),
    ("milestone-m4", "D73A4A", "M4 domain"),
    ("milestone-m5", "D73A4A", "M5 mobile UI"),
    ("feature", "A2EEEF", "New capability"),
    ("bug", "D73A4A", "Defect fix"),
    ("refactor", "C5DEF5", "Structural change, same behavior"),
    ("chore", "FEF2C0", "Tests, wiring, migrations, logs"),
    ("spike", "D4C5F9", "Investigation or decision-only"),
    ("backend", "0E8A16", "Go API / apps/backend"),
    ("mobile", "1D76DB", "Swift iOS / apps/mobile"),
    ("infrastructure", "FBCA04", "Compose, migrations, Justfile, env"),
    ("security", "B60205", "Auth, wallets, payout proof"),
    ("parent", "BFD4F2", "Umbrella ticket"),
    ("subissue", "C5DEF5", "Child of a parent ticket"),
    ("needs-decision", "E99695", "Blocked on product decision"),
    ("blocked", "B60205", "Cannot implement until unblocked"),
]

TYPE_LABELS = frozenset({"feature", "bug", "refactor", "chore", "spike"})
DOMAIN_LABELS = frozenset({"backend", "mobile", "infrastructure", "security", "frontend"})


@dataclass
class Ticket:
    mid: str  # e.g. M1-T4a
    title: str
    milestone: int  # 0-5
    wave: int
    owns: list[str]
    depends: list[str]
    parent: Optional[str] = None
    subissues: list[str] = field(default_factory=list)
    acceptance: list[str] = field(default_factory=list)
    out_of_scope: list[str] = field(default_factory=list)
    manual: list[str] = field(default_factory=list)
    summary: str = ""
    labels: list[str] = field(default_factory=list)
    needs_decision: bool = False
    goal: str = ""
    implement: list[str] = field(default_factory=list)
    api_contract: str = ""
    fakes: list[str] = field(default_factory=list)
    verify_cmds: list[str] = field(default_factory=list)
    env_vars: list[str] = field(default_factory=list)
    symbols: list[str] = field(default_factory=list)


MILESTONE_DOC = {
    0: "docs/archive/milestones/m0-scaffold.md",
    1: "docs/archive/milestones/m1-auth-wallets.md",
    2: "docs/archive/milestones/m2-deposits.md",
    3: "docs/archive/milestones/m3-jupiter.md",
    4: "docs/archive/milestones/m4-domain.md",
    5: "docs/archive/milestones/m5-mobile.md",
}

REQUIRED_HEADINGS = [
    "Context",
    "Problem",
    "Proposal",
    "Acceptance Criteria",
    "Verification",
    "Dependencies",
]

MILESTONE_CONTEXT = {
    0: "Greenfield monorepo. M0 lands runnable Go health API, SwiftUI shell, local Docker Postgres, and `just` recipes before product work.",
    1: "M1 follows M0 scaffold. Privy auth, member wallets, and group treasuries must exist before deposits (M2).",
    2: "M2 follows M1 wallets. Deposit → sweep → 1:1 share credit must work before Jupiter buys (M3).",
    3: "M3 follows M2 treasury USDC. Backend Jupiter buy/sell and dev stub route precede vote-gated execute in M4.",
    4: "M4 replaces thin M1–M3 stubs with group rules, votes, marked NAV, redeem, and leaderboards before M5 product UI.",
    5: "M5 follows M4 domain APIs. Replaces debug/scaffold mobile screens with demo-ready SwiftUI product shell.",
}


def gh_api(method: str, endpoint: str, data: dict | None = None) -> dict | list:
    cmd = ["gh", "api", "-X", method, endpoint]
    if data is not None:
        cmd.extend(["--input", "-"])
    proc = subprocess.run(
        cmd,
        input=json.dumps(data).encode() if data else None,
        capture_output=True,
        cwd=ROOT,
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.decode() or proc.stdout.decode())
    out = proc.stdout.decode().strip()
    return json.loads(out) if out else {}


def gh_search_issue(prefix: str) -> Optional[int]:
    proc = subprocess.run(
        [
            "gh",
            "issue",
            "list",
            "--repo",
            REPO,
            "--search",
            f'"{prefix}:" in:title',
            "--json",
            "number,title",
            "--limit",
            "5",
        ],
        capture_output=True,
        text=True,
        cwd=ROOT,
    )
    if proc.returncode != 0:
        return None
    items = json.loads(proc.stdout or "[]")
    for it in items:
        if it["title"].startswith(f"{prefix}:"):
            return it["number"]
    return None


def ticket_goal(t: Ticket) -> str:
    if t.goal:
        return t.goal
    if t.summary:
        return t.summary
    return t.title


def link_ticket_id(tid: str, issue_nums: dict[str, int], for_github: bool) -> str:
    if for_github and tid in issue_nums:
        return f"{tid} (#{issue_nums[tid]})"
    return tid


def link_dep(dep: str, issue_nums: dict[str, int], for_github: bool) -> str:
    m = re.match(r"(M\d+-T[\w]+)", dep)
    if m:
        return link_ticket_id(m.group(1), issue_nums, for_github) + dep[len(m.group(1)) :]
    return dep


def default_verify_cmds(t: Ticket) -> list[str]:
    if t.verify_cmds:
        return t.verify_cmds
    cmds: list[str] = []
    lbs = set(t.labels)
    if "backend" in lbs or "schema" in lbs or "tests" in lbs and "mobile" not in lbs:
        cmds.append("just test backend")
    if "mobile" in lbs:
        cmds.append("just test mobile")
    if not cmds:
        if t.milestone == 0:
            cmds = ["just build backend", "just build mobile", "just test backend", "just test mobile"]
        elif "mobile" in t.title.lower():
            cmds = ["just test mobile"]
        else:
            cmds = ["just test backend"]
    if "just wiring" in t.title.lower() or "wire just" in t.title.lower():
        if "just test backend" not in cmds:
            cmds.append("just test backend")
        if "mobile" in lbs and "just test mobile" not in cmds:
            cmds.append("just test mobile")
    return cmds


def default_fakes(t: Ticket) -> list[str]:
    if t.fakes:
        return t.fakes
    m = t.milestone
    fakes: list[str] = []
    if m == 0:
        fakes = ["`newTestHealthHandler()`", "`testHTTPRequest(method, path)`", "`integrationDB(t)`"]
    elif m == 1:
        fakes = ["`fakePrivyClient` (VerifySession, EnsureMemberWallet, EnsureTreasury)", "`integrationApp(t)` + `resetTables(t)`", "`fixtureSessionToken`"]
    elif m == 2:
        fakes = ["`fakePrivyClient` (MemberUSDCBalance, SubmitSweep)", "`fakeSolanaRPC`", "`stubClock`", "`buildObservedSweep(overrides)`", "`integrationApp(t)`"]
    elif m == 3:
        fakes = ["`fakeJupiterClient`", "`fakeXStocksResolver`", "`fakePrivyTreasurySigner`", "`integrationApp(t)`"]
    elif m == 4:
        fakes = ["`fakePythClient`", "`fakeJupiterClient`", "`fakePrivyClient`", "`integrationApp(t)`", "domain factories in `packages/domain` tests"]
    elif m == 5:
        fakes = ["`MockURLProtocol` / stub `MonacoAPIClient` session", "fixture JSON under `apps/mobile/Tests/Fixtures/`"]
    if "mobile" in t.labels and m >= 1:
        fakes.append("`MockURLProtocol` for API client unit tests")
    return fakes


def extract_route(title: str) -> Optional[tuple[str, str]]:
    m = re.search(r"(GET|POST|PUT|PATCH|DELETE)\s+(/v1[^\s,]+)", title, re.I)
    if m:
        return m.group(1).upper(), m.group(2)
    m = re.search(r"`(GET|POST|PUT|PATCH|DELETE)\s+(/v1[^`]+)`", title, re.I)
    if m:
        return m.group(1).upper(), m.group(2)
    return None


def build_api_contract(t: Ticket) -> str:
    if t.api_contract:
        return t.api_contract
    if t.needs_decision:
        return (
            "**No API.** Decision ticket only. Record chosen option in `docs/` when product decides. "
            "Do not ship handler/UI behavior until decision ticket is closed."
        )
    route = extract_route(t.title)
    if route:
        method, path = route
        auth = "Privy bearer token in `Authorization` header (same as M1 session middleware)."
        happy = "200 with JSON body defined in milestone doc for this route."
        failures = "401 missing/invalid auth; 400 invalid body; 403 non-member; 404 unknown resource."
        if "session" in path:
            auth = "Request body `{ \"accessToken\": \"<privy_access_token>\" }`. No session cookie."
            happy = "200 `{ userId, displayName, memberWalletAddress }`"
            failures = "401 invalid/expired Privy token."
        if "deposit" in path.lower():
            happy = "201 pending deposit `{ id, status: \"pending\", amount }` or 200 status with credited `shareUnits` when confirmed."
        if "dev" in path:
            failures += " 404 when `DEV_BUY_ENABLED` is false."
        return (
            f"**Route:** `{method} {path}`\n"
            f"**Auth:** {auth}\n"
            f"**Happy:** {happy}\n"
            f"**Failure codes:** {failures}\n"
            f"**Idempotency:** follow milestone doc keys (`tx_signature`, `execute_request_id`, `privy_user_id`, etc.)."
        )
    if t.parent:
        return f"**No new public route.** Subissue of `{t.parent}`. Internal symbols only in **Owns** paths."
    if t.subissues:
        return f"**Parent orchestration.** Public contract is union of subissue routes/symbols: {', '.join(t.subissues)}."
    if "migration" in t.title.lower():
        return "**No HTTP.** SQL migration only. Apply via migration runner invoked by API boot and `just test backend`."
    if "test" in t.title.lower() or "Wire just" in t.title:
        return "**No new API.** Tests and/or `Justfile` wiring only."
    if "mobile" in t.labels:
        return (
            "**Mobile consumes existing backend JSON only.** "
            "No direct calls to xStocks, Jupiter, Pyth, or Solana RPC from Swift. "
            "Use `MonacoAPIClient` methods matching backend routes in milestone doc."
        )
    return "**No new HTTP surface.** Internal Go/Swift symbols in **Owns** paths only."


def build_implement_steps(t: Ticket) -> list[str]:
    if t.implement:
        return t.implement
    steps: list[str] = []
    n = 1
    owns = ", ".join(f"`{p}`" for p in t.owns) if t.owns else "(see milestone doc)"
    steps.append(f"{n}. Edit **only** **Owns** paths: {owns}. No drive-by refactors.")
    n += 1

    if t.needs_decision:
        steps.append(
            f"{n}. Create/update `docs/` decision note listing options from milestone **Open decisions**. "
            f"Do **not** implement API/UI behavior. Mark ticket blocked until product picks."
        )
        return steps

    if t.parent:
        steps.append(f"{n}. Subissue of `{t.parent}`. Do not add routes or flows owned by parent/sibling subissues.")
        n += 1

    if t.subissues:
        joined = ", ".join(t.subissues)
        steps.append(f"{n}. Parent glue: wire subissues {joined} into `{t.owns[0] if t.owns else 'parent entrypoint'}` after each subissue lands.")
        n += 1

    title_l = t.title.lower()
    route = extract_route(t.title)

    if "migration" in title_l:
        steps.append(f"{n}. Add next sequential SQL file under `supabase/migrations/` with tables/columns from `{MILESTONE_DOC[t.milestone]}`.")
        n += 1
        steps.append(f"{n}. Run migration via existing runner (`apps/backend/internal/postgres/`). Do not hand-apply in prod.")
        n += 1

    if route:
        method, path = route
        handler_hint = t.owns[-1] if t.owns else "apps/backend/internal/httpapi/"
        steps.append(f"{n}. Register `{method} {path}` in route table (`apps/backend/cmd/api/` or `internal/httpapi/`).")
        n += 1
        steps.append(f"{n}. Implement handler in `{handler_hint}` calling `internal/app` method; return JSON status codes from **API / data contract** below.")
        n += 1

    for sym in t.symbols:
        steps.append(f"{n}. Implement symbol `{sym}` with behavior covered by locked tests below.")
        n += 1

    if "privy" in title_l and "login" in title_l:
        steps.append(f"{n}. Wire Privy Swift SDK in `apps/mobile/Features/Auth/`; on success obtain access token for `POST /v1/auth/session`.")
        n += 1

    if "justfile" in " ".join(t.owns).lower() or "wire just" in title_l:
        steps.append(f"{n}. Update root `Justfile` recipes so locked tests run under `just test backend` and/or `just test mobile`. Exit 0 required.")
        n += 1

    if "structured log" in title_l:
        steps.append(f"{n}. Add structured log lines at attempt/confirm/credit (or quote/execute/poll) transitions using existing logger; fields: `group_id`, `user_id`, `deposit_id`/`tx_signature`/`symbol` as applicable.")
        n += 1

    if t.acceptance:
        for test in t.acceptance:
            steps.append(
                f"{n}. Add `{test}` in the `*_test.go` or Swift test target under **Owns**; use AAA comments (`// Arrange`, `// Act`, `// Assert`)."
            )
            n += 1
    elif "mobile" in t.labels and "test" not in title_l:
        steps.append(f"{n}. Build SwiftUI screen in **Owns**; call `MonacoAPIClient` only (no xStocks/Jupiter/Pyth/Solana RPC).")
        n += 1
        steps.append(f"{n}. Compile gate: `just build mobile` and `just test mobile` exit 0.")
        n += 1
    else:
        steps.append(f"{n}. Satisfy milestone gate: `just test backend` and/or `just test mobile` exit 0 with no new failing tests.")
        n += 1

    if t.manual:
        steps.append(f"{n}. **Manual (after automated green):** " + "; ".join(t.manual))
        n += 1

    return steps


def build_context(t: Ticket, issue_nums: dict[str, int], for_github: bool) -> str:
    doc_path = MILESTONE_DOC[t.milestone]
    ms_name = MILESTONES[t.milestone][0]
    lines = [
        MILESTONE_CONTEXT[t.milestone],
        "",
        f"Ticket `{t.mid}` is wave **{t.wave}** in `{doc_path}` under milestone **{ms_name}**.",
    ]
    if t.parent:
        lines.append(f"Subissue of {link_ticket_id(t.parent, issue_nums, for_github)}.")
    elif t.subissues:
        subs = ", ".join(link_ticket_id(s, issue_nums, for_github) for s in t.subissues)
        lines.append(f"Parent ticket; subissues: {subs}.")
    if t.depends:
        deps = ", ".join(link_dep(d, issue_nums, for_github) for d in t.depends)
        lines.append(f"Blocked until dependencies land: {deps}.")
    return "\n".join(lines)


def build_problem(t: Ticket) -> str:
    if t.needs_decision:
        return (
            f"Product policy for `{t.title}` is not locked. Milestone doc lists options; "
            f"no `docs/` decision note or implementation exists yet."
        )
    owns = ", ".join(f"`{p}`" for p in t.owns) if t.owns else "named paths in milestone doc"
    route = extract_route(t.title)
    if route:
        method, path = route
        return f"`{method} {path}` and handler code under {owns} do not exist (or fail locked tests)."
    if "migration" in t.title.lower():
        return f"SQL tables/columns for `{t.mid}` are missing from `supabase/migrations/`; migration runner cannot apply them."
    if "test" in t.title.lower() or "wire just" in t.title.lower():
        return f"Locked test names for `{t.mid}` are absent or not wired into `just test` recipes; {owns} incomplete."
    if "mobile" in t.labels:
        return f"SwiftUI flow in {owns} is missing or still scaffold; product/API wiring for `{t.title}` not shipped."
    if t.parent:
        return f"Subissue `{t.mid}` of `{t.parent}`: symbols under {owns} not implemented; parent cannot close."
    if t.subissues:
        return f"Parent `{t.mid}` orchestration in {owns} missing; subissues {', '.join(t.subissues)} not wired together."
    return f"Behavior for `{t.title}` is not implemented under {owns}."


def build_proposal(t: Ticket) -> str:
    steps = build_implement_steps(t)
    numbered = "\n".join(steps)
    api = build_api_contract(t)
    env_block = ""
    if t.env_vars:
        env_block = "\n**Env vars:** " + ", ".join(f"`{e}`" for e in t.env_vars)
    fakes = default_fakes(t)
    fake_block = ""
    if fakes:
        fake_block = "\n**Test fakes (locked names):**\n" + "\n".join(f"- {f}" for f in fakes)
    oos = "\n".join(f"- {o}" for o in t.out_of_scope) if t.out_of_scope else "- (none listed)"
    owns_scope = "\n".join(f"- `{p}`" for p in t.owns) if t.owns else "- See milestone doc **Owns**"
    route = extract_route(t.title)
    route_line = ""
    if route:
        route_line = f"\n- Route: `{route[0]} {route[1]}`"
    return (
        f"Implement in order. No drive-by refactors. Lock choices below; do not invent product policy.\n\n"
        f"{numbered}\n\n"
        f"**API / data contract**\n{api}{env_block}{fake_block}\n\n"
        f"### Scope\n{owns_scope}{route_line}\n\n"
        f"**Out of scope (do not touch)**\n{oos}"
    )


def build_acceptance_criteria(t: Ticket) -> list[str]:
    items: list[str] = []
    if t.acceptance:
        for name in t.acceptance:
            items.append(f"Test `{name}` exists, uses AAA comments, and passes.")
    owns = ", ".join(f"`{p}`" for p in t.owns) if t.owns else "all **Scope** paths"
    items.append(f"Files and symbols named in **Proposal** exist under {owns}.")
    for cmd in default_verify_cmds(t):
        items.append(f"`{cmd}` exits 0 with no new failures.")
    items.append("**Out of scope** paths untouched; no unrelated refactors.")
    if t.subissues:
        items.append(f"All subissues ({', '.join(t.subissues)}) closed before parent closes.")
    # dedupe while preserving order
    seen: set[str] = set()
    out: list[str] = []
    for item in items:
        if item not in seen:
            seen.add(item)
            out.append(item)
    return out[:8] if len(out) > 8 else out


def build_verification(t: Ticket, issue_nums: dict[str, int], for_github: bool) -> str:
    baseline = "\n".join(f"- `{c}`" for c in default_verify_cmds(t))
    manual = ""
    if t.manual:
        manual = "\n\n**Manual (only for this ticket):**\n" + "\n".join(f"- {m}" for m in t.manual)
    done_items = [
        "Every **Acceptance Criteria** checkbox is satisfied.",
        *build_acceptance_criteria(t),
    ]
    if t.subissues:
        subs = ", ".join(link_ticket_id(s, issue_nums, for_github) for s in t.subissues)
        done_items.append(f"Subissues closed: {subs}.")
    done_items.append("No drive-by edits outside **Scope**; **Out of scope** untouched.")
    done = "\n".join(f"- [ ] {item}" for item in done_items)
    return (
        f"**Baseline (run before marking done):**\n{baseline}{manual}\n\n"
        f"**Done when (zero wiggle room):**\n{done}"
    )


def build_dependencies(t: Ticket, issue_nums: dict[str, int], for_github: bool) -> str:
    lines: list[str] = []
    if t.depends:
        for d in t.depends:
            lines.append(f"- {link_dep(d, issue_nums, for_github)} — **do not start while open**")
    else:
        lines.append("- None")
    lines.append(f"- **Wave:** {t.wave}")
    if t.parent:
        lines.append(f"- **Parent:** {link_ticket_id(t.parent, issue_nums, for_github)}")
    elif t.subissues:
        for s in t.subissues:
            lines.append(f"- **Subissue:** {link_ticket_id(s, issue_nums, for_github)}")
    return "\n".join(lines)


def section_heading(label: str, for_github: bool) -> str:
    return f"## {label}" if for_github else f"**{label}**"


def format_ticket_body(t: Ticket, issue_nums: dict[str, int], for_github: bool) -> str:
    ac_lines = "\n".join(f"- [ ] {item}" for item in build_acceptance_criteria(t))
    parts = [
        section_heading("Context", for_github),
        build_context(t, issue_nums, for_github),
        "",
        section_heading("Problem", for_github),
        build_problem(t),
        "",
        section_heading("Proposal", for_github),
        build_proposal(t),
        "",
        section_heading("Acceptance Criteria", for_github),
        ac_lines,
        "",
        section_heading("Verification", for_github),
        build_verification(t, issue_nums, for_github),
        "",
        section_heading("Dependencies", for_github),
        build_dependencies(t, issue_nums, for_github),
    ]
    return "\n".join(parts)


def ticket_detail_md(t: Ticket) -> str:
    return f"#### {t.mid}: {t.title}\n\n" + format_ticket_body(t, {}, for_github=False)


def issue_body(t: Ticket, issue_nums: dict[str, int]) -> str:
    return format_ticket_body(t, issue_nums, for_github=True)


def has_section(body: str, label: str) -> bool:
    return f"## {label}" in body or f"**{label}**" in body


def validate_body(body: str) -> tuple[bool, list[str]]:
    errors: list[str] = []
    if not body.strip():
        return False, ["empty body"]
    for h in REQUIRED_HEADINGS:
        if not has_section(body, h):
            errors.append(f"missing section: {h}")
    text_no_headings = re.sub(r"^#+\s.*$|^\*\*[^*]+\*\*$", "", body, flags=re.M).strip()
    if len(text_no_headings) < 120:
        errors.append(f"body too short ({len(text_no_headings)} chars excl headings; need >=120)")
    if "## Acceptance Criteria" in body:
        ac_section = body.split("## Acceptance Criteria", 1)[1].split("## Verification", 1)[0]
    elif "**Acceptance Criteria**" in body:
        ac_section = body.split("**Acceptance Criteria**", 1)[1].split("**Verification**", 1)[0]
    else:
        ac_section = ""
    ac_bullets = re.findall(r"^- \[ \] .+", ac_section, flags=re.M)
    real_ac = [
        b for b in ac_bullets
        if "placeholder" not in b.lower() and "todo" not in b.lower() and len(b) > 10
    ]
    if len(real_ac) < 2:
        errors.append(f"need >=2 AC bullets, found {len(real_ac)}")
    return len(errors) == 0, errors


def is_thin_body(body: str) -> bool:
    ok, _ = validate_body(body)
    return not ok


def ticket_type_label(t: Ticket) -> str:
    if t.needs_decision:
        return "spike"
    title_l = t.title.lower()
    if any(
        k in title_l
        for k in [
            "open decision",
            "wire just",
            "unit test",
            "integration test",
            "handler test",
            "swift unit test",
            "structured log",
            "mark",
            "deletion",
        ]
    ):
        return "chore"
    if "migration" in title_l or "justfile" in title_l or "docker-compose" in title_l:
        return "chore"
    if "refactor" in title_l or "replace thin" in title_l:
        return "refactor"
    return "feature"


def ticket_domain_labels(t: Ticket) -> list[str]:
    domains: list[str] = []
    lbs = set(t.labels)
    owns_l = " ".join(t.owns).lower()
    if "mobile" in lbs:
        domains.append("mobile")
    if "backend" in lbs or ("tests" in lbs and "mobile" not in lbs):
        domains.append("backend")
    if "schema" in lbs or any(x in owns_l for x in ["supabase/migrations", "docker-compose", "justfile", ".env", "scripts/"]):
        if "infrastructure" not in domains:
            domains.append("infrastructure")
    title_l = t.title.lower()
    if any(k in title_l for k in ["auth", "privy", "session", "payout proof", "relayer"]):
        if "security" not in domains and len(domains) < 2:
            domains.append("security")
    if not domains:
        domains.append("mobile" if t.milestone == 5 else "backend")
    return domains[:2]


def default_labels(t: Ticket) -> list[str]:
    lbs = [f"milestone-m{t.milestone}", ticket_type_label(t)]
    lbs.extend(ticket_domain_labels(t))
    if t.parent:
        lbs.append("subissue")
    if t.subissues:
        lbs.append("parent")
    if t.needs_decision:
        lbs.append("needs-decision")
        lbs.append("blocked")
    return sorted(set(lbs))


def inject_ticket_details(filename: str, tickets: list[Ticket]) -> None:
    path = DOCS / filename
    text = path.read_text()
    section = "## Ticket details\n\n" + "\n".join(ticket_detail_md(t) for t in tickets) + "\n"
    if "## Ticket details" in text:
        text = re.sub(r"## Ticket details\n.*?(?=\n## )", section, text, flags=re.S)
    else:
        text = text.replace("\n## Automated verification\n", f"\n{section}\n## Automated verification\n")
    path.write_text(text)


# --- Ticket definitions -----------------------------------------------------

def all_tickets() -> list[Ticket]:
    """Full ticket catalog keyed to milestone docs."""
    tickets: list[Ticket] = []

    # M0
    m0_oos = ["Privy auth", "wallet provision", "domain tables beyond migration plumbing", "Jupiter", "deposits", "group rules", "hosted Supabase for local dev"]
    tickets += [
        Ticket("M0-T1", "Create monorepo directories under apps/ and packages/", 0, 1,
               ["apps/backend/", "apps/mobile/", "packages/domain/", "docs/", "scripts/"], [],
               summary="Land empty app and package directories so later waves can scaffold in parallel.",
               out_of_scope=m0_oos),
        Ticket("M0-T2", "Add root Justfile with build, test, and run for backend and mobile only", 0, 4,
               ["Justfile"], ["M0-T1"], labels=["backend", "mobile"],
               acceptance=["TestJustTestBackend_exitsZeroOnCleanClone"],
               out_of_scope=["Product recipes beyond build/test/run", "just db", "just check"]),
        Ticket("M0-T3", "Scaffold the Go API module with GET /health", 0, 2,
               ["apps/backend/cmd/api/", "apps/backend/internal/httpapi/"], ["M0-T1"],
               labels=["backend"],
               acceptance=["TestHealthHandler_returns200AndOkBody", "TestHealthHandler_setsContentTypeJson"],
               out_of_scope=["Product routes", "Postgres in handler"]),
        Ticket("M0-T4", "Scaffold the SwiftUI iOS 18 app target in apps/mobile", 0, 2,
               ["apps/mobile/Monaco.xcodeproj", "apps/mobile/"], ["M0-T1"], labels=["mobile"],
               out_of_scope=["Privy", "API client beyond shell"]),
        Ticket("M0-T5", "Add docker-compose.yml with a local Postgres service for dev and test", 0, 2,
               ["docker-compose.yml"], ["M0-T1"], labels=["schema"],
               out_of_scope=["Hosted Supabase", "Application schema tables"]),
        Ticket("M0-T6", "Add supabase/migrations/ and a migration runner the API and just test backend invoke", 0, 3,
               ["supabase/migrations/", "apps/backend/internal/postgres/", "scripts/"], ["M0-T3", "M0-T5"],
               labels=["backend", "schema"],
               acceptance=["TestMigrationRunner_appliesInitialMigrationOnEmptyDb", "TestMigrationRunner_isIdempotentOnSecondRun"],
               out_of_scope=["M1+ domain tables"]),
        Ticket("M0-T7", "Add .env.example with localhost DATABASE_URL and Privy placeholders. Gitignore .env", 0, 2,
               [".env.example", ".gitignore", "scripts/"], ["M0-T1"], labels=["backend"],
               acceptance=["TestDatabaseURLGuard_rejectsHostedSupabaseUrl", "TestDatabaseURLGuard_acceptsLocalhostComposeUrl"],
               out_of_scope=["Real Privy secrets", "Production env"]),
        Ticket("M0-T8", "Add a Go unit test for the health handler", 0, 4,
               ["apps/backend/internal/httpapi/*_test.go"], ["M0-T3"], labels=["backend", "tests"],
               acceptance=["TestHealthHandler_returns200AndOkBody", "TestHealthHandler_setsContentTypeJson"],
               out_of_scope=["Integration tests requiring Postgres"]),
        Ticket("M0-T9", "Wire just test backend and just test mobile to pass on a clean clone", 0, 4,
               ["Justfile", "scripts/"], ["M0-T2", "M0-T4", "M0-T6", "M0-T8"], labels=["backend", "mobile", "tests"],
               acceptance=["TestJustTestBackend_exitsZeroOnCleanClone"],
               out_of_scope=["CI deploy", "TestFlight"]),
    ]

    m1_oos = ["USDC deposits", "sweeps", "share units", "NAV", "votes", "Jupiter", "P&L boards"]
    tickets += [
        Ticket("M1-T1", "Add the migration for users, member_wallets, groups, and treasuries", 1, 1,
               ["supabase/migrations/"], [],
               labels=["schema"],
               summary="Create M1 auth and wallet tables in local Postgres migrations.",
               out_of_scope=m1_oos + ["Deposit or vote tables"]),
        Ticket("M1-T2", "Add the Go config loader for Privy, localhost Postgres, and relayer credentials", 1, 1,
               ["apps/backend/internal/config/"], [],
               labels=["backend"],
               out_of_scope=["Privy API calls", "HTTP handlers"]),
        Ticket("M1-T3", "Add Privy server helpers that create Solana wallets", 1, 1,
               ["apps/backend/internal/privy/"], ["M1-T2"], labels=["backend"],
               out_of_scope=["HTTP handlers", "Postgres store methods", "Session orchestration in internal/app"]),
        Ticket("M1-T4", "Implement POST /v1/auth/session to verify Privy token, upsert user, provision member wallet idempotently", 1, 2,
               ["apps/backend/internal/app/session.go", "apps/backend/internal/httpapi/auth.go"], ["M1-T1", "M1-T3"],
               labels=["backend", "parent"],
               subissues=["M1-T4a", "M1-T4b", "M1-T4c"],
               acceptance=["TestPOST_auth_session_happyPath_returns200AndSetsSession"],
               out_of_scope=["Group create", "Deposits"]),
        Ticket("M1-T4a", "Verify Privy access token via Privy client", 1, 2,
               ["apps/backend/internal/privy/verify.go"], ["M1-T3"], parent="M1-T4", labels=["backend"],
               acceptance=["TestSession_validPrivyToken_upsertsUserAndReturnsSession", "TestSession_invalidPrivyToken_returns401", "TestSession_expiredPrivyToken_returns401"],
               out_of_scope=["Postgres upsert", "Wallet provision"]),
        Ticket("M1-T4b", "Upsert users row on privy_user_id", 1, 2,
               ["apps/backend/internal/postgres/users.go"], ["M1-T1", "M1-T4a"], parent="M1-T4", labels=["backend", "schema"],
               acceptance=["TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow"],
               out_of_scope=["Wallet provision", "HTTP routing"]),
        Ticket("M1-T4c", "EnsureMemberWallet idempotent provision and persist member_wallets", 1, 2,
               ["apps/backend/internal/app/session.go", "apps/backend/internal/postgres/member_wallets.go"], ["M1-T4b", "M1-T3"], parent="M1-T4", labels=["backend"],
               acceptance=["TestEnsureMemberWallet_firstSession_createsMemberWalletRow", "TestEnsureMemberWallet_repeatSession_reusesSameWallet"],
               out_of_scope=["Group treasury wallets"]),
        Ticket("M1-T5", "Implement GET /v1/me returning user id, display name, and member wallet Solana address", 1, 3,
               ["apps/backend/internal/httpapi/me.go", "apps/backend/internal/app/session.go"], ["M1-T4"], labels=["backend"],
               acceptance=["TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress", "TestGET_me_missingAuth_returns401", "TestGET_me_unknownUser_returns404"],
               out_of_scope=["Group endpoints"]),
        Ticket("M1-T6", "Implement POST /v1/groups to insert group row and provision treasury server wallet", 1, 3,
               ["apps/backend/internal/app/group.go", "apps/backend/internal/httpapi/groups.go"], ["M1-T4"],
               labels=["backend", "parent"],
               subissues=["M1-T6a", "M1-T6b"],
               acceptance=["TestCreateGroup_insertsGroupAndTreasuryRows"],
               out_of_scope=["Join policy", "Votes", "Deposits"]),
        Ticket("M1-T6a", "Insert thin groups row", 1, 3,
               ["apps/backend/internal/postgres/groups.go", "apps/backend/internal/app/group.go"], ["M1-T4"], parent="M1-T6", labels=["backend", "schema"],
               acceptance=["TestCreateGroup_insertsGroupAndTreasuryRows"],
               out_of_scope=["Treasury Privy provision"]),
        Ticket("M1-T6b", "Privy client provisions group treasury and persist treasuries", 1, 3,
               ["apps/backend/internal/privy/treasury.go", "apps/backend/internal/postgres/treasuries.go"], ["M1-T6a", "M1-T3"], parent="M1-T6", labels=["backend"],
               acceptance=["TestCreateGroup_provisionsTreasuryViaPrivyClient"],
               out_of_scope=["Join policy columns"]),
        Ticket("M1-T7", "Implement GET /v1/groups/{id} returning group name and treasury Solana address", 1, 3,
               ["apps/backend/internal/httpapi/groups.go"], ["M1-T6"], labels=["backend"],
               acceptance=["TestGET_group_byId_returnsNameAndTreasuryAddress", "TestGET_group_nonMemberOrUnknown_returns404", "TestGET_group_missingAuth_returns401"],
               out_of_scope=["Member list", "NAV"]),
        Ticket("M1-T8", "Register relayer fee payer config and fail API startup if key material does not load", 1, 2,
               ["apps/backend/internal/config/relayer.go", "apps/backend/cmd/api/main.go"], ["M1-T2"], labels=["backend"],
               acceptance=["TestAPIServer_missingRelayerKey_failsStartup", "TestAPIServer_validRelayerKey_startsSuccessfully"],
               out_of_scope=["Signing sweeps until M2", "Funding UX"]),
        Ticket("M1-T9", "Add the Privy SMS login path", 1, 1,
               ["apps/mobile/Features/Auth/"], [], labels=["mobile"],
               out_of_scope=["Backend session handler", "Email/password UI"]),
        Ticket("M1-T10", "Enable Privy email and password login in dashboard and app config", 1, 1,
               ["apps/mobile/Features/Auth/", ".env.example"], [], labels=["mobile"],
               manual=["Enable email+password in Privy dashboard"],
               out_of_scope=["Backend changes"]),
        Ticket("M1-T11", "Add email and password fields on the M1 login screen", 1, 2,
               ["apps/mobile/Features/Auth/LoginView.swift"], ["M1-T9", "M1-T10"], labels=["mobile"],
               out_of_scope=["Product launch shell (M5)"]),
        Ticket("M1-T12", "Add post-login screen calling GET /v1/me and showing member wallet address", 1, 4,
               ["apps/mobile/Features/Auth/", "apps/mobile/API/MonacoAPIClient.swift"], ["M1-T5", "M1-T11"], labels=["mobile"],
               manual=["Confirm Solana address on slim sim"],
               out_of_scope=["Hiding addresses from main flow (M5)"]),
        Ticket("M1-T13", "Add create-group screen calling POST /v1/groups and showing treasury address", 1, 4,
               ["apps/mobile/Features/Groups/"], ["M1-T6", "M1-T7", "M1-T12"], labels=["mobile"],
               out_of_scope=["Join policy UI (M5)"]),
        Ticket("M1-T14", "Add Go handler tests for auth session and group create happy paths", 1, 5,
               ["apps/backend/internal/httpapi/*_test.go"], ["M1-T4", "M1-T6"], labels=["backend", "tests"],
               acceptance=["TestPOST_auth_session_happyPath_returns200AndSetsSession", "TestPOST_groups_missingAuth_returns401"],
               out_of_scope=["Integration Postgres row asserts (M1-T15)"]),
        Ticket("M1-T15", "Add Go integration test asserting Postgres rows after member and treasury wallet provision", 1, 5,
               ["apps/backend/internal/postgres/*_integration_test.go"], ["M1-T4", "M1-T6"], labels=["backend", "tests", "schema"],
               acceptance=["TestEnsureMemberWallet_firstSession_createsMemberWalletRow", "TestCreateGroup_insertsGroupAndTreasuryRows"],
               out_of_scope=["Mobile tests"]),
        Ticket("M1-T16", "Wire just test backend to cover auth and group Postgres integration", 1, 5,
               ["Justfile"], ["M1-T14", "M1-T15"], labels=["backend", "tests"],
               out_of_scope=["just test mobile wiring beyond M0"]),
        Ticket("M1-T17", "Add Swift unit test that API client sends Privy auth header after login", 1, 5,
               ["apps/mobile/Tests/"], ["M1-T11"], labels=["mobile", "tests"],
               acceptance=["testAPIClient_afterLogin_sendsAuthorizationHeader", "testMeDTO_decodesFixtureJSON"],
               out_of_scope=["UI snapshot tests"]),
    ]

    m2_oos = ["Jupiter swaps", "buy proposals", "votes", "redeem payout", "leaderboards", "Pyth marks"]
    tickets += [
        Ticket("M2-T1", "Add migration for deposits, positions, and withdrawals schema", 2, 1,
               ["supabase/migrations/"], [], labels=["schema"],
               summary="Add deposit, position, and withdrawal tables for sweep credit.",
               out_of_scope=m2_oos),
        Ticket("M2-T2", "Define domain types for deposit, position, and sweep credit at API boundary", 2, 1,
               ["apps/backend/internal/app/deposit.go"], [], labels=["backend"],
               out_of_scope=["Worker loop", "HTTP handlers"]),
        Ticket("M2-T3", "Implement POST deposit keyed by user, group, and amount", 2, 2,
               ["apps/backend/internal/httpapi/deposits.go", "apps/backend/internal/app/deposit.go"], ["M2-T1", "M2-T2"], labels=["backend"],
               acceptance=["TestPOST_deposit_createsPendingDepositRow", "TestPOST_deposit_missingAuth_returns401", "TestPOST_deposit_nonMember_returns403", "TestPOST_deposit_zeroOrNegativeAmount_returns400"],
               out_of_scope=["Sweep poller", "Share credit"]),
        Ticket("M2-T4", "Implement member-wallet USDC balance poller using Privy and mainnet RPC", 2, 2,
               ["apps/backend/internal/worker/sweep_poller.go"], ["M2-T1"], labels=["backend"],
               acceptance=["TestSweepPoller_memberBalanceCoversIntent_triggersSubmitSweep", "TestSweepPoller_memberBalanceBelowIntent_doesNotSweep"],
               out_of_scope=["Share credit", "Jupiter"]),
        Ticket("M2-T5", "Implement server-signed USDC sweep from member wallet to group treasury via Privy", 2, 3,
               ["apps/backend/internal/privy/sweep.go", "apps/backend/internal/worker/"], ["M2-T4"], labels=["backend"],
               acceptance=["TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses"],
               out_of_scope=["Position credit"]),
        Ticket("M2-T6", "Wire app relayer as SOL fee payer on sweep transactions", 2, 2,
               ["apps/backend/internal/privy/sweep.go"], ["M1-T8"], labels=["backend"],
               acceptance=["TestSubmitSweep_includesRelayerAsFeePayer"],
               out_of_scope=["Jupiter fee payer"]),
        Ticket("M2-T8", "Credit position share units on confirmed treasury credit only, idempotent on deposit signature", 2, 3,
               ["apps/backend/internal/app/deposit.go", "apps/backend/internal/postgres/deposits.go", "apps/backend/internal/postgres/positions.go"],
               ["M2-T5", "M2-T6"], labels=["backend", "parent"],
               subissues=["M2-T7", "M2-T9", "M2-T10"],
               acceptance=["TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually"],
               out_of_scope=["Marked NAV minting (M4)"]),
        Ticket("M2-T7", "Confirm sweep on-chain and set tx_signature on deposit row with unique upsert", 2, 3,
               ["apps/backend/internal/postgres/deposits.go", "apps/backend/internal/app/deposit.go"], ["M2-T5"], parent="M2-T8", labels=["backend", "schema"],
               acceptance=["TestObserveSweep_confirmedTreasuryArrival_setsDepositStatusConfirmed"],
               out_of_scope=["Share unit increment logic (M2-T9)"]),
        Ticket("M2-T9", "On confirmed sweep increment amount_deposited and share_units by swept USDC (1:1 in M2)", 2, 3,
               ["apps/backend/internal/postgres/positions.go", "apps/backend/internal/app/deposit.go"], ["M2-T7"], parent="M2-T8", labels=["backend"],
               acceptance=["TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually", "TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited"],
               out_of_scope=["Marked pot ratio (M4)"]),
        Ticket("M2-T10", "Reject share credit when USDC sits only in the member wallet", 2, 3,
               ["apps/backend/internal/app/deposit.go"], ["M2-T8"], parent="M2-T8", labels=["backend"],
               acceptance=["TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition"],
               out_of_scope=["Jupiter", "Votes"]),
        Ticket("M2-T11", "Add GET endpoints for deposit status, member share units, and treasury USDC balance", 2, 4,
               ["apps/backend/internal/httpapi/deposits.go"], ["M2-T8"], labels=["backend"],
               acceptance=["TestGET_depositStatus_returnsCreditedShareUnitsWhenConfirmed", "TestGET_memberShareUnits_reflectsPositionAfterSweep", "TestGET_treasuryUsdcBalance_returnsPostSweepAmount"],
               out_of_scope=["xStock balances"]),
        Ticket("M2-T12", "Add structured logs for sweep attempt, confirmation, and share credit", 2, 1,
               ["apps/backend/internal/worker/", "apps/backend/internal/app/deposit.go"], [], labels=["backend"],
               out_of_scope=["Metrics stack", "Privy webhooks"]),
        Ticket("M2-T13", "Add thin mobile deposit screen to create deposit and poll status", 2, 4,
               ["apps/mobile/Features/Deposit/"], ["M2-T3", "M2-T11"], labels=["mobile"],
               manual=["Fund member wallet with mainnet USDC", "Confirm sweep in explorer"],
               out_of_scope=["Product deposit copy (M5)"]),
        Ticket("M2-T14", "Add unit tests that credited share_units and amount_deposited equal swept USDC", 2, 5,
               ["apps/backend/internal/app/deposit_test.go"], ["M2-T8"], labels=["backend", "tests"],
               acceptance=["TestObserveSweep_confirmedTreasuryArrival_incrementsShareUnitsAndAmountDepositedEqually", "TestProperty_sweptUsdcEqualsShareUnitsAndAmountDeposited"],
               out_of_scope=["Integration duplicate-signature test (M2-T16)"]),
        Ticket("M2-T15", "Add unit tests that multiple deposits sum share_units and amount_deposited correctly", 2, 5,
               ["apps/backend/internal/app/deposit_test.go"], ["M2-T8"], labels=["backend", "tests"],
               acceptance=["TestObserveSweep_multipleDeposits_sumsShareUnitsAndAmountDeposited", "TestProperty_multipleDepositsPreserveOneToOneInvariant"],
               out_of_scope=["Mobile tests"]),
        Ticket("M2-T16", "Add integration test that duplicate sweep signature does not double-credit shares", 2, 5,
               ["apps/backend/internal/app/deposit_integration_test.go"], ["M2-T8"], labels=["backend", "tests"],
               acceptance=["TestObserveSweep_duplicateSignature_doesNotDoubleCredit", "TestObserveSweep_concurrentDuplicateSignature_creditsOnceOnly", "TestProperty_observeSweepIdempotent"],
               out_of_scope=["Jupiter idempotency"]),
        Ticket("M2-T17", "Add integration test that member-wallet-only USDC creates zero position share units", 2, 5,
               ["apps/backend/internal/app/deposit_integration_test.go"], ["M2-T10"], labels=["backend", "tests"],
               acceptance=["TestObserveSweep_memberWalletOnlyBalance_doesNotCreditPosition"],
               out_of_scope=["Treasury-only USDC without deposit row"]),
        Ticket("M2-T18", "Wire just test backend to cover deposit and sweep tests against local Docker Postgres", 2, 5,
               ["Justfile"], ["M2-T14", "M2-T15", "M2-T16", "M2-T17"], labels=["backend", "tests"],
               out_of_scope=["just test mobile"]),
    ]

    m3_oos = ["Vote lifecycle", "proposal UI", "member voting", "redeem payout", "leaderboards", "Pyth marks"]
    tickets += [
        Ticket("M3-T1", "Add migration for transactions if not created in M2", 3, 1,
               ["supabase/migrations/"], [], labels=["schema"],
               summary="Persist Jupiter swap transactions with signature idempotency.",
               out_of_scope=m3_oos),
        Ticket("M3-T2", "Implement xStocks catalog client that resolves the Solana mint", 3, 1,
               ["apps/backend/internal/xstocks/"], [], labels=["backend"],
               out_of_scope=["Jupiter client", "HTTP handlers"]),
        Ticket("M3-T3", "Implement Jupiter v2 quote client for USDC inputMint and xStock outputMint", 3, 2,
               ["apps/backend/internal/jupiter/quote.go"], ["M3-T2"], labels=["backend"],
               out_of_scope=["Execute/poll", "Mobile"]),
        Ticket("M3-T4", "Refuse quote when Jupiter returns no route", 3, 2,
               ["apps/backend/internal/jupiter/quote.go", "apps/backend/internal/app/start_buy.go"], ["M3-T3"], labels=["backend"],
               acceptance=["TestJupiterQuoteBuy_noRoute_returnsRoutableFalse"],
               out_of_scope=["Vote-gated execute"]),
        Ticket("M3-T5", "Implement buy transaction builder that signs treasury via Privy and POSTs Jupiter execute", 3, 3,
               ["apps/backend/internal/app/swap.go", "apps/backend/internal/jupiter/execute.go"], ["M3-T3"],
               labels=["backend", "parent"], subissues=["M3-T6", "M3-T7", "M3-T8"],
               out_of_scope=["Vote gate", "Mobile Jupiter calls"]),
        Ticket("M3-T6", "Poll Jupiter execute until Success with code 0 or terminal failure, then persist transaction row", 3, 3,
               ["apps/backend/internal/jupiter/poll.go", "apps/backend/internal/postgres/transactions.go"], ["M3-T5"], parent="M3-T5", labels=["backend"],
               acceptance=["TestJupiterExecute_successRequiresStatusSuccessAndCodeZero", "TestJupiterPoll_transitionsFromPendingToSuccess", "TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow"],
               out_of_scope=["Sell path"]),
        Ticket("M3-T7", "Make buy execute idempotent on transaction signature or execute request id", 3, 3,
               ["apps/backend/internal/postgres/transactions.go"], ["M3-T6"], parent="M3-T5", labels=["backend", "schema"],
               acceptance=["TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis", "TestDevExecuteBuy_duplicateExecuteRequestId_isIdempotent", "TestProperty_confirmedBuyExactlyOneTransactionRowPerSignature"],
               out_of_scope=["Sell idempotency (M3-T10)"]),
        Ticket("M3-T8", "Persist cost basis columns from fill price and output amount on confirmed buy transaction", 3, 3,
               ["apps/backend/internal/postgres/transactions.go"], ["M3-T6"], parent="M3-T5", labels=["backend"],
               acceptance=["TestDevExecuteBuy_happyPath_insertsOneTransactionRowWithCostBasis", "TestProperty_costBasisAmountMatchesFillOutput"],
               out_of_scope=["Live Pyth marks"]),
        Ticket("M3-T9", "Implement sell quote and execute from treasury xStock mint to USDC outputMint", 3, 3,
               ["apps/backend/internal/jupiter/sell.go", "apps/backend/internal/app/swap.go"], ["M3-T3"],
               labels=["backend", "parent"], subissues=["M3-T10"],
               out_of_scope=["Redeem payout to member wallet"]),
        Ticket("M3-T10", "Poll sell execute confirmation and persist USDC proceeds with idempotent signature upsert", 3, 3,
               ["apps/backend/internal/jupiter/poll.go", "apps/backend/internal/postgres/transactions.go"], ["M3-T9"], parent="M3-T9", labels=["backend"],
               acceptance=["TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc", "TestSellToUSDC_duplicateSignature_doesNotDoubleApply"],
               out_of_scope=["Member payout"]),
        Ticket("M3-T11", "Add temporary dev-only POST execute-buy stub with group id and symbol, no vote gate", 3, 4,
               ["apps/backend/internal/httpapi/dev_buy.go", "apps/backend/internal/app/start_buy.go"], ["M3-T5"], labels=["backend"],
               acceptance=["TestStartBuy_devRouteAllowed_whenDevFlagSet", "TestPOST_devBuy_withoutDevFlag_returns404"],
               out_of_scope=["Production vote gate"]),
        Ticket("M3-T12", "Mark dev stub for deletion when M4 vote pass becomes execute hook", 3, 4,
               ["apps/backend/internal/httpapi/dev_buy.go"], ["M3-T11"], labels=["backend"],
               out_of_scope=["Implementing M4 delete (tracked in M4-T19)"]),
        Ticket("M3-T13", "Add GET endpoints for transaction status, treasury token balances, and cost basis by symbol", 3, 4,
               ["apps/backend/internal/httpapi/transactions.go"], ["M3-T5", "M3-T9"], labels=["backend"],
               acceptance=["TestGET_transactionStatus_returnsConfirmedFill", "TestGET_treasuryTokenBalances_reflectsPostBuyHoldings", "TestGET_costBasisBySymbol_returnsFillDerivedBasis"],
               out_of_scope=["Mobile catalog search"]),
        Ticket("M3-T14", "Add structured logs for quote, execute submit, poll transitions, and refusal", 3, 1,
               ["apps/backend/internal/jupiter/", "apps/backend/internal/app/"], [], labels=["backend"],
               out_of_scope=["WebSocket client"]),
        Ticket("M3-T15", "Add thin mobile debug control calling POST /v1/dev/groups/{id}/buy for stub AAPLx buy", 3, 4,
               ["apps/mobile/Features/Debug/"], ["M3-T11"], labels=["mobile"],
               out_of_scope=["Swift calls to Jupiter or xStocks"]),
        Ticket("M3-T16", "Add unit tests for xStocks mint parsing from sample AAPLx and TSLAx payloads", 3, 2,
               ["apps/backend/internal/xstocks/*_test.go"], ["M3-T2"], labels=["backend", "tests"],
               acceptance=["TestXStocksResolver_AAPLx_returnsSolanaMintFromDeployments", "TestXStocksResolver_TSLAx_returnsSolanaMintFromDeployments", "TestXStocksResolver_missingSolanaDeployment_returnsError"],
               out_of_scope=["Mainnet HTTP"]),
        Ticket("M3-T17", "Add unit tests for Jupiter quote parsing, including no-route refusal", 3, 5,
               ["apps/backend/internal/jupiter/*_test.go"], ["M3-T4"], labels=["backend", "tests"],
               acceptance=["TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint", "TestJupiterQuoteBuy_noRoute_returnsRoutableFalse"],
               out_of_scope=["Execute poll tests"]),
        Ticket("M3-T18", "Add unit tests that execute success requires status Success and code 0", 3, 5,
               ["apps/backend/internal/jupiter/*_test.go"], ["M3-T6"], labels=["backend", "tests"],
               acceptance=["TestJupiterExecute_successRequiresStatusSuccessAndCodeZero"],
               out_of_scope=["Integration buy path"]),
        Ticket("M3-T19", "Add unit tests for execute failure and other terminal non-success states", 3, 5,
               ["apps/backend/internal/jupiter/*_test.go"], ["M3-T6"], labels=["backend", "tests"],
               acceptance=["TestJupiterExecute_terminalFailure_doesNotPersistConfirmedRow", "TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed"],
               out_of_scope=["Failed execute retry policy (needs-decision M4-T40)"]),
        Ticket("M3-T20", "Add integration test that duplicate buy signature does not double-record transaction or cost basis", 3, 5,
               ["apps/backend/internal/app/swap_integration_test.go"], ["M3-T7"], labels=["backend", "tests"],
               acceptance=["TestDevExecuteBuy_duplicateTxSignature_doesNotDoubleRecordOrCostBasis"],
               out_of_scope=["Sell integration"]),
        Ticket("M3-T21", "Add integration test that sell reduces xStock balance and increases treasury USDC", 3, 5,
               ["apps/backend/internal/app/swap_integration_test.go"], ["M3-T10"], labels=["backend", "tests"],
               acceptance=["TestSellToUSDC_happyPath_reducesXStockAndIncreasesTreasuryUsdc"],
               out_of_scope=["Redeem payout"]),
        Ticket("M3-T22", "Wire just test backend to cover Jupiter quote, execute, and sell tests", 3, 5,
               ["Justfile"], ["M3-T16", "M3-T17", "M3-T18", "M3-T19", "M3-T20", "M3-T21"], labels=["backend", "tests"],
               out_of_scope=["just test mobile beyond M3-T15"]),
    ]

    # M4 - abbreviated summaries for open decision tickets
    m4_oos_base = ["Custom on-chain programs", "On-chain voting", "Android/web", "Privy production webhooks"]
    tickets += [
        Ticket("M4-T1", "Add remaining schema for group settings, members, proposals, votes, NAV snapshots, and payout proofs", 4, 1,
               ["supabase/migrations/"], [], labels=["schema"],
               summary="Extend groups with rules; add proposals, votes, nav_snapshots, redeem_jobs.",
               out_of_scope=["Recreating M1 wallet or M2 deposit tables"]),
        Ticket("M4-T2", "Add domain types for join policy, voter set, threshold, expiry, proposal status, and withdrawal status", 4, 1,
               ["packages/domain/", "apps/backend/internal/app/"], ["M4-T1"], labels=["backend"],
               out_of_scope=["HTTP handlers", "Swift types"]),
        Ticket("M4-T3", "Mint claim units at marked pot NAV on deposit. Keep M2 1:1 USDC credit when treasury holds no marked xStock", 4, 2,
               ["apps/backend/internal/app/deposit.go", "packages/domain/nav.go"], ["M4-T2", "M4-T5"],
               labels=["backend", "parent"], subissues=["M4-T4"],
               acceptance=["TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc", "TestSharesForDeposit_markedPot_mintsSharesFromPotNav"],
               out_of_scope=["Mobile share math"]),
        Ticket("M4-T4", "Add tests for USDC-only deposit credit, post-buy marked-pot minting, and README Alex/Blair worked example", 4, 5,
               ["packages/domain/*_test.go", "apps/backend/internal/app/deposit_test.go"], ["M4-T3"], parent="M4-T3", labels=["tests"],
               acceptance=["TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals", "TestObserveSweep_postBuyDeposit_doesNotLetNewMemberCaptureUnrealizedGain"],
               out_of_scope=["Manual demo script"]),
        Ticket("M4-T5", "Implement pot NAV as treasury USDC plus each xStock marked, with transaction cost basis for holdings", 4, 2,
               ["packages/domain/nav.go", "apps/backend/internal/app/views.go"], ["M4-T2"],
               labels=["backend", "parent"], subissues=["M4-T6"],
               out_of_scope=["Swift NAV math"]),
        Ticket("M4-T6", "Add tests for pot NAV with USDC only, mixed pot, and empty shares", 4, 5,
               ["packages/domain/nav_test.go"], ["M4-T5"], parent="M4-T5", labels=["tests"],
               acceptance=["TestComputePotNAV_usdcOnly_returnsTreasuryUsdc", "TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings", "TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare"],
               out_of_scope=["Pyth live HTTP in unit tests"]),
        Ticket("M4-T7", "Replace thin group create with persisted join policy, voter set, threshold, and expiry", 4, 2,
               ["apps/backend/internal/app/governance.go", "apps/backend/internal/httpapi/groups.go"], ["M4-T2"], labels=["backend"],
               acceptance=["TestPOST_groups_persistsJoinPolicyVoterSetThresholdExpiry"],
               out_of_scope=["Join password flow (M4-T8)"]),
        Ticket("M4-T8", "Implement join for open groups and for password groups", 4, 2,
               ["apps/backend/internal/app/governance.go", "apps/backend/internal/httpapi/groups.go"], ["M4-T7"], labels=["backend"],
               acceptance=["TestPOST_join_openGroup_addsMemberWithoutPassword", "TestPOST_join_passwordGroup_requiresCorrectPassword", "TestPOST_join_wrongPassword_returns403"],
               out_of_scope=["Dissolve on creator leave (needs-decision M4-T41)"]),
        Ticket("M4-T9", "Implement named voter subset with minimum size 1 and every-member voter set mode", 4, 2,
               ["apps/backend/internal/app/governance.go", "packages/domain/votes.go"], ["M4-T7"], labels=["backend"],
               acceptance=["TestVoterSet_namedSubset_enforcesMinimumSizeOne", "TestVoterSet_everyMemberMode_allowsAllMembersToVote"],
               out_of_scope=["Who may propose (needs-decision M4-T39)"]),
        Ticket("M4-T10", "Enforce one user in many groups on membership rows", 4, 2,
               ["apps/backend/internal/postgres/members.go"], ["M4-T7"], labels=["backend", "schema"],
               acceptance=["TestUser_inManyGroups_hasDistinctPositionsPerGroup"],
               out_of_scope=["Cross-group people board math (M4-T28)"]),
        Ticket("M4-T11", "Add GET /v1/groups/{id}/assets?query= with backend xStocks mint resolution for mobile catalog search", 4, 2,
               ["apps/backend/internal/httpapi/catalog.go", "apps/backend/internal/xstocks/"], ["M4-T2"], labels=["backend"],
               acceptance=["TestGET_assets_search_returnsBackendResolvedCatalog"],
               out_of_scope=["Swift xStocks calls"]),
        Ticket("M4-T12", "Add POST /v1/groups/{id}/quotes and refuse proposal create when backend cannot quote USDC to output mint", 4, 2,
               ["apps/backend/internal/httpapi/quotes.go"], ["M4-T11", "M3-T3"], labels=["backend"],
               acceptance=["TestPOST_quotes_noRoute_returnsRoutableFalse", "TestPOST_proposals_noRoute_refusesBeforeInsert"],
               out_of_scope=["Execute on pass"]),
        Ticket("M4-T13", "Implement buy proposal create with symbol, USDC amount, proposer, and expiry deadline", 4, 3,
               ["apps/backend/internal/app/governance.go", "apps/backend/internal/postgres/proposals.go"], ["M4-T9", "M4-T12"], labels=["backend"],
               acceptance=["TestPOST_proposals_happyPath_createsOpenProposalWithExpiry"],
               out_of_scope=["Proposer permission rule until M4-T39 decision"]),
        Ticket("M4-T14", "Implement yes/no votes limited to current voter set members and tally to passed, failed, or expired", 4, 3,
               ["packages/domain/votes.go", "apps/backend/internal/app/governance.go"], ["M4-T13"],
               labels=["backend", "parent"], subissues=["M4-T15", "M4-T16", "M4-T17", "M4-T18"],
               out_of_scope=["On-chain voting"]),
        Ticket("M4-T15", "Implement unanimous tally among the voter set", 4, 3,
               ["packages/domain/votes.go"], ["M4-T14"], parent="M4-T14", labels=["backend"],
               acceptance=["TestTallyProposal_unanimous_allYes_passes"],
               out_of_scope=["Majority mode"]),
        Ticket("M4-T16", "Implement majority tally among the voter set", 4, 3,
               ["packages/domain/votes.go"], ["M4-T14"], parent="M4-T14", labels=["backend"],
               acceptance=["TestTallyProposal_majority_moreYesThanNo_passes", "TestTallyProposal_majority_moreNoThanYes_fails"],
               out_of_scope=["Unanimous mode"]),
        Ticket("M4-T17", "Expire open proposals at deadline with failed status and no swap", 4, 3,
               ["apps/backend/internal/app/governance.go"], ["M4-T14"], parent="M4-T14", labels=["backend"],
               acceptance=["TestTallyProposal_expiredOpenProposal_failsWithoutSwap"],
               out_of_scope=["Failed execute retry (needs-decision M4-T40)"]),
        Ticket("M4-T18", "Add tests for unanimous pass, majority pass, majority fail, and expiry fail", 4, 5,
               ["packages/domain/votes_test.go", "apps/backend/internal/app/governance_test.go"], ["M4-T15", "M4-T16", "M4-T17"], parent="M4-T14", labels=["tests"],
               acceptance=["TestPOST_vote_nonVoterSetMember_returns403", "TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected", "TestPOST_vote_concurrentDoubleVote_recordsOneBallot"],
               out_of_scope=["Jupiter execute integration"]),
        Ticket("M4-T19", "On pass, build Jupiter v2 transaction, sign treasury, POST execute, confirm Success code 0. Delete M3 stub route", 4, 4,
               ["apps/backend/internal/app/start_buy.go", "apps/backend/internal/httpapi/dev_buy.go"], ["M4-T14", "M3-T5"],
               labels=["backend", "parent"], subissues=["M4-T20", "M4-T21", "M4-T22"],
               acceptance=["TestExecuteOnPass_onlyAfterTallyPassed_callsJupiter", "TestDevBuyRoute_deleted_returns404"],
               out_of_scope=["Failed execute retry policy (needs-decision M4-T40)"]),
        Ticket("M4-T20", "Record treasury holdings and write NAV snapshot on confirmed buy transaction", 4, 4,
               ["apps/backend/internal/postgres/nav_snapshots.go", "apps/backend/internal/app/views.go"], ["M4-T19"], parent="M4-T19", labels=["backend", "schema"],
               acceptance=["TestExecuteOnPass_writesNavSnapshotOnConfirm"],
               out_of_scope=["Withdrawal snapshots (M4-T37)"]),
        Ticket("M4-T21", "Add idempotent execute guard keyed on proposal id and Jupiter tx signature", 4, 4,
               ["apps/backend/internal/app/start_buy.go", "apps/backend/internal/postgres/transactions.go"], ["M4-T19"], parent="M4-T19", labels=["backend"],
               acceptance=["TestExecuteOnPass_duplicateProposalAndSignature_executesOnce"],
               out_of_scope=["Manual reconcile UX"]),
        Ticket("M4-T22", "Add tests for idempotent deposit credit, buy execute, and withdrawal payout", 4, 5,
               ["apps/backend/internal/app/*_integration_test.go"], ["M4-T19", "M4-T30"], parent="M4-T19", labels=["tests"],
               acceptance=["TestIdempotency_depositSweepBuyWithdrawal_replaySignatureOnceEach"],
               out_of_scope=["Mobile tests"]),
        Ticket("M4-T23", "Implement member equity and percent return per group from positions", 4, 3,
               ["packages/domain/pnl.go"], ["M4-T5"], labels=["backend"],
               acceptance=["TestMemberEquity_matchesShareFractionTimesPotNav", "TestPercentReturn_equityOverNetInMinusOne"],
               out_of_scope=["Swift formatters (M5-T23)"]),
        Ticket("M4-T24", "Implement net USDC in per member per group from position columns", 4, 3,
               ["packages/domain/pnl.go"], ["M4-T23"], labels=["backend"],
               acceptance=["TestPercentReturn_zeroNetIn_returnsNil"],
               out_of_scope=["People board aggregation"]),
        Ticket("M4-T25", "Skip leaderboard rows when net USDC in equals zero", 4, 3,
               ["packages/domain/pnl.go", "apps/backend/internal/app/views.go"], ["M4-T24"], labels=["backend"],
               acceptance=["TestBoards_skipRowsWhenNetUsdcInZero"],
               out_of_scope=["Dollar-ranked boards"]),
        Ticket("M4-T26", "Implement in-group member board ranked by percent return", 4, 3,
               ["apps/backend/internal/app/views.go"], ["M4-T25"], labels=["backend"],
               acceptance=["TestInGroupBoard_ranksByPercentReturnNotDollars"],
               out_of_scope=["Mobile board UI (M5)"]),
        Ticket("M4-T27", "Implement app-wide group board ranked by pot percent return", 4, 3,
               ["apps/backend/internal/app/views.go"], ["M4-T5"], labels=["backend"],
               acceptance=["TestGroupBoard_ranksPotsByPercentReturn"],
               out_of_scope=["Swift home UI"]),
        Ticket("M4-T28", "Implement app-wide people board ranked by summed cross-group percent return", 4, 3,
               ["apps/backend/internal/app/views.go"], ["M4-T24"], labels=["backend"],
               acceptance=["TestPeopleBoard_aggregatesCrossGroupNetInAndEquity"],
               out_of_scope=["Profile UI (M5-T16)"]),
        Ticket("M4-T29", "Add tests for percent ranking, zero-net skip, and full exit drop from in-group board", 4, 5,
               ["packages/domain/pnl_test.go"], ["M4-T26", "M4-T27", "M4-T28"], labels=["tests"],
               acceptance=["TestInGroupBoard_fullExit_dropsMemberFromBoard"],
               out_of_scope=["Snapshot UI tests"]),
        Ticket("M4-T30", "Implement partial redeem with share amount or dollar target and dust minimum", 4, 4,
               ["apps/backend/internal/app/redeem.go", "packages/domain/redeem.go"], ["M4-T5", "M3-T9"],
               labels=["backend", "parent"], subissues=["M4-T31", "M4-T32", "M4-T33", "M4-T34", "M4-T35", "M4-T36"],
               out_of_scope=["Creator dissolve (needs-decision M4-T41)"]),
        Ticket("M4-T31", "Debit position share units first in row-locked Postgres transaction", 4, 4,
               ["apps/backend/internal/app/redeem.go", "apps/backend/internal/postgres/positions.go"], ["M4-T30"], parent="M4-T30", labels=["backend"],
               acceptance=["TestRedeem_partialByShareAmount_debitsUnitsFirst", "TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit"],
               out_of_scope=["Jupiter sell slice"]),
        Ticket("M4-T32", "Sell redeemed slice of each xStock to USDC on Jupiter when treasury holds stock", 4, 4,
               ["apps/backend/internal/app/redeem.go", "apps/backend/internal/jupiter/sell.go"], ["M4-T31"], parent="M4-T30", labels=["backend"],
               acceptance=["TestRedeem_withXStockInTreasury_sellsSliceBeforePayout"],
               out_of_scope=["Pay USDC to external wallet"]),
        Ticket("M4-T33", "Pay USDC only to payout address user proved with signed message. Persist withdrawal row", 4, 4,
               ["apps/backend/internal/privy/payout.go", "apps/backend/internal/postgres/withdrawals.go"], ["M4-T32"], parent="M4-T30", labels=["backend"],
               acceptance=["TestRedeem_validProof_paysUsdcOnlyToProvenAddress", "TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot"],
               out_of_scope=["Dissolve flow"]),
        Ticket("M4-T34", "Reject redeem when payout pubkey fails ownership proof", 4, 4,
               ["apps/backend/internal/app/redeem.go"], ["M4-T30"], parent="M4-T30", labels=["backend"],
               acceptance=["TestRedeem_invalidPayoutProof_rejected"],
               out_of_scope=["Retry UX for failed proof"]),
        Ticket("M4-T35", "Write NAV snapshot and recompute boards after confirmed withdrawal payout", 4, 4,
               ["apps/backend/internal/postgres/nav_snapshots.go", "apps/backend/internal/app/views.go"], ["M4-T33"], parent="M4-T30", labels=["backend"],
               acceptance=["TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot"],
               out_of_scope=["Mobile board refresh (M5-T19)"]),
        Ticket("M4-T36", "Add tests for debit-first ordering, slice USDC versus deposit refund, and board recompute", 4, 5,
               ["apps/backend/internal/app/redeem_test.go"], ["M4-T31", "M4-T35"], parent="M4-T30", labels=["tests"],
               acceptance=["TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund", "TestRedeem_concurrentDoubleRedeem_debitsOnce"],
               out_of_scope=["Mainnet manual redeem"]),
        Ticket("M4-T37", "Persist NAV snapshots on deposit, transaction confirm, and withdrawal payout", 4, 2,
               ["apps/backend/internal/postgres/nav_snapshots.go"], ["M4-T5"], labels=["backend", "schema"],
               acceptance=["TestNavSnapshot_writtenOnDepositConfirmTransactionConfirmWithdrawalPayout"],
               out_of_scope=["Chart history"]),
        Ticket("M4-T38", "Wire after-hours flag when Pyth equity mark is frozen", 4, 2,
               ["apps/backend/internal/pyth/", "apps/backend/internal/app/views.go"], ["M4-T5"], labels=["backend"],
               acceptance=["TestMarkedPot_afterHoursFlag_surfacesOnGroupView"],
               out_of_scope=["Swift after-hours label (M5-T20)"]),
        Ticket("M4-T39", "Open decision ticket: who may propose a buy", 4, 5,
               ["docs/"], [], labels=["needs-decision"],
               needs_decision=True,
               summary="Record product decision only. Options: any member, voter set only, or creator only. Do not implement until decided.",
               out_of_scope=["Implementation", "API behavior", "UI buttons"] + m4_oos_base),
        Ticket("M4-T40", "Open decision ticket: failed Jupiter execute after passed vote", 4, 5,
               ["docs/"], [], labels=["needs-decision"],
               needs_decision=True,
               summary="Record product decision only. Retry, refund, or manual reconcile — do not pick in code until decided.",
               out_of_scope=["Implementation", "Automatic retry", "Refund flows"] + m4_oos_base),
        Ticket("M4-T41", "Open decision ticket: creator leave and group dissolve", 4, 5,
               ["docs/"], [], labels=["needs-decision"],
               needs_decision=True,
               summary="Record product decision only. Creator leave and group dissolve rules stay unlocked.",
               out_of_scope=["Implementation", "Dissolve API", "Treasury wind-down automation"] + m4_oos_base),
    ]

    m5_oos = ["Android/web", "App Store public listing", "On-chain governance UI", "Production Privy webhooks"]
    tickets += [
        Ticket("M5-T1", "Replace M1 wallet-debug login shell with product launch screen", 5, 1,
               ["apps/mobile/Features/Auth/"], [], labels=["mobile"],
               summary="Product launch UX; keep SMS demo path and optional email/password for testers.",
               manual=["SMS sign-in on slim sim"],
               out_of_scope=m5_oos + ["Hiding addresses (M5-T21)"]),
        Ticket("M5-T2", "Add session gate and send signed-in users to app home", 5, 1,
               ["apps/mobile/Features/Auth/", "apps/mobile/Features/Home/"], ["M5-T1"], labels=["mobile"],
               acceptance=["testAPIClient_getHome_callsV1Home"],
               out_of_scope=["Board layout (M5-T14+)"]),
        Ticket("M5-T3", "Build create group with join policy, voter set picker, threshold, and expiry duration", 5, 2,
               ["apps/mobile/Features/Groups/CreateGroupView.swift"], ["M5-T2"], labels=["mobile"],
               out_of_scope=["Proposer permission UI until M4-T39 decided"]),
        Ticket("M5-T4", "Build join group with open join and password entry", 5, 2,
               ["apps/mobile/Features/Groups/JoinGroupView.swift"], ["M5-T2"], labels=["mobile"],
               out_of_scope=["Dissolve UX until M4-T41 decided"]),
        Ticket("M5-T5", "Build deposit with add money copy and sweep status feedback", 5, 2,
               ["apps/mobile/Features/Deposit/"], ["M5-T2"], labels=["mobile"],
               acceptance=["Deposit poll state machine: pending → confirmed from fixture deposit DTO"],
               out_of_scope=["Member-wallet balance as money display"]),
        Ticket("M5-T6", "Build catalog search via GET /v1/groups/{id}/assets", 5, 3,
               ["apps/mobile/Features/Proposals/", "apps/mobile/API/MonacoAPIClient.swift"], ["M5-T3"], labels=["mobile"],
               acceptance=["testAPIClient_searchAssets_usesGroupsAssetsQueryParam", "testProductFeatures_noDirectXStocksJupiterPythOrSolanaRpcUrls"],
               out_of_scope=["Direct xStocks API calls"]),
        Ticket("M5-T7", "Disable propose when POST /v1/groups/{id}/quotes returns routable false", 5, 3,
               ["apps/mobile/Features/Proposals/"], ["M5-T6"], labels=["mobile"],
               acceptance=["testAPIClient_postQuotes_sendsSymbolAndUsdc", "testAPIClient_postProposal_doesNotCallWhenRoutableFalse"],
               out_of_scope=["Backend quote logic"]),
        Ticket("M5-T8", "Build propose buy with amount entry and social copy", 5, 3,
               ["apps/mobile/Features/Proposals/"], ["M5-T7"], labels=["mobile"],
               out_of_scope=["Mint jargon in copy (M5-T22 audit)"]),
        Ticket("M5-T9", "Build proposal detail with yes and no for voter set members", 5, 3,
               ["apps/mobile/Features/Proposals/ProposalDetailView.swift"], ["M5-T8"], labels=["mobile"],
               out_of_scope=["Failed execute retry UI until M4-T40 decided"]),
        Ticket("M5-T10", "Show proposal status for open, passed, failed, and expired", 5, 3,
               ["apps/mobile/Features/Proposals/"], ["M5-T9"], labels=["mobile"],
               acceptance=["testProposalDTO_decodesOpenPassedFailedExpiredStatuses"],
               out_of_scope=["On-chain status"]),
        Ticket("M5-T11", "Build group pot section with USDC and xStock rows, units, mark, and dollar value", 5, 3,
               ["apps/mobile/Features/Groups/GroupDetailView.swift"], ["M5-T3"], labels=["mobile"],
               acceptance=["testGroupViewDTO_decodesFixtureWithPotAndMemberBoard"],
               out_of_scope=["Recomputing marks in Swift"]),
        Ticket("M5-T12", "Build you section with slice dollars, slice percent, dollar P&L, and percent return", 5, 3,
               ["apps/mobile/Features/Groups/"], ["M5-T11"], labels=["mobile"],
               out_of_scope=["Equity math in Swift"]),
        Ticket("M5-T13", "Build in-group member board ranked by percent return", 5, 3,
               ["apps/mobile/Features/Groups/"], ["M5-T12"], labels=["mobile"],
               acceptance=["Board cells render server rank order without re-sorting by dollars"],
               out_of_scope=["People board (M5-T15)"]),
        Ticket("M5-T14", "Build app home group board with name, percent, and dollar P&L per row", 5, 3,
               ["apps/mobile/Features/Home/"], ["M5-T2"], labels=["mobile"],
               acceptance=["testHomeViewDTO_decodesGroupAndPeopleBoards"],
               out_of_scope=["Cross-group math in Swift"]),
        Ticket("M5-T15", "Build app home people board with one row per user across all groups", 5, 3,
               ["apps/mobile/Features/Home/"], ["M5-T14"], labels=["mobile"],
               out_of_scope=["Profile drill-down (M5-T16)"]),
        Ticket("M5-T16", "Wire tap-through from boards to group detail and profile list of groups", 5, 3,
               ["apps/mobile/Features/Home/", "apps/mobile/Features/Groups/"], ["M5-T13", "M5-T15"], labels=["mobile"],
               out_of_scope=["Deep linking"]),
        Ticket("M5-T17", "Build partial redeem with slider, dust minimum, and cash out copy", 5, 4,
               ["apps/mobile/Features/Redeem/"], ["M5-T13"], labels=["mobile", "parent"],
               subissues=["M5-T18", "M5-T19"],
               acceptance=["testRedeemSlider_dustMinimum_disablesSubmitBelowThreshold"],
               out_of_scope=["Full exit special case beyond slider max"]),
        Ticket("M5-T18", "Collect signed payout address proof before redeem submit", 5, 4,
               ["apps/mobile/Features/Redeem/"], ["M5-T17"], parent="M5-T17", labels=["mobile"],
               acceptance=["testAPIClient_postRedeem_includesPayoutProofPayload"],
               out_of_scope=["Backend proof verification (M4-T34)"]),
        Ticket("M5-T19", "Refresh all three boards after redeem success", 5, 4,
               ["apps/mobile/Features/Redeem/", "apps/mobile/Features/Home/", "apps/mobile/Features/Groups/"], ["M5-T18"], parent="M5-T17", labels=["mobile"],
               acceptance=["testAPIClient_afterRedeemSuccess_refreshesGroupAndHome"],
               out_of_scope=["Push notifications"]),
        Ticket("M5-T20", "Add after-hours label on xStock pot rows when afterHours is true on group view", 5, 3,
               ["apps/mobile/Features/Groups/"], ["M5-T11"], labels=["mobile"],
               acceptance=["testPotRowDTO_afterHoursTrue_decodesLabelFlag"],
               out_of_scope=["Pyth client in Swift"]),
        Ticket("M5-T21", "Add Settings with Advanced explorer links only. Move Solana addresses off main flow", 5, 2,
               ["apps/mobile/Features/Settings/"], ["M5-T2"], labels=["mobile"],
               acceptance=["testSettingsAdvanced_containsExplorerLinksOnly"],
               out_of_scope=["Wallet copy on main flow"]),
        Ticket("M5-T22", "Copy audit on main flow screens. No wallet, gas, seed phrase, mint, or NAV in user-visible strings", 5, 4,
               ["apps/mobile/Features/"], ["M5-T5", "M5-T8", "M5-T13", "M5-T15", "M5-T17"], labels=["mobile", "tests"],
               acceptance=["testMainFlowStrings_excludeWalletGasSeedPhraseMintAndNav"],
               out_of_scope=["Settings Advanced explorer strings"]),
        Ticket("M5-T23", "Add Swift unit tests for percent return and net USDC in display formatting", 5, 4,
               ["apps/mobile/Tests/Display/"], [], labels=["mobile", "tests"],
               acceptance=["testPercentReturnFormatter_positiveReturn_showsPlusPrefix", "testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden", "testDollarPnlFormatter_negativeShowsLossCopy", "testSlicePercentFormatter_formatsOneDecimal"],
               out_of_scope=["Recomputing equity"]),
        Ticket("M5-T24", "Optional snapshot tests for group screen and app home", 5, 5,
               ["apps/mobile/Tests/Snapshots/"], ["M5-T13", "M5-T15"], labels=["mobile", "tests"],
               acceptance=["testGroupScreen_snapshot_matchesFixture", "testAppHome_snapshot_matchesFixture"],
               out_of_scope=["Required for demo gate"]),
        Ticket("M5-T25", "Optional TestFlight upload and internal tester invite", 5, 5,
               ["apps/mobile/"], ["M5-T26"], labels=["mobile"],
               manual=["Upload build", "Invite internal testers"],
               out_of_scope=["App Store public listing"]),
        Ticket("M5-T26", "Run full demo script on gold slim sim UDID", 5, 4,
               ["docs/"], ["M5-T16", "M5-T19", "M5-T22"], labels=["mobile"],
               manual=["README hackathon demo checklist on $SIMSLIM_UDID (machine gold slim sim)"],
               out_of_scope=["Android demo"]),
    ]

    return tickets


MILESTONE_FILES = {
    0: "m0-scaffold.md",
    1: "m1-auth-wallets.md",
    2: "m2-deposits.md",
    3: "m3-jupiter.md",
    4: "m4-domain.md",
    5: "m5-mobile.md",
}


def patch_docs(tickets: list[Ticket]) -> list[str]:
    changed = []
    for m, fname in MILESTONE_FILES.items():
        subset = [t for t in tickets if t.milestone == m]
        inject_ticket_details(fname, subset)
        changed.append(str(DOCS / fname))
    return changed


def ensure_labels() -> None:
    existing = {x["name"] for x in gh_api("GET", f"repos/{REPO}/labels?per_page=100")}
    for name, color, desc in LABELS:
        if name in existing:
            continue
        gh_api("POST", f"repos/{REPO}/labels", {"name": name, "color": color, "description": desc})
        print(f"label created: {name}")


def ensure_milestones() -> dict[str, int]:
    existing = gh_api("GET", f"repos/{REPO}/milestones?state=all&per_page=100")
    by_title = {m["title"]: m for m in existing}
    nums: dict[str, int] = {}
    for title, desc in MILESTONES:
        if title in by_title:
            nums[title] = by_title[title]["number"]
            print(f"milestone exists: {title} #{nums[title]}")
            continue
        m = gh_api("POST", f"repos/{REPO}/milestones", {"title": title, "description": desc, "state": "open"})
        nums[title] = m["number"]
        print(f"milestone created: {title} #{nums[title]}")
    return nums


def fetch_all_issue_nums(tickets: list[Ticket]) -> dict[str, int]:
    issue_nums: dict[str, int] = {}
    for t in tickets:
        num = gh_search_issue(t.mid)
        if num:
            issue_nums[t.mid] = num
    return issue_nums


def sync_issues(tickets: list[Ticket], milestone_nums: dict[str, int]) -> tuple[dict[str, int], int, int, int]:
    issue_nums = fetch_all_issue_nums(tickets)
    order = sorted(tickets, key=lambda t: (t.milestone, 0 if t.subissues else 1 if t.parent else 0, t.mid))
    created = updated = skipped = 0
    for t in order:
        ms_title = MILESTONES[t.milestone][0]
        title = f"{t.mid}: {t.title}"
        labels = default_labels(t)
        milestone = milestone_nums[ms_title]
        existing = issue_nums.get(t.mid) or gh_search_issue(t.mid)

        body = issue_body(t, issue_nums)
        ok, errs = validate_body(body)
        if not ok:
            raise RuntimeError(f"{t.mid} body failed validation: {errs}")

        if existing:
            issue_nums[t.mid] = existing
            cur = gh_api("GET", f"repos/{REPO}/issues/{existing}")
            if is_thin_body(cur.get("body") or "") or cur.get("body") != body:
                gh_api(
                    "PATCH",
                    f"repos/{REPO}/issues/{existing}",
                    {"body": body, "title": title, "labels": labels, "milestone": milestone},
                )
                updated += 1
                print(f"updated: {t.mid} #{existing}")
            else:
                skipped += 1
                print(f"skip current: {t.mid} #{existing}")
            time.sleep(0.2)
            continue

        payload = {
            "title": title,
            "body": body,
            "labels": labels,
            "milestone": milestone,
        }
        issue = gh_api("POST", f"repos/{REPO}/issues", payload)
        issue_nums[t.mid] = issue["number"]
        created += 1
        print(f"created: {t.mid} #{issue['number']}")
        time.sleep(0.3)

    # second pass: refresh bodies now that all issue numbers exist
    for t in tickets:
        num = issue_nums.get(t.mid)
        if not num:
            continue
        body = issue_body(t, issue_nums)
        gh_api("PATCH", f"repos/{REPO}/issues/{num}", {"body": body})

    print(f"issues created={created} updated={updated} skipped={skipped}")
    return issue_nums, created, updated, skipped


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: create_milestone_issues.py docs|github|all")
        return 1
    tickets = all_tickets()
    cmd = sys.argv[1]
    if cmd in ("docs", "all"):
        changed = patch_docs(tickets)
        print("docs patched:", *changed, sep="\n  ")
    if cmd in ("github", "all"):
        ensure_labels()
        milestone_nums = ensure_milestones()
        issue_nums, created, updated, skipped = sync_issues(tickets, milestone_nums)
        counts: dict[int, int] = {i: 0 for i in range(6)}
        for t in tickets:
            counts[t.milestone] += 1
        thin: list[str] = []
        for t in tickets:
            num = issue_nums.get(t.mid)
            if not num:
                thin.append(f"{t.mid} (missing)")
                continue
            cur = gh_api("GET", f"repos/{REPO}/issues/{num}")
            if is_thin_body(cur.get("body") or ""):
                thin.append(f"{t.mid} (#{num})")
        print("\n=== SUMMARY ===")
        print(f"created={created} updated={updated} skipped={skipped} total_tickets={len(tickets)}")
        for i, (title, _) in enumerate(MILESTONES):
            n = milestone_nums[title]
            print(f"M{i} {title}: {counts[i]} tickets — https://github.com/{REPO}/milestone/{n}")
        if thin:
            print("still-thin:", ", ".join(thin))
        else:
            print("still-thin: none")
        sample_mid = "M1-T4"
        if sample_mid in issue_nums:
            sample_body = issue_body(next(t for t in tickets if t.mid == sample_mid), issue_nums)
            print(f"\n=== SAMPLE BODY ({sample_mid} #{issue_nums[sample_mid]}) ===\n{sample_body}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

# Prompt templates

## Kickoff

Sequential:

> Starting with `<foundation>` in a worktree — later slices depend on it. Implement → one light review → merge.

Parallel:

> One worktree agent per lane. I light-review and merge as they finish.

---

## Implementer (`best-of-n-runner`, `model: composer-2.5`, `run_in_background: true`)

```
# Implement SLICE <N> | lane <lane>

Stay in this worktree/branch. Ship code; don’t spawn reviewers.

Plan: <abs path>
Repo: <abs root>
Integration branch (merge later): <branch>
Feature branch: feat/<name>
Start from tip: <sha>

## Ownership (HARD)
Own: <paths from AGENTS.md / plan>
Do NOT: other lanes; docs/plans/**; docs/brainstorms/**
Avoid shared/ops unless this slice owns them. If must: minimal + list in report.

## Done gate
<AGENTS.md gate>
No just start / just up / full-stack smoke.

## Commits
feat(<lane>): …

## Final report
1. Branch + tip SHA
2. Commits
3. Implemented vs deferred
4. Shared-file touches
5. check/test results
6. Worktree path or "removed, branch retained"
```

---

## Light review (`cavecrew-reviewer` or `generalPurpose`, `model: cursor-grok-4.5-high`)

```
Review feat/<branch> before merge into <integration>.
Worktree: <path or none>
Commit: <sha>
Plan: <path>

Check plan + AGENTS ownership + done-gate evidence.
Return APPROVE or REQUEST_CHANGES with path:line. Brief.
Blocking bugs only.
```

Parallel:

```
# Review feat/<lane>-impl (read-only — DO NOT merge/push)
git diff <integration>...HEAD --stat + spot-check
Return MERGE_OK | NEEDS_FIX | BLOCK with path:line.
```

---

## Fix (after light fail)

```
Light review blocked feat/<branch>.
Worktree: <path>
Findings:
- <path:line — issue>

Fix only those. Re-run lane done gate. Return tip SHA.
```

Prefer `resume:<implementer-id>`. Model: `composer-2.5`.

---

## Opt-in deep (ONLY if user asked)

Single Task, `model: cursor-grok-4.5-high` (deslop edits: `composer-2.5`).
No multi-model panel. One skill per ask.

```
<field: deslop | thermo-nuclear | blast-radius | interrogate-rubric-only>
feat/<branch> vs <integration>
Intent: <one paragraph>
Return blocking vs non-blocking path:line. Do not auto-apply (except deslop).
```

---

## Parallel merge (`generalPurpose`, `composer-2.5`)

```
On <repo>, branch <integration>.
Merge --no-ff in order: feat/<a>, feat/<b>, …
On conflict: abort, report, STOP.
Do not push. Return merge SHAs.
```

---

## Retry

```
RETRY: prior agent failed (<error>). Same mission for feat/<branch>.
Start from tip: <sha>
model must be composer-2.5 (implement) or cursor-grok-4.5-high (review only).
```

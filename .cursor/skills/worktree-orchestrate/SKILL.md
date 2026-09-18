---
name: worktree-orchestrate
description: Orchestrate implementation via composer-2.5 best-of-n-runner subagents in git worktrees, one cursor-grok-4.5-high review, then merge into the current integration branch. Use when the user asks to implement with worktree subagents, orchestrate parallel lane agents, slice a plan across feature branches, or merge reviewed worktree work into this branch.
---

# Worktree orchestrate

**Code first.** Parent stays on the **integration branch**. Implementers ship in worktrees on `feat/*`. One light review → merge. Never push unless user asks.

Before dispatch: read repo `AGENTS.md` (and lane READMEs) for ownership + done gate.

Announce: `Using worktree-orchestrate to [slice|parallel] → light review → merge into <branch>.`

## Models (hard allowlist)

**Only** these Task `model` values. Pass explicitly. Never inherit a third model. Never follow another skill’s multi-model panel.

| Work | `model` |
|------|---------|
| Implement, deslop, fix, merge, post-merge build fix | `composer-2.5` |
| Light review (and any **opt-in** deep review) | `cursor-grok-4.5-high` |

If a skill (e.g. `interrogate`) wants other models — **do not use that skill’s panel**. Either skip it or run a **single** `cursor-grok-4.5-high` reviewer with that skill’s rubric.

## Loop (default — match historical chats)

Same shape as strategy-slice / parallel-lane runs:

```
dispatch implementer(s)          # composer-2.5 best-of-n-runner, background
  → spot-check branch / tip SHA / --stat
  → ONE light review             # cursor-grok-4.5-high, read-only
  → REQUEST_CHANGES → fix on feat branch → re-light once
  → merge into integration
  → (sequential) cite new tip SHA → next slice
  → (parallel) wait all MERGE_OK → ordered merge
  → workspace done gate
```

**Do not** run deslop / thermo-nuclear / interrogate / blast-radius / `ce-code-review` on the default path.

**Opt-in deep gate** only when user says so (e.g. “deep review”, “interrogate”, “blast radius”, “thermo”). Then at most **one** `cursor-grok-4.5-high` Task per requested skill, after light APPROVE, before merge. Still no multi-model fan-out.

## Mode pick

| Mode | When | How |
|------|------|-----|
| **Sequential slices** | Shared `contracts` / `bus` / ops, or deps between slices | One implementer; merge; next prompt gets **tip SHA** |
| **Parallel lanes** | Disjoint ownership | All implementers in **one** message; merge after each (or all) light-clear |

Default: sequential if unsure.

**Shared serialization:** one agent at a time on `contracts`, `bus`, `justfile`, `docker-compose.yml`, `.env.example`. Foundation slice first when contracts change.

## Subagent defaults

| Role | `subagent_type` | `model` | `run_in_background` |
|------|-----------------|---------|---------------------|
| Implementer | `best-of-n-runner` | `composer-2.5` | `true` |
| Light review | `cavecrew-reviewer` or `generalPurpose` | `cursor-grok-4.5-high` | `true` |
| Fix | `resume:<id>` or `best-of-n-runner` on same branch | `composer-2.5` | as needed |
| Merge (multi-lane) | `generalPurpose` | `composer-2.5` | — |
| Post-merge break | `best-of-n-runner` on **main** checkout | `composer-2.5` | `true` |
| Opt-in deep (only if asked) | `generalPurpose` | `cursor-grok-4.5-high` | `true` |

No `environment: cloud`.

## Speed rules

- Prefer **implementing next slice** over extra reviewers.
- **One** light review per slice/lane — not parallel adversarial panels.
- Skip review only for pure typo/docs if user ok; otherwise always one light pass.
- Cap fix→re-review at **one** round unless still broken; then report to user.
- Do not nest `lfg`, `ce-code-review` autofix pipelines, or `interrogate` multi-model.

## Optional companions

| When | Skill | Note |
|------|-------|------|
| No plan | `ce-plan` | Before dispatch |
| Implementer body | mention `ce-work` in prompt | Don’t re-read whole CE stack in parent |
| User asks deep | `deslop` / thermo / blast / single-grok interrogate rubric | Opt-in only; `cursor-grok-4.5-high` or `composer-2.5` for deslop edits |
| Merge conflict + user ok | `fix-merge-conflicts` | Else abort+report |
| Ship later | `ce-commit-push-pr` / `pr-summary` | After orchestration |

## Implementer prompt

```
# Implement <SLICE N | lane <name>>

Stay in this worktree/branch. Ship code; don’t spawn reviewers.

Plan: <path or user message>
Repo: <root>
Integration branch (merge later): <name>
Feature branch: feat/<name>
Start from tip: <sha>

## Ownership (HARD)
Own: <paths>
Do NOT: other lanes; docs/plans/**; docs/brainstorms/**
Avoid shared/ops unless this slice owns them — if must, minimal + report paths.

## Done gate
<AGENTS.md gate, e.g. cargo check -p <lib> -p <lib>-app && cargo test -p <lib>>
No just start / just up / full-stack smoke.

## Commits
feat(<lane>): … on the feature branch.

## Final report
1. Branch + tip SHA
2. Commits
3. Implemented vs deferred
4. Shared-file touches
5. check/test results
6. Worktree path or "removed, branch retained"
```

`WORKTREE_ID=<purpose>-<short-hash>`. Keep branch for parent merge.

## Light review prompt

Read-only. No merge/push. **Blocking bugs only.**

```
Review feat/<branch> before merge into <integration>.
Commit: <sha>
Plan: <path>
Check: plan + AGENTS ownership + done-gate evidence.

Return APPROVE or REQUEST_CHANGES with path:line. Brief.
```

Parallel lanes: same idea → `MERGE_OK` | `NEEDS_FIX` | `BLOCK`.

## Merge

Parent on integration branch.

```bash
git merge feat/<branch> -m "$(cat <<'EOF'
Merge feat/<branch> into <integration>.

<one-line why>
EOF
)"
```

Parallel: fixed order (writers/foundation first), `--no-ff` ok. Conflict → abort + report unless user says resolve.

Keep feature branches until user deletes them.

## Workspace verify

Touched packages’ done gate. After shared/contracts merges: workspace check / `just build`. Breaks → fix on **integration** in main worktree.

## Failure recovery

| Signal | Action |
|--------|--------|
| `REQUEST_CHANGES` / `NEEDS_FIX` | Fix once on feat; re-light once |
| Dead / `resource_exhausted` / API limit | `RETRY:` same mission; **do not** fan out more reviewers |
| Wrong-model / empty subagent | Relaunch with allowlist model only |
| Merge conflict | Abort; report |
| Post-merge build break | Fix on integration |

## Anti-patterns

- Default deep gate (thermo ∥ interrogate ∥ blast)
- Any Task model outside `composer-2.5` / `cursor-grok-4.5-high`
- Multi-model interrogate panel
- Reviewing instead of dispatching the next implementer
- Implementing on main checkout during orchestration
- Full-stack smoke as done gate
- Nesting `lfg`

## Templates

[prompts.md](prompts.md)

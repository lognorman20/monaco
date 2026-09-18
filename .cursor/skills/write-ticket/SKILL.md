---
name: write-ticket
description: Draft issue descriptions with the user's required structure, acceptance criteria, validation rules, labels, and MCP creation workflow. Use when the user asks to write, draft, create, save, or place Linear tickets, issues, sub-issues, ticket bodies, issue descriptions, or acceptance criteria.
---

# Write Ticket

Use this skill whenever writing a ticket body or creating a issue through MCP.

## Core Rules

- Default to detailed tickets.
- Default to giving a fenced codeblock of the markdown containing your ticket. Do not put triple-backtick codeblocks inside that output. For commands in the ticket body, use indented command lines or inline code. If a nested codeblock is unavoidable, wrap the whole ticket in a four-backtick fence.
- Use the full six-section body unless the user clearly asks for brevity with words like "quick issue", "one-liner", "just the AC", "brief"
- Keep `Context` and `Problem` distinct. `Context` explains why this work matters now, what prior work or workflow led here, and who or what is affected. `Problem` states the concrete current failure, gap, or messy behavior without repeating the motivation.
- If the user gives only a title, draft the description from the full template yourself. Ask follow-up questions only when missing context would make the ticket misleading. Do research in the codebase when necessary to fully comprehend.
- Every issue description must include `## Acceptance Criteria` with at least 2 concrete, testable checklist items.
- Before MCP issue creation, validate that the body has non-empty content, at least 120 characters excluding headings, an Acceptance Criteria heading, and at least 2 non-placeholder AC bullets.
  - Ask the user where the issue should go if not already specified (e.g. engineering triage queue, a specific project, etc...)
- For MCP-created issues, apply labels when available:
  - Exactly one type label: `feature`, `bug`, `refactor`, `chore`, or `spike`
  - 1-2 domain labels: `backend`, `frontend`, `security`, `infrastructure`, etc.
- When using the Linear MCP, read the tool schema first. Send markdown strings with real newlines, not literal `\n`.

## Issue Description Template

Copy this skeleton into the `description` argument of `create-issue`, `create-sub-issue`, or the Linear MCP equivalent. Fill in every section before submitting.

```markdown
**Title:** <title>

## Context

<Why this work matters now. Include prior issues, docs, incidents, user pain, workflow pressure, or business/operational impact. Do not restate the broken behavior in detail here.>

## Problem

<The concrete current failure, gap, or messy behavior. Name the file, flow, route, command, or behavior. Avoid repeating the motivation from Context.>

## Proposal

<What you intend to do about it. For implementation tickets, describe the high-level approach, not line-by-line code. For investigation or bug-fix tickets, describe likely solution directions, hypotheses to validate, and where to look first. Keep codeblocks minimal; when used, they should only sketch the idea.>

### Scope

<What is included in this issue. Name specific apps, services, files, routes, user flows, or environments when known.>

## Acceptance Criteria

- [ ] <Concrete, testable outcome>
- [ ] <Concrete, testable outcome>

## Verification

<How the AC will actually be checked. Start with baseline checks like `pnpm build` and passing relevant tests, then add task-specific manual steps, commands, screenshots, logs, or review instructions.>
```

## Depth Bar

Match this level of detail when drafting normal tickets:

````markdown
## Context

**Title:** Cache Linear label list in memory to avoid re-fetching on every `labels validate` call

`labels validate` is invoked from `create-issue` (`scripts/linear-ops.ts:135`) on every issue creation. It re-fetches the full label list from Linear on each call — ~400ms round-trip. For batch scripts creating 10+ issues in a loop, that's 4+ seconds of avoidable latency, and it's the dominant cost now that the `lin` CLI fast-path handles the cheap cases.

## Problem

`scripts/lib/labels.ts:fetchAllLabels()` has no caching. Each caller gets a fresh network fetch even when the label set hasn't changed within the process lifetime. No in-memory map, no module-level singleton, no short-lived cache.

## Proposal

Add an in-memory cache to `fetchAllLabels()` keyed by workspace ID (derived from the SDK client). Cache TTL is the process lifetime — no invalidation needed, because new labels appearing mid-batch isn't a realistic case for CLI scripts. Fall through to the network on cache miss; populate the cache on success only (do not cache failures).

## Acceptance Criteria

- [ ] `fetchAllLabels()` makes at most one network call per process for a given workspace
- [ ] A new test in `scripts/__tests__/labels.test.ts` spies on the fetcher and asserts call count ≤ 1 across 3 consecutive `validate` invocations
- [ ] No change to `fetchAllLabels()` signature — all call sites remain identical
- [ ] `LINEAR_DISABLE_LABEL_CACHE=1` env var escape hatch restores fetch-every-call behavior

## Verification

```bash
pnpm run build && pnpm test
# then, with a valid LINEAR_API_KEY:
time pnpm run ops -- labels validate "feature,backend"   # run 3 times back-to-back
# First run: ~400ms (network). Runs 2-3: <50ms each (cached).
LINEAR_DISABLE_LABEL_CACHE=1 pnpm run ops -- labels validate "feature,backend"
# Should re-fetch, ~400ms again.
```
````

## Expanded Ticket Shape

Use this shape when the issue is broad, exploratory, or multi-route:

```markdown
## Problem

<What is broken or noisy today. Include current behavior, affected systems, and why it matters.>

## Goal

<The outcome expected from the work.>

## Scope

<What areas are included in this pass. Be explicit about excluded areas or follow-ups.>

## Proposed Approach

<Ordered, high-level implementation approach. Include prioritization when useful.>

## Acceptance Criteria

- [ ] <Concrete, testable outcome>
- [ ] <Concrete, testable outcome>

## Risks and Rollback

<Risks, mitigations, and rollback path.>

## Observability and Validation

<Manual validation, logs, metrics, screenshots, commands, or review proof required.>

## Dependencies

<Related issues, prior work, docs, or blockers.>
```

## MCP Creation Workflow

1. Draft the title and description using the template.
2. Validate the body against the rules above.
3. Choose labels from the taxonomy rules above.
4. Read the Linear MCP tool schema before calling the tool.
5. Create the issue with real markdown newlines in the `description`.
6. Return the Linear issue key and URL.

## Brief Mode

Only use a shortened ticket when the user asks for brevity. Even then, keep:

```markdown
## Context

<Short context.>

## Acceptance Criteria

- [ ] <Concrete, testable outcome>
- [ ] <Concrete, testable outcome>
```

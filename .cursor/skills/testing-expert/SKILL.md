---
name: testing-expert
description: Expert-level TypeScript testing skill focused on writing high-quality tests that find bugs, ensure intended functionality, serve as documentation, and prevent regressions. Use when writing service code, unit tests, integration tests, and E2E tests.
---

# Testing Expert

## Overview

Write tests that find bugs, document behavior, verify expected functionality, and prevent regressions. Prefer focused, deterministic tests with realistic data and the smallest useful dependency surface.

Scope: functions, components, unit tests, and integration tests. Avoid black-box e2e unless the user asks for journey testing.

Tooling: `jest`, `ts-jest`, and `fast-check`. Recommend missing tools when needed, then adapt to what the repo already has.

## Testable Code Signals

Code is hard to test when it has:

- Non-deterministic objects (e.g., using `new Date()` or `Math.random()` directly)
- Really long functions
- Mixed concerns (business logic + object creation in same function)
- Doesn't follow single resposibility principle
- Dependencies on external lookups (DB searches, API calls) without injection
- Work done in constructors instead of methods
- Too many conditionals and branching paths
- Global state dependencies
- Too many dependencies it creates itself instead of asking for what it needs

Aim for:

- Small, isolated components testable individually
- Functions that either contain business logic OR create things, not both
- Dependencies injected, not instantiated internally
- Unit tests that state the writer's assumptions
- Tests that tell the story of how the function is used

## Unit Tests

- Focus on exhaustively testing each core function in a service file
- Keep tests quick and deterministic: no network calls, no real databases
- Use `jest.useFakeTimers()` for time-dependent code
- Mock only the methods the service actually touches (minimal surface)
- Use Nest `Test.createTestingModule` with `useValue` mocks for dependencies
- Prisma mock: plain object with `jest.fn()` on nested methods (`prisma.market.findMany`)
- External libraries (e.g. `ioredis`, `ably`, `@aws-sdk`): use `jest.mock` at module level

Test:

- Expected behavior, not implementation details
- Return shapes and branching logic
- Happy path handling
- ALL known error paths
- Function behavior with mocked external behavior (e.g. Solana transactions coming in)

Do not test:

- Private helpers directly; test them through public callers
- Complex raw SQL with mocked Prisma; use integration tests
- External dependency behavior; mock it
- Controllers, those should be done via integration tests

Test data:

- Always use factories from `@factmachine/db/test` when available (`UserFactory`, `MarketFactory`, etc.)
- Use local helpers for domain objects, e.g. `buildMarket(overrides)`, `makeJob(overrides)`
- Use describe-scoped constants for shared fixtures

## Integration Tests

Integration tests verify the request to DB lifecycle. They should not rehash unit tests.

Prefer controller integration tests:

- Verify the full HTTP request and response cycle
- Focus on valid params in, expected DB response out
- Verify status codes, response shapes, and Zod contract validation
- Use supertest with `request(app.getHttpServer())`
- Mock external services (Ably, metrics), but keep Prisma and DB real

Use service integration tests rarely:

- Only when unit tests cannot verify behavior well
- Good fits: raw SQL, complex aggregations, leaderboard ranking logic
- Call `service.method()` directly against real DB

Describe structure:

- Top level: `ControllerName (Integration)` or `ServiceName (Integration)`
- Nested level: HTTP verb + full route path, e.g. `POST /api/v1/market-idea/save-market-idea`

DB lifecycle:

- `beforeAll`: `setupIntegration()` for real `PrismaService`
- `beforeEach`: `resetDatabase(prisma)` for isolation
- `afterAll`: `app.close()` then `stopContainer()`

Data setup:

- Factories via `new UserFactory().inDatabase(prisma)`
- Direct Prisma writes for edge cases
- Local seed helpers for complex scenarios

## E2E Tests

- Test user/platform journeys, NOT implementation details or multiple journeys at once
- Isolate tests and clean up created records/state
- Focus only on happy paths
- Always verify the state of the system (e.g. market seeded with the correct state)

## Core Guidelines

Follow AAA pattern with visible comments:

```ts
it('should...', () => {
  // Arrange
  code;

  // Act
  code;

  // Assert
  code;
});
```

Test design:

- Use one key assertion per test. Supporting expects for the same behavior are fine.
- Split tests when the name joins unrelated behaviors with "and".
- Keep tests focused on one aspect with no complex test logic
- Test behavior, not internal details
- Prefer stubs over mocks (call count assertions are usually internal details)
- Only mock when necessary; stay close to production behavior
- Reset mocks/globals in `beforeEach` (or enable jest's `restoreMocks`/`clearMocks`)
- Use realistic data (real names, plausible values)
- Keep test files under 700 lines. Longer files often mean the service needs refactoring or the test belongs at a higher level.
- Tests are documentation; a new dev should understand behavior by reading them

Snapshots:

- Only snapshot when expected structure is clear; overuse makes tests brittle
- Use snapshots for shape/structure, screenshots for visual styling

Code smells:

- Warn if code needs >5 params or mocks to test (SRP violation)
- Test or service file is over 700 lines
- Factor recurrent logic into focused helper functions (no giant `prepare` functions)
- Verify tests actually fail when the tested condition is removed

## Property-Based Testing

**When to use property-based testing:**

- Invariants that must hold for all inputs (e.g., "sorted output is always sorted")
- Edge case detection (especially math)
- Function focuses on transforming strings & integers
- Control random values instead of using uncontrolled randomness

Property-based and example-based tests are complementary. Don't try to cover 100% with properties; use both.

```ts
fc.assert(
  fc.property(fc.string(), fc.string(), fc.string(), (a, b, c) => {
    expect(isSubstring(a + b + c, b)).toBe(true);
  }),
);
```

Data:

- Avoid hardcoded values for unused fields
- Avoid unexplained magic strings/numbers in expected behavior

Use `fast-check` for unused fields instead of hardcoding:

```ts
fc.assert(
  fc.property(fc.string(), (randomName) => {
    const user: User = { name: randomName, birthday: '2010-02-03' };
    expect(computeAge(user)).toBeGreaterThan(0);
  }),
);
```

Time and locale dependencies:

Stub time with `jest.useFakeTimers()` + `jest.setSystemTime`. Combine with fast-check to fuzz dates:

```ts
it('should compute positive age for any date after birthday', () => {
  jest.useFakeTimers();
  fc.assert(
    fc.property(fc.date({ min: new Date('2010-02-04') }), (today) => {
      jest.setSystemTime(today);
      const user: User = { name: 'Alice', birthday: '2010-02-03' };
      expect(computeAge(user)).toBeGreaterThan(0);
    }),
  );
  jest.useRealTimers();
});
```

Extract complex logic from components into testable functions. Don't test trivial zero-complexity logic.

Input generation:

- Construct inputs where you know the expected outcome; don't regenerate the algorithm logic
- Assert characteristics of return values, not exact values
- Never add constraints (`maxLength`, `min`, `max`) unless required by the algorithm
- Use `size: '-1'` only if algorithm is slow on large inputs

Prefer built-in options over `filter`/`pre`:

```ts
fc.string({ minLength: 2 });
fc.integer({ min: 1 });
fc.nat();
```

Use `map` over `filter` when possible:

```ts
fc.nat().map((value) => value * 2);
fc.tuple(fc.string(), fc.string()).map(([prefix, suffix]) => `${prefix}A${suffix}`);
```

Classical property patterns:

1. Input-independent characteristics: `Math.floor(d)` is always integer
2. Input-derived characteristics: `average(a, b)` is between a and b
3. Constructed inputs with known outcomes: `a + b + c` contains `b`
4. Round-trip/inverse: `unzip(zip(file))` equals original
5. Oracle comparison: binary search matches linear search

## Race Conditions

Use `fc.scheduler()` to test async code with variable resolution order:

```ts
it('should resolve in call order', async () => {
  await fc.assert(
    fc.asyncProperty(fc.scheduler(), async (scheduler) => {
      const seenAnswers: number[] = [];
      const call = jest.fn().mockImplementation((value: number) => Promise.resolve(value));
      const queued = queue(scheduler.scheduleFunction(call));

      await scheduler.waitFor(Promise.all([queued(1).then((v) => seenAnswers.push(v)), queued(2).then((v) => seenAnswers.push(v))]));

      expect(seenAnswers).toEqual([1, 2]);
    }),
  );
});
```

## Factories With fast-check

This repo uses builder-pattern factories in `packages/db/test/factories/`. To get shrinkable, reproducible property-based tests, wrap factories with fast-check arbitraries that control the variable inputs:

```ts
import fc from 'fast-check';

import { UserFactory } from '@factmachine/db/test/factories/user.factory';

const userArb = fc
  .record({
    username: fc.string({ minLength: 1, maxLength: 20 }),
    walletAddress: fc.hexaString({ minLength: 44, maxLength: 44 }),
  })
  .chain(({ username, walletAddress }) => fc.constant(new UserFactory().withUsername(username).withWalletAddress(walletAddress).build()));
```

This keeps shrinking on primitive values while reusing factory defaults and consistency. For async factories that hit the DB, use `fc.asyncProperty`:

```ts
const dbUserArb = fc
  .record({
    username: fc.string({ minLength: 1, maxLength: 20 }),
  })
  .chain(({ username }) => fc.constant(() => new UserFactory().withUsername(username).inDatabase(prisma)));

await fc.assert(
  fc.asyncProperty(dbUserArb, async (createUser) => {
    const user = await createUser();
    // test with user
  }),
);
```

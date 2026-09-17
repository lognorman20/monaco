# M5-T24 screen snapshots

Host-runnable snapshot regression tests live in `packages/mobile-core`:

- `ScreenSnapshotRenderer` — deterministic text layout from fixture DTOs
- `ScreenSnapshotTests` — `testGroupScreen_snapshot_matchesFixture`, `testAppHome_snapshot_matchesFixture`

Run: `just test mobile`

Golden fixtures: `packages/mobile-core/Tests/MonacoCoreTests/Fixtures/*_snapshot.txt`

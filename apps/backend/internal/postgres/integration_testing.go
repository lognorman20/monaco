package postgres

// Parallel-safe integration helpers: PrepareTestDB, PrepareIntegrationDB, and TestIsolation
// in test_isolation.go. Each test tracks its own user/group roots and cleans up via t.Cleanup.

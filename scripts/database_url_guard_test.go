package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runDatabaseURLGuard(t *testing.T, databaseURL string) error {
	t.Helper()

	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "assert-local-database-url.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATABASE_URL="+databaseURL)
	return cmd.Run()
}

func TestDatabaseURLGuard_rejectsHostedSupabaseUrl(t *testing.T) {
	// Arrange
	databaseURL := "postgres://user:pass@abc.supabase.co:5432/postgres"

	// Act
	err := runDatabaseURLGuard(t, databaseURL)

	// Assert
	if err == nil {
		t.Fatal("expected error for hosted Supabase DATABASE_URL")
	}
}

func TestDatabaseURLGuard_acceptsLocalhostComposeUrl(t *testing.T) {
	// Arrange
	databaseURL := "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable"

	// Act
	err := runDatabaseURLGuard(t, databaseURL)

	// Assert
	if err != nil {
		t.Fatalf("expected localhost DATABASE_URL to pass guard: %v", err)
	}
}

func TestDeriveTestDatabaseURL_appendsTestSuffix(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "derive-test-database-url.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATABASE_URL=postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("derive-test-database-url.sh: %v", err)
	}
	got := string(out)
	want := "postgres://monaco:monaco@localhost:54322/monaco_test?sslmode=disable\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

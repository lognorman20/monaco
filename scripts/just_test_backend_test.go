package scripts_test

import (
	"os"
	"os/exec"
	"testing"
)

func TestJustTestBackend_exitsZeroOnCleanClone(t *testing.T) {
	if testing.Short() {
		t.Skip("just smoke test skipped in short mode")
	}

	// Arrange
	root := repoRoot(t)
	if _, err := exec.LookPath("just"); err != nil {
		t.Skip("just is not on PATH")
	}

	cmd := exec.Command("just", "test", "backend")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SKIP_SCRIPTS_TESTS=1")

	// Act
	output, err := cmd.CombinedOutput()

	// Assert
	if err != nil {
		t.Fatalf("just test backend failed: %v\n%s", err, output)
	}
}

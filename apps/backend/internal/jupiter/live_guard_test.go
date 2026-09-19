package jupiter

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoProductionJupiterURLInBackendTestFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasSuffix(path, "live_guard_test.go") {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if strings.Contains(text, "jup.ag") {
			t.Errorf("%s references jup.ag; use NewFakeClient or httptest stub via NewHTTPClientWithBaseURL", path)
		}
		if strings.Contains(text, "jupiter.NewHTTPClient()") {
			t.Errorf("%s calls jupiter.NewHTTPClient(); use NewFakeClient or NewHTTPClientWithBaseURL(httptest)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend tests: %v", err)
	}
}

func TestLiveJupiterHTTPBlockedDuringGoTest(t *testing.T) {
	t.Parallel()

	client := NewHTTPClient()
	_, err := client.QuoteBuy(context.Background(), QuoteBuyParams{
		GroupID:    "group-guard",
		UserID:     "user-guard",
		Symbol:     "AAPLx",
		OutputMint: AAPLxMint,
		USDCAmount: 1_000_000,
	})
	if err == nil {
		t.Fatal("expected live Jupiter HTTP to be blocked during go test")
	}
	if !strings.Contains(err.Error(), "blocked during go test") {
		t.Fatalf("QuoteBuy() error = %v, want live API block", err)
	}
}

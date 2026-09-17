package pyth

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

func TestNewHermesClientFromConfig_missingAPIKey_returnsError(t *testing.T) {
	// Arrange
	cfg := &config.Config{}

	// Act
	_, err := NewHermesClientFromConfig(cfg)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing PYTH_API_KEY")
	}
}

func TestNewHermesClientFromConfig_withAPIKey_returnsClient(t *testing.T) {
	// Arrange
	cfg := &config.Config{PythAPIKey: "test-pyth-key"}

	// Act
	client, err := NewHermesClientFromConfig(cfg)

	// Assert
	if err != nil {
		t.Fatalf("NewHermesClientFromConfig: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

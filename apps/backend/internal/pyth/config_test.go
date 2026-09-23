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

func TestNewMarketDataClientFromConfig_buildsBenchmarksWithoutAKey(t *testing.T) {
	client := NewMarketDataClientFromConfig(&config.Config{})
	if client.HasAPIKey() {
		t.Fatal("no key was configured")
	}
	source, ok := client.seriesSource.(*BenchmarksClient)
	if !ok || source.baseURL != defaultBenchmarksBaseURL {
		t.Fatalf("series source = %#v, want Benchmarks on the public host", client.seriesSource)
	}
}

func TestNewMarketDataClientFromConfig_honoursBothOverrides(t *testing.T) {
	client := NewMarketDataClientFromConfig(&config.Config{
		PythAPIKey:            "test-pyth-key",
		PythHermesBaseURL:     "https://example.test/hermes",
		PythBenchmarksBaseURL: "https://example.test/benchmarks",
	})
	if !client.HasAPIKey() || client.baseURL != "https://example.test/hermes" {
		t.Fatalf("hermes = %q key=%v", client.baseURL, client.HasAPIKey())
	}
	source, ok := client.seriesSource.(*BenchmarksClient)
	if !ok || source.baseURL != "https://example.test/benchmarks" {
		t.Fatalf("series source = %#v", client.seriesSource)
	}
}

func TestNewHermesClientFromConfig_withBaseURLOverride_usesCustomHost(t *testing.T) {
	cfg := &config.Config{
		PythAPIKey:        "test-pyth-key",
		PythHermesBaseURL: "https://example.test/hermes",
	}

	client, err := NewHermesClientFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewHermesClientFromConfig: %v", err)
	}
	if client.baseURL != "https://example.test/hermes" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
}

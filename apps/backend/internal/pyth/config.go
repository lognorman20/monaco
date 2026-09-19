package pyth

import (
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

// NewHermesClientFromConfig returns an authenticated Hermes client for marked pot NAV.
func NewHermesClientFromConfig(cfg *config.Config) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.PythAPIKey == "" {
		return nil, fmt.Errorf("PYTH_API_KEY is required for Pyth marks")
	}
	return NewHermesClientWithBaseURL(cfg.PythHermesBaseURL, cfg.PythAPIKey), nil
}

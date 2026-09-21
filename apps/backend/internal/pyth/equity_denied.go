package pyth

import (
	"strings"
	"sync"
)

var (
	equityDeniedMu sync.Mutex
	equityDenied   bool
)

func equityFeedsDenied() bool {
	equityDeniedMu.Lock()
	defer equityDeniedMu.Unlock()
	return equityDenied
}

func markEquityDenied(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "not entitled") {
		return false
	}
	equityDeniedMu.Lock()
	equityDenied = true
	equityDeniedMu.Unlock()
	return true
}

func resetEquityDeniedForTest() {
	equityDeniedMu.Lock()
	equityDenied = false
	equityDeniedMu.Unlock()
}

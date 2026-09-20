// Package telemetrytest reads metric values in tests of packages that record telemetry.
package telemetrytest

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// Value returns the current value of one series, written as it appears in the exposition
// without the value, e.g. `monaco_money_events_total{event="swap_buy",outcome="ok"}`.
// A series that has never been touched reads 0. Labels are in alphabetical order.
//
// Metrics are process-global: compare a delta around the code under test, and do not call
// t.Parallel in a test that does, so no other test moves the counter in between.
func Value(t *testing.T, series string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		rest, found := strings.CutPrefix(line, series+" ")
		if !found {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			t.Fatalf("parse metric line %q: %v", line, err)
		}
		return value
	}
	return 0
}

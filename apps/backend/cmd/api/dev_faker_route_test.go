package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/httpapi"
)

func TestDevFakerRouteOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		mux := http.NewServeMux()
		routes := registerDevFakerRoute(mux, &httpapi.DevFakerHandlers{Enabled: enabled}, []string{"GET /health"})
		listed := false
		for _, route := range routes {
			if route == "POST /v1/dev/faker" {
				listed = true
			}
		}
		_, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, "/v1/dev/faker", nil))
		registered := pattern != ""
		if listed != enabled || registered != enabled {
			t.Fatalf("enabled=%v: listed=%v registered=%v", enabled, listed, registered)
		}
	}
}

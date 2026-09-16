package httpapi

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_returns200AndOkBody(t *testing.T) {
	// Arrange
	handler := newTestHealthHandler()
	req := testHTTPRequest("GET", "/health")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", payload["status"])
	}
}

func TestHealthHandler_setsContentTypeJson(t *testing.T) {
	// Arrange
	handler := newTestHealthHandler()
	req := testHTTPRequest("GET", "/health")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

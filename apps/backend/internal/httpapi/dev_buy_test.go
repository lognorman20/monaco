package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPOST_devBuy_withoutDevFlag_returns404(t *testing.T) {
	// Arrange
	t.Setenv("DEV_BUY_ENABLED", "")
	handlers := &DevBuyHandlers{}
	req := httptest.NewRequest(http.MethodPost, "/v1/dev/groups/group-1/buy", strings.NewReader(`{"symbol":"AAPLx","usdc":1000000}`))
	req.SetPathValue("id", "group-1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fixture-session-token")
	rec := httptest.NewRecorder()

	// Act
	handlers.DevBuyHandler(rec, req)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

package httpapi

import (
	"net/http"
	"net/http/httptest"
)

func newTestHealthHandler() http.HandlerFunc {
	return HealthHandler
}

func testHTTPRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

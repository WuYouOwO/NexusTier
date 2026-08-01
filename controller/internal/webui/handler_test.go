package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesDashboardAndAssets(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/", "text/html", "NexusTier"},
		{"/app.css", "text/css", ":root"},
		{"/app.js", "text/javascript", "react"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			if !strings.Contains(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("content type = %q, want %q", response.Header().Get("Content-Type"), test.contentType)
			}
			if !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("body does not contain %q", test.contains)
			}
			if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("missing security headers: %v", response.Header())
			}
		})
	}
}

func TestHandlerDoesNotFallbackUnknownRoutesToDashboard(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	response := httptest.NewRecorder()

	Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

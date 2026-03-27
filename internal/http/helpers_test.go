package apihttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestWriteErrorMapsRateLimitedToTooManyRequests(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeError(recorder, fmt.Errorf("%w: all configured Gemini models are unavailable", domain.ErrRateLimited))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error message in response body, got %v", payload)
	}
}

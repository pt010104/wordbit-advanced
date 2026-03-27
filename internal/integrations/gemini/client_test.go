package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

func TestClientRotatesAfter429AndSkipsCooledDownModel(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model := modelFromPath(r.URL.Path)
		counts[model]++

		switch model {
		case "model-a":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(rateLimitBody("30s")))
		case "model-b":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(candidateResponseBody("climate")))
		default:
			t.Fatalf("unexpected model %q", model)
		}
	}))
	defer server.Close()

	now := time.Date(2026, 3, 27, 1, 33, 0, 0, time.UTC)
	client := newTestClient(server.URL, []string{"model-a", "model-b"}, 0, 0, 1)
	client.now = func() time.Time { return now }

	input := service.GenerationInput{
		RequestedCount: 1,
		CEFRLevel:      domain.CEFRB1,
		Topic:          "Environment",
	}

	candidates, _, err := client.GenerateCandidates(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateCandidates() first call error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Word != "climate" {
		t.Fatalf("unexpected first candidates: %+v", candidates)
	}

	candidates, _, err = client.GenerateCandidates(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateCandidates() second call error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Word != "climate" {
		t.Fatalf("unexpected second candidates: %+v", candidates)
	}

	if counts["model-a"] != 1 {
		t.Fatalf("expected model-a to be hit once before cooldown skip, got %d", counts["model-a"])
	}
	if counts["model-b"] != 2 {
		t.Fatalf("expected model-b to handle both successful calls, got %d", counts["model-b"])
	}
}

func TestClientReturnsRateLimitedWhenAllModelsUnavailable(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model := modelFromPath(r.URL.Path)
		counts[model]++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(rateLimitBody("45s")))
	}))
	defer server.Close()

	client := newTestClient(server.URL, []string{"model-a", "model-b"}, 0, 0, 1)
	client.now = func() time.Time { return time.Date(2026, 3, 27, 1, 33, 0, 0, time.UTC) }

	_, _, err := client.GenerateCandidates(context.Background(), service.GenerationInput{
		RequestedCount: 1,
		CEFRLevel:      domain.CEFRB1,
		Topic:          "Environment",
	})
	if err == nil {
		t.Fatal("expected rate-limited error, got nil")
	}
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if counts["model-a"] != 1 || counts["model-b"] != 1 {
		t.Fatalf("expected one attempt per model, got counts=%v", counts)
	}
}

func TestClientRetriesServerErrorsOnSameModel(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model := modelFromPath(r.URL.Path)
		counts[model]++
		if counts[model] < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(candidateResponseBody("ecosystem")))
	}))
	defer server.Close()

	client := newTestClient(server.URL, []string{"model-a"}, 0, 0, 3)

	candidates, _, err := client.GenerateCandidates(context.Background(), service.GenerationInput{
		RequestedCount: 1,
		CEFRLevel:      domain.CEFRB1,
		Topic:          "Environment",
	})
	if err != nil {
		t.Fatalf("GenerateCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Word != "ecosystem" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
	if counts["model-a"] != 3 {
		t.Fatalf("expected same model to be retried 3 times, got %d", counts["model-a"])
	}
}

func TestClientSkipsModelWhenLocalRPMLimitReached(t *testing.T) {
	t.Parallel()

	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model := modelFromPath(r.URL.Path)
		counts[model]++
		word := "climate"
		if model == "model-b" {
			word = "habitat"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(candidateResponseBody(word)))
	}))
	defer server.Close()

	now := time.Date(2026, 3, 27, 1, 33, 0, 0, time.UTC)
	client := newTestClient(server.URL, []string{"model-a", "model-b"}, 1, 0, 1)
	client.now = func() time.Time { return now }

	input := service.GenerationInput{
		RequestedCount: 1,
		CEFRLevel:      domain.CEFRB1,
		Topic:          "Environment",
	}

	first, _, err := client.GenerateCandidates(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateCandidates() first call error = %v", err)
	}
	second, _, err := client.GenerateCandidates(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateCandidates() second call error = %v", err)
	}

	if first[0].Word != "climate" {
		t.Fatalf("expected first request to use model-a, got %+v", first)
	}
	if second[0].Word != "habitat" {
		t.Fatalf("expected second request to rotate to model-b after local RPM exhaustion, got %+v", second)
	}
	if counts["model-a"] != 1 || counts["model-b"] != 1 {
		t.Fatalf("expected local RPM limit to prevent a second model-a request, got counts=%v", counts)
	}
}

func newTestClient(baseURL string, models []string, rpmLimit int, rpdLimit int, maxRetries int) *Client {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewClient(config.GeminiConfig{
		BaseURL:         baseURL,
		Models:          models,
		APIKey:          "test-key",
		Timeout:         time.Second,
		MaxRetries:      maxRetries,
		Temperature:     0.4,
		MaxOutputTokens: 1024,
		RPMLimit:        rpmLimit,
		RPDLimit:        rpdLimit,
	}, logger)
	client.sleep = func(time.Duration) {}
	return client
}

func modelFromPath(path string) string {
	path = strings.TrimPrefix(path, "/models/")
	path = strings.TrimSuffix(path, ":generateContent")
	return path
}

func candidateResponseBody(word string) string {
	inner := fmt.Sprintf(`{"words":[{"word":"%s","canonical_form":"%s","lemma":"%s","level":"B1","topic":"Environment","english_meaning":"meaning","vietnamese_meaning":"nghia"}]}`,
		word, word, word,
	)
	return fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":%q}]}}]}`, inner)
}

func rateLimitBody(retryDelay string) string {
	return fmt.Sprintf(`{"error":{"code":429,"message":"Quota exceeded","details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaMetric":"generativelanguage.googleapis.com/generate_content_requests","quotaId":"GenerateRequestsPerMinute"}]},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":%q}]}}`, retryDelay)
}

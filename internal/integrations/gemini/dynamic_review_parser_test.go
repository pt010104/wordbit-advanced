package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestParseDynamicReviewGenerateResponse(t *testing.T) {
	t.Parallel()

	wordID := uuid.New()
	payload := domain.DynamicReviewPromptBatchPayload{
		Items: []domain.DynamicReviewPromptBatchItem{{
			WordID:     wordID,
			ReviewMode: domain.ReviewModeMultipleChoice,
			PromptText: "Which word best describes using influence to gain business advantages?",
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	parsed, raw, err := parseDynamicReviewGenerateResponse(testExerciseEnvelope(t, string(body)))
	if err != nil {
		t.Fatalf("parseDynamicReviewGenerateResponse() error = %v", err)
	}
	if len(parsed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(parsed.Items))
	}
	if parsed.Items[0].WordID != wordID {
		t.Fatalf("expected word id %s, got %s", wordID, parsed.Items[0].WordID)
	}
	if parsed.Items[0].PromptText == "" {
		t.Fatalf("expected non-empty prompt text")
	}
	if !strings.Contains(raw, wordID.String()) {
		t.Fatalf("expected raw payload to contain word id")
	}
}

func TestParseDynamicReviewGenerateResponseRejectsMissingPrompt(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"items": []map[string]any{{
			"word_id":     uuid.New().String(),
			"review_mode": "fill_in_blank",
			"prompt_text": "",
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	_, _, err = parseDynamicReviewGenerateResponse(testExerciseEnvelope(t, string(body)))
	if err == nil {
		t.Fatalf("expected error for blank prompt_text")
	}
}

func TestParseDynamicReviewGenerateResponseRejectsUnsupportedMode(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"items": []map[string]any{{
			"word_id":     uuid.New().String(),
			"review_mode": "hidden_meaning",
			"prompt_text": "Prompt text",
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	_, _, err = parseDynamicReviewGenerateResponse(testExerciseEnvelope(t, string(body)))
	if err == nil {
		t.Fatalf("expected error for unsupported review mode")
	}
}

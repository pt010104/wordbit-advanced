package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func parseDynamicReviewGenerateResponse(body []byte) (domain.DynamicReviewPromptBatchPayload, string, error) {
	clean, err := extractGenerateJSONText(body)
	if err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, "", err
	}

	var payload domain.DynamicReviewPromptBatchPayload
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, clean, fmt.Errorf("decode dynamic review payload: %w", err)
	}
	if err := validateDynamicReviewPayloadShape(payload); err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, clean, err
	}
	return payload, clean, nil
}

func validateDynamicReviewPayloadShape(payload domain.DynamicReviewPromptBatchPayload) error {
	if len(payload.Items) == 0 {
		return fmt.Errorf("dynamic review payload missing items")
	}
	for index, item := range payload.Items {
		prefix := fmt.Sprintf("item %d", index+1)
		if item.WordID == uuid.Nil {
			return fmt.Errorf("%s missing word_id", prefix)
		}
		switch item.ReviewMode {
		case domain.ReviewModeMultipleChoice, domain.ReviewModeFillBlank:
		default:
			return fmt.Errorf("%s has unsupported review_mode", prefix)
		}
		if strings.TrimSpace(item.PromptText) == "" {
			return fmt.Errorf("%s missing prompt_text", prefix)
		}
	}
	return nil
}

package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"wordbit-advanced-app/backend/internal/domain"
)

func parseMode4WeakPassageGenerateResponse(body []byte) (domain.Mode4WeakPassagePayload, string, error) {
	clean, err := extractGenerateJSONText(body)
	if err != nil {
		return domain.Mode4WeakPassagePayload{}, "", err
	}

	var payload domain.Mode4WeakPassagePayload
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return domain.Mode4WeakPassagePayload{}, clean, fmt.Errorf("decode mode4 payload: %w", err)
	}
	if err := validateMode4WeakPassagePayloadShape(payload); err != nil {
		return domain.Mode4WeakPassagePayload{}, clean, err
	}
	return payload, clean, nil
}

func validateMode4WeakPassagePayloadShape(payload domain.Mode4WeakPassagePayload) error {
	if strings.TrimSpace(payload.PlainPassageText) == "" {
		return fmt.Errorf("mode4 payload missing plain_passage_text")
	}
	if strings.TrimSpace(payload.MarkedPassageMarkdown) == "" {
		return fmt.Errorf("mode4 payload missing marked_passage_markdown")
	}
	return nil
}

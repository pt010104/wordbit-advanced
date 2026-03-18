package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"wordbit-advanced-app/backend/internal/domain"
)

func parseGenerateResponse(body []byte) ([]domain.CandidateWord, string, error) {
	var response generateResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("decode gemini envelope: %w", err)
	}
	if len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
		return nil, "", fmt.Errorf("gemini response had no text candidates")
	}
	var textBuilder strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		textBuilder.WriteString(part.Text)
	}
	text := textBuilder.String()
	clean := stripCodeFences(text)

	var direct []domain.CandidateWord
	if err := json.Unmarshal([]byte(clean), &direct); err == nil {
		return direct, clean, nil
	}

	var wrapped struct {
		Words      []domain.CandidateWord `json:"words"`
		Candidates []domain.CandidateWord `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(clean), &wrapped); err != nil {
		return nil, clean, fmt.Errorf("decode gemini payload: %w", err)
	}
	if len(wrapped.Words) > 0 {
		return wrapped.Words, clean, nil
	}
	if len(wrapped.Candidates) > 0 {
		return wrapped.Candidates, clean, nil
	}
	return nil, clean, fmt.Errorf("gemini payload did not include words")
}

func stripCodeFences(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```JSON")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	return string(bytes.TrimSpace([]byte(value)))
}

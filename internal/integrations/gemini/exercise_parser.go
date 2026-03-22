package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"wordbit-advanced-app/backend/internal/domain"
)

func parseExerciseGenerateResponse(body []byte) (domain.ContextExercisePayload, string, error) {
	clean, err := extractGenerateJSONText(body)
	if err != nil {
		return domain.ContextExercisePayload{}, "", err
	}

	var payload domain.ContextExercisePayload
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return domain.ContextExercisePayload{}, clean, fmt.Errorf("decode exercise payload: %w", err)
	}
	if err := validateExercisePayloadShape(payload); err != nil {
		return domain.ContextExercisePayload{}, clean, err
	}
	return payload, clean, nil
}

func validateExercisePayloadShape(payload domain.ContextExercisePayload) error {
	if strings.TrimSpace(payload.Topic) == "" {
		return fmt.Errorf("exercise payload missing topic")
	}
	if strings.TrimSpace(payload.Title) == "" {
		return fmt.Errorf("exercise payload missing title")
	}
	if strings.TrimSpace(payload.Passage) == "" {
		return fmt.Errorf("exercise payload missing passage")
	}
	if len(payload.ClusterWords) == 0 {
		return fmt.Errorf("exercise payload missing cluster_words")
	}
	if len(payload.Questions) == 0 {
		return fmt.Errorf("exercise payload missing questions")
	}
	for index, question := range payload.Questions {
		prefix := fmt.Sprintf("question %d", index+1)
		if strings.TrimSpace(question.ID) == "" {
			return fmt.Errorf("%s missing id", prefix)
		}
		if strings.TrimSpace(question.TargetWord) == "" {
			return fmt.Errorf("%s missing target_word", prefix)
		}
		if strings.TrimSpace(question.Prompt) == "" {
			return fmt.Errorf("%s missing prompt", prefix)
		}
		if strings.TrimSpace(question.Explanation) == "" {
			return fmt.Errorf("%s missing explanation", prefix)
		}
		if len(question.Options) != 4 {
			return fmt.Errorf("%s must contain exactly 4 options", prefix)
		}
		matches := 0
		for _, option := range question.Options {
			if option == question.Answer {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s answer must match exactly one option", prefix)
		}
	}
	return nil
}

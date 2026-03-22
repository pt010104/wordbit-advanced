package gemini

import (
	"encoding/json"
	"testing"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestParseExerciseGenerateResponseParsesStrictJSON(t *testing.T) {
	t.Parallel()

	payload := `{"pack_id":"","topic":"Business","cefr_level":"B2","pack_type":"context_cluster_challenge","cluster_words":["allocate","forecast","revenue","strategy"],"title":"Quarterly Planning","passage":"The team reviewed the quarter.","questions":[{"id":"q1","type":"best_fit","target_word":"allocate","prompt":"Choose the best word.","options":["allocate","forecast","revenue","strategy"],"answer":"allocate","explanation":"Allocate means assign resources."},{"id":"q2","type":"meaning_match","target_word":"forecast","prompt":"Which word means prediction?","options":["forecast","allocate","revenue","strategy"],"answer":"forecast","explanation":"Forecast is a prediction."},{"id":"q3","type":"sentence_usage","target_word":"revenue","prompt":"Which sentence is correct?","options":["Revenue increased last quarter.","They revenue a deal.","We revenue the budget.","Revenue with the vendor."],"answer":"Revenue increased last quarter.","explanation":"Revenue is a noun."},{"id":"q4","type":"definition_match","target_word":"strategy","prompt":"Which word means a long-term plan?","options":["revenue","strategy","forecast","allocate"],"answer":"strategy","explanation":"Strategy means a plan."}],"summary_tip":"These words often appear in planning meetings."}`

	parsed, raw, err := parseExerciseGenerateResponse(testExerciseEnvelope(t, payload))
	if err != nil {
		t.Fatalf("parseExerciseGenerateResponse() error = %v", err)
	}
	if parsed.Topic != "Business" {
		t.Fatalf("expected topic Business, got %q", parsed.Topic)
	}
	if parsed.PackType != domain.ExercisePackTypeContextClusterChallenge {
		t.Fatalf("expected context_cluster_challenge, got %q", parsed.PackType)
	}
	if len(parsed.Questions) != 4 {
		t.Fatalf("expected 4 questions, got %d", len(parsed.Questions))
	}
	if raw == "" {
		t.Fatal("expected raw payload text")
	}
}

func TestParseExerciseGenerateResponseCleansWrappedJSON(t *testing.T) {
	t.Parallel()

	payload := "Here is your pack:\n```json\n" +
		`{"pack_id":"","topic":"Work","cefr_level":"B2","pack_type":"context_cluster_challenge","cluster_words":["allocate","forecast","revenue","strategy"],"title":"Project Brief","passage":"The project team aligned on a new plan.","questions":[{"id":"q1","type":"best_fit","target_word":"allocate","prompt":"Pick the best fit.","options":["allocate","forecast","revenue","strategy"],"answer":"allocate","explanation":"Allocate means assign."},{"id":"q2","type":"meaning_match","target_word":"forecast","prompt":"Which word means prediction?","options":["forecast","allocate","revenue","strategy"],"answer":"forecast","explanation":"Forecast is a prediction."},{"id":"q3","type":"sentence_usage","target_word":"revenue","prompt":"Which sentence is correct?","options":["Revenue increased this year.","They revenue the plan.","We revenue the budget.","Revenue with the client."],"answer":"Revenue increased this year.","explanation":"Revenue is money earned."},{"id":"q4","type":"definition_match","target_word":"strategy","prompt":"Which word means a long-term plan?","options":["revenue","strategy","forecast","allocate"],"answer":"strategy","explanation":"Strategy means a plan."}],"summary_tip":"Focus on how these business words work together."}` +
		"\n```"

	parsed, _, err := parseExerciseGenerateResponse(testExerciseEnvelope(t, payload))
	if err != nil {
		t.Fatalf("parseExerciseGenerateResponse() error = %v", err)
	}
	if parsed.Title != "Project Brief" {
		t.Fatalf("expected cleaned JSON title, got %q", parsed.Title)
	}
}

func TestParseExerciseGenerateResponseRejectsInvalidQuestionShape(t *testing.T) {
	t.Parallel()

	payload := `{"pack_id":"","topic":"Work","cefr_level":"B2","pack_type":"context_cluster_challenge","cluster_words":["allocate","forecast","revenue","strategy"],"title":"Project Brief","passage":"The project team aligned on a new plan.","questions":[{"id":"q1","type":"best_fit","target_word":"allocate","prompt":"","options":["allocate","forecast","revenue","strategy"],"answer":"allocate","explanation":"Allocate means assign."}],"summary_tip":"Focus on business words."}`

	_, _, err := parseExerciseGenerateResponse(testExerciseEnvelope(t, payload))
	if err == nil {
		t.Fatal("expected parse error for invalid question shape")
	}
}

func TestParseExerciseGenerateResponseRejectsWrongAnswerCardinality(t *testing.T) {
	t.Parallel()

	payload := `{"pack_id":"","topic":"Work","cefr_level":"B2","pack_type":"context_cluster_challenge","cluster_words":["allocate","forecast","revenue","strategy"],"title":"Project Brief","passage":"The project team aligned on a new plan.","questions":[{"id":"q1","type":"best_fit","target_word":"allocate","prompt":"Pick the best fit.","options":["allocate","forecast","revenue"],"answer":"allocate","explanation":"Allocate means assign."}],"summary_tip":"Focus on business words."}`

	_, _, err := parseExerciseGenerateResponse(testExerciseEnvelope(t, payload))
	if err == nil {
		t.Fatal("expected parse error for invalid answer/options cardinality")
	}
}

func testExerciseEnvelope(t *testing.T, text string) []byte {
	t.Helper()

	type envelope struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	body := envelope{
		Candidates: []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}{
			{
				Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{
						{Text: text},
					},
				},
			},
		},
	}
	bytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return bytes
}

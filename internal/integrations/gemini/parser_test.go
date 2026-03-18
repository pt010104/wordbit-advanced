package gemini

import "testing"

func TestParseGenerateResponseWithFenceWrappedJSON(t *testing.T) {
	t.Parallel()

	body := []byte("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"```json\\n{\\\"words\\\":[{\\\"word\\\":\\\"sustain\\\",\\\"canonical_form\\\":\\\"sustain\\\",\\\"lemma\\\":\\\"sustain\\\",\\\"level\\\":\\\"B2\\\",\\\"topic\\\":\\\"Environment\\\",\\\"english_meaning\\\":\\\"maintain\\\",\\\"vietnamese_meaning\\\":\\\"duy trì\\\"}]}\\n```\"}]}}]}")

	words, raw, err := parseGenerateResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(words))
	}
	if raw == "" || words[0].Word != "sustain" {
		t.Fatalf("unexpected parse result: raw=%q word=%q", raw, words[0].Word)
	}
}

func TestParseGenerateResponseWithMultipleParts(t *testing.T) {
	t.Parallel()

	body := []byte("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"{\\\"words\\\":[{\\\"word\\\":\\\"sustain\\\",\"},{\"text\":\"\\\"canonical_form\\\":\\\"sustain\\\",\\\"lemma\\\":\\\"sustain\\\",\\\"level\\\":\\\"B2\\\",\\\"topic\\\":\\\"Environment\\\",\\\"english_meaning\\\":\\\"maintain\\\",\\\"vietnamese_meaning\\\":\\\"duy trì\\\"}]}\"}]}}]}")

	words, raw, err := parseGenerateResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("expected 1 word, got %d", len(words))
	}
	if raw == "" || words[0].Word != "sustain" {
		t.Fatalf("unexpected parse result: raw=%q word=%q", raw, words[0].Word)
	}
}

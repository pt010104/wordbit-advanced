package gemini

import "testing"

func TestParseMode4WeakPassageGenerateResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "text": "{\"plain_passage_text\":\"Alpha met beta before gamma.\",\"marked_passage_markdown\":\"**alpha** met **beta** before **gamma**.\"}"
          }
        ]
      }
    }
  ]
}`)

	payload, raw, err := parseMode4WeakPassageGenerateResponse(body)
	if err != nil {
		t.Fatalf("parseMode4WeakPassageGenerateResponse() error = %v", err)
	}
	if payload.PlainPassageText == "" || payload.MarkedPassageMarkdown == "" {
		t.Fatalf("expected parsed payload fields, got %+v", payload)
	}
	if raw == "" {
		t.Fatalf("expected extracted raw json text")
	}
}

func TestParseMode4WeakPassageGenerateResponseRejectsMissingFields(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "text": "{\"plain_passage_text\":\"\",\"marked_passage_markdown\":\"\"}"
          }
        ]
      }
    }
  ]
}`)

	if _, _, err := parseMode4WeakPassageGenerateResponse(body); err == nil {
		t.Fatalf("expected missing fields validation error")
	}
}

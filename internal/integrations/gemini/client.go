package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

type Client struct {
	baseURL         string
	model           string
	apiKey          string
	timeout         time.Duration
	maxRetries      int
	temperature     float64
	maxOutputTokens int
	logger          *slog.Logger
	httpClient      *http.Client
}

func NewClient(cfg config.GeminiConfig, logger *slog.Logger) *Client {
	return &Client{
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		model:           cfg.Model,
		apiKey:          cfg.APIKey,
		timeout:         cfg.Timeout,
		maxRetries:      cfg.MaxRetries,
		temperature:     cfg.Temperature,
		maxOutputTokens: cfg.MaxOutputTokens,
		logger:          logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

type generateRequest struct {
	SystemInstruction contentBlock     `json:"systemInstruction"`
	Contents          []contentBlock   `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type contentBlock struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature      float64 `json:"temperature"`
	ResponseMimeType string  `json:"responseMimeType"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

func (c *Client) GenerateCandidates(ctx context.Context, input service.GenerationInput) ([]domain.CandidateWord, string, error) {
	body := generateRequest{
		SystemInstruction: contentBlock{
			Parts: []part{{Text: systemInstruction}},
		},
		Contents: []contentBlock{{
			Role:  "user",
			Parts: []part{{Text: buildPrompt(input)}},
		}},
		GenerationConfig: generationConfig{
			Temperature:      c.temperature,
			ResponseMimeType: "application/json",
			MaxOutputTokens:  c.maxOutputTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("marshal gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)
	var lastErr error
	for attempt := 1; attempt <= maxInt(c.maxRetries, 1); attempt++ {
		start := time.Now()
		c.logger.Info("gemini generate request",
			"model", c.model,
			"attempt", attempt,
			"requested_count", input.RequestedCount,
			"timeout_ms", c.timeout.Milliseconds(),
			"payload_size", len(payload),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, "", fmt.Errorf("create gemini request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("gemini request failed: %w", err)
			c.logger.Warn("gemini generate request failed",
				"model", c.model,
				"attempt", attempt,
				"requested_count", input.RequestedCount,
				"timeout_ms", c.timeout.Milliseconds(),
				"duration_ms", time.Since(start).Milliseconds(),
				"error", err,
			)
			c.backoff(attempt)
			continue
		}

		rawBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read gemini response: %w", readErr)
			c.logger.Warn("gemini generate response read failed",
				"model", c.model,
				"attempt", attempt,
				"requested_count", input.RequestedCount,
				"timeout_ms", c.timeout.Milliseconds(),
				"duration_ms", time.Since(start).Milliseconds(),
				"status_code", resp.StatusCode,
				"error", readErr,
			)
			c.backoff(attempt)
			continue
		}
		raw := string(rawBytes)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("gemini server error: status=%d body=%s", resp.StatusCode, raw)
			c.logger.Warn("gemini generate server error",
				"model", c.model,
				"attempt", attempt,
				"requested_count", input.RequestedCount,
				"timeout_ms", c.timeout.Milliseconds(),
				"duration_ms", time.Since(start).Milliseconds(),
				"status_code", resp.StatusCode,
				"response_size", len(rawBytes),
			)
			c.backoff(attempt)
			continue
		}
		if resp.StatusCode >= 400 {
			c.logger.Warn("gemini generate client error",
				"model", c.model,
				"attempt", attempt,
				"requested_count", input.RequestedCount,
				"timeout_ms", c.timeout.Milliseconds(),
				"duration_ms", time.Since(start).Milliseconds(),
				"status_code", resp.StatusCode,
				"response_size", len(rawBytes),
			)
			return nil, raw, fmt.Errorf("gemini error: status=%d body=%s", resp.StatusCode, raw)
		}

		parsed, text, err := parseGenerateResponse(rawBytes)
		if err != nil {
			lastErr = fmt.Errorf("parse gemini response: %w", err)
			c.logger.Warn("gemini generate parse failed",
				"model", c.model,
				"attempt", attempt,
				"requested_count", input.RequestedCount,
				"timeout_ms", c.timeout.Milliseconds(),
				"duration_ms", time.Since(start).Milliseconds(),
				"status_code", resp.StatusCode,
				"response_size", len(rawBytes),
				"error", err,
			)
			c.backoff(attempt)
			continue
		}
		for i := range parsed {
			if parsed[i].SourceProvider == "" {
				parsed[i].SourceProvider = domain.DefaultGeminiProvider
			}
			if parsed[i].SourceMetadata == nil {
				parsed[i].SourceMetadata = domain.JSONMap{}
			}
			parsed[i].SourceMetadata["model"] = c.model
			parsed[i].SourceMetadata["generated_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		c.logger.Info("gemini generate response",
			"model", c.model,
			"attempt", attempt,
			"requested_count", input.RequestedCount,
			"timeout_ms", c.timeout.Milliseconds(),
			"duration_ms", time.Since(start).Milliseconds(),
			"status_code", resp.StatusCode,
			"response_size", len(rawBytes),
			"candidate_count", len(parsed),
			"text_size", len(text),
		)
		return parsed, text, nil
	}
	return nil, "", lastErr
}

func (c *Client) backoff(attempt int) {
	if attempt >= c.maxRetries {
		return
	}
	delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
	time.Sleep(delay)
}

func buildPrompt(input service.GenerationInput) string {
	return fmt.Sprintf(`
Generate %d English vocabulary candidates for a Vietnamese learner.

Requirements:
- CEFR level: %s
- Topic: %s
- Meanings must include both English and Vietnamese
- Avoid duplicates, inflections, confusable collisions, and anything in the exclusion lists
- Prefer practical academic or real-world vocabulary
- Return strict JSON only

	Output format:
	{
	  "words": [
	    {
      "word": "string",
      "canonical_form": "string",
      "lemma": "string",
      "word_family": "string",
      "confusable_group_key": "string",
      "part_of_speech": "string",
      "level": "B1|B2|C1|C2",
      "topic": "string",
      "ipa": "string",
      "pronunciation_hint": "string",
	      "vietnamese_meaning": "string",
	      "english_meaning": "string",
	      "example_sentence_1": "string",
	      "example_sentence_2": "string",
	      "common_rate": "common|formal|rare"
	    }
	  ]
	}

	Common-rate rubric:
	- common: everyday or broadly useful vocabulary that appears often in normal speech and writing
	- formal: more academic, professional, or formal-register vocabulary that is still useful but less everyday
	- rare: uncommon or lower-frequency vocabulary that an advanced learner may still encounter

	Exclude normalized words: %s
	Exclude lemmas: %s
Exclude confusable groups: %s
`, input.RequestedCount, input.CEFRLevel, input.Topic, strings.Join(input.ExcludeWords, ", "), strings.Join(input.ExcludeLemmas, ", "), strings.Join(input.ExcludeGroupKeys, ", "))
}

const systemInstruction = `
You generate backend-ingestable English vocabulary data for a production vocabulary learning service.
Always return valid JSON.
Do not wrap the JSON in markdown fences.
`

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

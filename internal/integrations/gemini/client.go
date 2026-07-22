package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
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
	models          []string
	apiKey          string
	timeout         time.Duration
	maxRetries      int
	temperature     float64
	maxOutputTokens int
	logger          *slog.Logger
	httpClient      *http.Client
	quotaCache      *quotaCache
	now             func() time.Time
	sleep           func(time.Duration)
}

func NewClient(cfg config.DeepSeekConfig, logger *slog.Logger) *Client {
	models := append([]string(nil), cfg.Models...)
	if len(models) == 0 {
		models = []string{"deepseek-v4-flash"}
	}
	return &Client{
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		models:          models,
		apiKey:          cfg.APIKey,
		timeout:         cfg.Timeout,
		maxRetries:      cfg.MaxRetries,
		temperature:     cfg.Temperature,
		maxOutputTokens: cfg.MaxOutputTokens,
		logger:          logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		quotaCache: newQuotaCache(cfg.RPMLimit, cfg.RPDLimit),
		now:        time.Now,
		sleep:      time.Sleep,
	}
}

type chatCompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (c *Client) GenerateCandidates(ctx context.Context, input service.GenerationInput) ([]domain.CandidateWord, string, error) {
	body := chatCompletionRequest{
		Messages: []chatMessage{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: buildPrompt(input)},
		},
		Temperature:    c.temperature,
		MaxTokens:      c.maxOutputTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
		Stream:         false,
	}

	result, err := executeJSON(c, ctx, func(model string) ([]byte, error) {
		requestBody := body
		requestBody.Model = model
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal deepseek request: %w", err)
		}
		return payload, nil
	}, requestOperation{
		requestLog:       "deepseek generate request",
		requestFailedLog: "deepseek generate request failed",
		readFailedLog:    "deepseek generate response read failed",
		serverErrorLog:   "deepseek generate server error",
		clientErrorLog:   "deepseek generate client error",
		parseFailedLog:   "deepseek generate parse failed",
		successLog:       "deepseek generate response",
		createErrPrefix:  "create deepseek request",
		requestErrPrefix: "deepseek request failed",
		readErrPrefix:    "read deepseek response",
		serverErrPrefix:  "deepseek server error",
		clientErrPrefix:  "deepseek error",
		parseErrPrefix:   "parse deepseek response",
		extraFields: []any{
			"requested_count", input.RequestedCount,
		},
	}, parseGenerateResponse)
	if err != nil {
		return nil, result.raw, err
	}

	parsed := result.value
	for i := range parsed {
		if parsed[i].SourceProvider == "" {
			parsed[i].SourceProvider = domain.DefaultLLMProvider
		}
		if parsed[i].SourceMetadata == nil {
			parsed[i].SourceMetadata = domain.JSONMap{}
		}
		parsed[i].SourceMetadata["model"] = result.model
		parsed[i].SourceMetadata["generated_at"] = c.now().UTC().Format(time.RFC3339)
	}
	return parsed, result.text, nil
}

func (c *Client) GenerateDynamicReviewPrompts(ctx context.Context, input service.DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error) {
	body := chatCompletionRequest{
		Messages: []chatMessage{
			{Role: "system", Content: dynamicReviewSystemInstruction},
			{Role: "user", Content: buildDynamicReviewPrompt(input)},
		},
		Temperature:    c.temperature,
		MaxTokens:      c.maxOutputTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
		Stream:         false,
	}

	result, err := executeJSON(c, ctx, func(model string) ([]byte, error) {
		requestBody := body
		requestBody.Model = model
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal deepseek dynamic review request: %w", err)
		}
		return payload, nil
	}, requestOperation{
		requestLog:       "deepseek dynamic review request",
		requestFailedLog: "deepseek dynamic review request failed",
		readFailedLog:    "deepseek dynamic review response read failed",
		serverErrorLog:   "deepseek dynamic review server error",
		clientErrorLog:   "deepseek dynamic review client error",
		parseFailedLog:   "deepseek dynamic review parse failed",
		successLog:       "deepseek dynamic review response",
		createErrPrefix:  "create deepseek dynamic review request",
		requestErrPrefix: "deepseek dynamic review request failed",
		readErrPrefix:    "read deepseek dynamic review response",
		serverErrPrefix:  "deepseek dynamic review server error",
		clientErrPrefix:  "deepseek dynamic review error",
		parseErrPrefix:   "parse deepseek dynamic review response",
		extraFields: []any{
			"item_count", len(input.Items),
		},
	}, parseDynamicReviewGenerateResponse)
	if err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, result.raw, err
	}
	return result.value, result.text, nil
}

func (c *Client) GeneratePromptResponse(ctx context.Context, prompt string) (string, string, string, error) {
	body := chatCompletionRequest{
		Messages: []chatMessage{
			{Role: "system", Content: testPromptSystemInstruction},
			{Role: "user", Content: strings.TrimSpace(prompt)},
		},
		Temperature: c.temperature,
		MaxTokens:   c.maxOutputTokens,
		Stream:      false,
	}

	result, err := executeJSON(c, ctx, func(model string) ([]byte, error) {
		requestBody := body
		requestBody.Model = model
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal deepseek test prompt request: %w", err)
		}
		return payload, nil
	}, requestOperation{
		requestLog:       "deepseek test prompt request",
		requestFailedLog: "deepseek test prompt request failed",
		readFailedLog:    "deepseek test prompt response read failed",
		serverErrorLog:   "deepseek test prompt server error",
		clientErrorLog:   "deepseek test prompt client error",
		parseFailedLog:   "deepseek test prompt parse failed",
		successLog:       "deepseek test prompt response",
		createErrPrefix:  "create deepseek test prompt request",
		requestErrPrefix: "deepseek test prompt request failed",
		readErrPrefix:    "read deepseek test prompt response",
		serverErrPrefix:  "deepseek test prompt server error",
		clientErrPrefix:  "deepseek test prompt error",
		parseErrPrefix:   "parse deepseek test prompt response",
		extraFields: []any{
			"prompt_length", len(strings.TrimSpace(prompt)),
		},
	}, parsePromptResponse)
	if err != nil {
		return "", result.raw, result.model, err
	}
	return result.value, result.raw, result.model, nil
}

func (c *Client) backoff(attempt int) {
	if attempt >= c.maxRetries {
		return
	}
	delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
	c.sleep(delay)
}

func buildPrompt(input service.GenerationInput) string {
	requiredWordLine := ""
	if strings.TrimSpace(input.RequiredWord) != "" {
		requiredWordLine = fmt.Sprintf("- Required item: return exactly \"%s\" as the candidate word, not a substitute\n", strings.TrimSpace(input.RequiredWord))
	}
	return fmt.Sprintf(`
Generate %d English vocabulary candidates for a Vietnamese learner.

Requirements:
- CEFR level: %s
- Topic: %s
%s
- Meanings must include both English and Vietnamese
- Avoid duplicates, inflections, confusable collisions, and anything in the exclusion lists
- Prefer practical academic or real-world vocabulary
- Include the requested item mix when provided. "single_word" items are one lexical word; "phrasal_verb" items are verb particles like "check in"; "collocation" items are natural multi-word chunks like "heavy traffic".
- For phrasal verbs, set part_of_speech to "phrasal verb". For collocations, set part_of_speech to "collocation".
- word_family must be a short comma-separated string with up to 4 useful family items, formatted like "act (verb), action (noun), active (adj), actively (adv)"
- Prefer derivational family members; avoid tense-only or plural-only variants unless no better option exists
- Return strict JSON only

	Output format:
	{
	  "words": [
	    {
      "word": "string",
      "canonical_form": "string",
      "lemma": "string",
      "word_family": "string like \"act (verb), action (noun), active (adj), actively (adv)\"",
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

	Highest-priority words to avoid reusing from today's pool and recent study history: %s
	Highest-priority lemmas to avoid: %s
Highest-priority confusable groups to avoid: %s
Requested item mix: %s
`, input.RequestedCount, input.CEFRLevel, input.Topic, requiredWordLine, strings.Join(input.ExcludeWords, ", "), strings.Join(input.ExcludeLemmas, ", "), strings.Join(input.ExcludeGroupKeys, ", "), input.MixHint)
}

const systemInstruction = `
You generate backend-ingestable English vocabulary data for a production vocabulary learning service.
Always return valid json.
Do not wrap the json in markdown fences.
`

const dynamicReviewSystemInstruction = `
You generate fresh prompt-only overrides for vocabulary review cards in a production learning service.
Always return valid json only.
Do not wrap the json in markdown fences.
For multiple_choice and fill_in_blank, do not reveal the answer word, canonical form, or lemma in the prompt.
For listening_sentence, include the target word naturally because the user will hear and type the full sentence.
Do not ask follow-up questions.
`

const testPromptSystemInstruction = `
You are a helpful assistant for a vocabulary learning app team.
Answer the user's prompt directly and clearly.
`

func buildDynamicReviewPrompt(input service.DynamicReviewPromptGenerationInput) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`
Generate exactly %d prompt overrides for vocabulary review cards.

Requirements:
- return exactly one item for every requested word_id + review_mode pair
- keep word_id and review_mode exactly as provided
- review_mode can only be "multiple_choice", "fill_in_blank", or "listening_sentence"
- for multiple_choice:
  - write one fresh question stem or semantic cue only
  - do not include answer choices
  - do not reveal the answer word, canonical form, or lemma
- for fill_in_blank:
  - write one natural sentence or short passage fragment with the target replaced by "_____"
  - the prompt must contain "_____"
  - do not reveal the answer word, canonical form, or lemma
- for listening_sentence:
  - write one short natural sentence that includes the target word exactly once
  - the sentence must be basic B1/B1+ English and under 10 words
  - do not use a blank marker
- do not copy the english meaning, vietnamese meaning, or example sentences verbatim
- keep prompts concise and CEFR-appropriate
- return strict JSON only

Output format:
{
  "items": [
    {
      "word_id": "uuid",
      "review_mode": "multiple_choice|fill_in_blank|listening_sentence",
      "prompt_text": "string"
    }
  ]
}

Requested review prompts:
`, len(input.Items)))
	for index, item := range input.Items {
		builder.WriteString(fmt.Sprintf(`
%d. word_id="%s"
   review_mode="%s"
   word="%s"
   part_of_speech="%s"
   topic="%s"
   english_meaning="%s"
   example_sentence_1="%s"
`, index+1, item.WordID, item.ReviewMode, item.Word.Word, item.Word.PartOfSpeech, item.Word.Topic, item.Word.EnglishMeaning, item.Word.ExampleSentence1))
	}
	return builder.String()
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func parsePromptResponse(body []byte) (string, string, error) {
	text, err := extractChatText(body)
	if err != nil {
		return "", "", err
	}
	return text, text, nil
}

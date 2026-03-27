package gemini

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

func NewClient(cfg config.GeminiConfig, logger *slog.Logger) *Client {
	models := append([]string(nil), cfg.Models...)
	if len(models) == 0 {
		models = []string{"gemini-2.0-flash"}
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

	result, err := executeJSON(c, ctx, payload, requestOperation{
		requestLog:       "gemini generate request",
		requestFailedLog: "gemini generate request failed",
		readFailedLog:    "gemini generate response read failed",
		serverErrorLog:   "gemini generate server error",
		clientErrorLog:   "gemini generate client error",
		parseFailedLog:   "gemini generate parse failed",
		successLog:       "gemini generate response",
		createErrPrefix:  "create gemini request",
		requestErrPrefix: "gemini request failed",
		readErrPrefix:    "read gemini response",
		serverErrPrefix:  "gemini server error",
		clientErrPrefix:  "gemini error",
		parseErrPrefix:   "parse gemini response",
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
			parsed[i].SourceProvider = domain.DefaultGeminiProvider
		}
		if parsed[i].SourceMetadata == nil {
			parsed[i].SourceMetadata = domain.JSONMap{}
		}
		parsed[i].SourceMetadata["model"] = result.model
		parsed[i].SourceMetadata["generated_at"] = c.now().UTC().Format(time.RFC3339)
	}
	return parsed, result.text, nil
}

func (c *Client) GenerateContextExercisePack(ctx context.Context, input service.ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error) {
	body := generateRequest{
		SystemInstruction: contentBlock{
			Parts: []part{{Text: exerciseSystemInstruction}},
		},
		Contents: []contentBlock{{
			Role:  "user",
			Parts: []part{{Text: buildExercisePrompt(input)}},
		}},
		GenerationConfig: generationConfig{
			Temperature:      c.temperature,
			ResponseMimeType: "application/json",
			MaxOutputTokens:  c.maxOutputTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return domain.ContextExercisePayload{}, "", fmt.Errorf("marshal gemini exercise request: %w", err)
	}

	result, err := executeJSON(c, ctx, payload, requestOperation{
		requestLog:       "gemini exercise generate request",
		requestFailedLog: "gemini exercise generate request failed",
		readFailedLog:    "gemini exercise response read failed",
		serverErrorLog:   "gemini exercise generate server error",
		clientErrorLog:   "gemini exercise generate client error",
		parseFailedLog:   "gemini exercise generate parse failed",
		successLog:       "gemini exercise generate response",
		createErrPrefix:  "create gemini exercise request",
		requestErrPrefix: "gemini exercise request failed",
		readErrPrefix:    "read gemini exercise response",
		serverErrPrefix:  "gemini exercise server error",
		clientErrPrefix:  "gemini exercise error",
		parseErrPrefix:   "parse gemini exercise response",
		extraFields: []any{
			"cluster_size", len(input.ClusterWords),
			"topic", input.Topic,
		},
	}, parseExerciseGenerateResponse)
	if err != nil {
		return domain.ContextExercisePayload{}, result.raw, err
	}
	return result.value, result.text, nil
}

func (c *Client) GenerateMode4WeakPassage(ctx context.Context, input service.Mode4PassageGenerationInput) (domain.Mode4WeakPassagePayload, string, error) {
	body := generateRequest{
		SystemInstruction: contentBlock{
			Parts: []part{{Text: mode4WeakPassageSystemInstruction}},
		},
		Contents: []contentBlock{{
			Role:  "user",
			Parts: []part{{Text: buildMode4WeakPassagePrompt(input)}},
		}},
		GenerationConfig: generationConfig{
			Temperature:      c.temperature,
			ResponseMimeType: "application/json",
			MaxOutputTokens:  c.maxOutputTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return domain.Mode4WeakPassagePayload{}, "", fmt.Errorf("marshal gemini mode4 request: %w", err)
	}

	result, err := executeJSON(c, ctx, payload, requestOperation{
		requestLog:       "gemini mode4 generate request",
		requestFailedLog: "gemini mode4 generate request failed",
		readFailedLog:    "gemini mode4 response read failed",
		serverErrorLog:   "gemini mode4 server error",
		clientErrorLog:   "gemini mode4 client error",
		parseFailedLog:   "gemini mode4 parse failed",
		successLog:       "gemini mode4 response",
		createErrPrefix:  "create gemini mode4 request",
		requestErrPrefix: "gemini mode4 request failed",
		readErrPrefix:    "read gemini mode4 response",
		serverErrPrefix:  "gemini mode4 server error",
		clientErrPrefix:  "gemini mode4 error",
		parseErrPrefix:   "parse gemini mode4 response",
		extraFields: []any{
			"target_count", len(input.TargetWords),
		},
	}, parseMode4WeakPassageGenerateResponse)
	if err != nil {
		return domain.Mode4WeakPassagePayload{}, result.raw, err
	}
	return result.value, result.text, nil
}

func (c *Client) GenerateDynamicReviewPrompts(ctx context.Context, input service.DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error) {
	body := generateRequest{
		SystemInstruction: contentBlock{
			Parts: []part{{Text: dynamicReviewSystemInstruction}},
		},
		Contents: []contentBlock{{
			Role:  "user",
			Parts: []part{{Text: buildDynamicReviewPrompt(input)}},
		}},
		GenerationConfig: generationConfig{
			Temperature:      c.temperature,
			ResponseMimeType: "application/json",
			MaxOutputTokens:  c.maxOutputTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, "", fmt.Errorf("marshal gemini dynamic review request: %w", err)
	}

	result, err := executeJSON(c, ctx, payload, requestOperation{
		requestLog:       "gemini dynamic review request",
		requestFailedLog: "gemini dynamic review request failed",
		readFailedLog:    "gemini dynamic review response read failed",
		serverErrorLog:   "gemini dynamic review server error",
		clientErrorLog:   "gemini dynamic review client error",
		parseFailedLog:   "gemini dynamic review parse failed",
		successLog:       "gemini dynamic review response",
		createErrPrefix:  "create gemini dynamic review request",
		requestErrPrefix: "gemini dynamic review request failed",
		readErrPrefix:    "read gemini dynamic review response",
		serverErrPrefix:  "gemini dynamic review server error",
		clientErrPrefix:  "gemini dynamic review error",
		parseErrPrefix:   "parse gemini dynamic review response",
		extraFields: []any{
			"item_count", len(input.Items),
		},
	}, parseDynamicReviewGenerateResponse)
	if err != nil {
		return domain.DynamicReviewPromptBatchPayload{}, result.raw, err
	}
	return result.value, result.text, nil
}

func (c *Client) backoff(attempt int) {
	if attempt >= c.maxRetries {
		return
	}
	delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
	c.sleep(delay)
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

const exerciseSystemInstruction = `
You generate reusable context-cluster vocabulary exercise packs for a production vocabulary learning service.
Always return valid JSON only.
Do not wrap the JSON in markdown fences.
Do not ask follow-up questions.
`

const mode4WeakPassageSystemInstruction = `
You generate reusable weak-word review passages for a production vocabulary learning service.
Always return valid JSON only.
Do not wrap the JSON in markdown fences.
Do not ask follow-up questions.
`

const dynamicReviewSystemInstruction = `
You generate fresh prompt-only overrides for vocabulary review cards in a production learning service.
Always return valid JSON only.
Do not wrap the JSON in markdown fences.
Do not reveal the answer word, canonical form, or lemma in the prompt.
Do not ask follow-up questions.
`

func buildExercisePrompt(input service.ExercisePackGenerationInput) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`
Generate one reusable English vocabulary exercise pack for weak-word review.

Requirements:
- pack_type must be "context_cluster_challenge"
- topic should stay coherent and realistic
- CEFR level should be %s
- use all selected cluster words exactly as study targets
- produce exactly %d questions
- every selected word must be targeted by at least one question
- use only closed-form questions
- allowed question types: best_fit, meaning_match, definition_match, sentence_usage, passage_understanding, confusable_choice
- every question must have exactly 4 options
- every question must have exactly 1 correct answer and it must match one of the options exactly
- keep the passage and explanations natural and CEFR-appropriate
- return strict JSON only

Output format:
{
  "pack_id": "",
  "topic": "string",
  "cefr_level": "B1|B2|C1|C2",
  "pack_type": "context_cluster_challenge",
  "cluster_words": ["word1", "word2"],
  "title": "string",
  "passage": "string",
  "questions": [
    {
      "id": "q1",
      "type": "best_fit|meaning_match|definition_match|sentence_usage|passage_understanding|confusable_choice",
      "target_word": "string",
      "prompt": "string",
      "options": ["a", "b", "c", "d"],
      "answer": "string",
      "explanation": "string"
    }
  ],
  "summary_tip": "string"
}
Selected weak words:
`, input.CEFRLevel, len(input.ClusterWords)))
	for index, word := range input.ClusterWords {
		builder.WriteString(fmt.Sprintf(`
%d. word="%s"
   normalized_form="%s"
   canonical_form="%s"
   lemma="%s"
   part_of_speech="%s"
   topic="%s"
   level="%s"
   english_meaning="%s"
   vietnamese_meaning="%s"
   example_sentence_1="%s"
   example_sentence_2="%s"
`, index+1, word.Word, word.NormalizedForm, word.CanonicalForm, word.Lemma, word.PartOfSpeech, word.Topic, word.Level, word.EnglishMeaning, word.VietnameseMeaning, word.ExampleSentence1, word.ExampleSentence2))
	}
	return builder.String()
}

func buildMode4WeakPassagePrompt(input service.Mode4PassageGenerationInput) string {
	var builder strings.Builder
	builder.WriteString(`
Generate one English weak-word review passage for a Learn-flow card.

Requirements:
- return strict JSON only
- output must be English only
- everyday, natural tone
- at most 10 sentences
- use every selected target word at least once
- mark every target word occurrence with markdown **double-asterisk** markers
- prefer the exact selected surface form of each target word
- do not ask questions
- do not include blanks
- do not include multiple-choice content
- do not add explanations outside the JSON

Output format:
{
  "plain_passage_text": "string",
  "marked_passage_markdown": "string"
}

Selected weak words:
`)
	for index, word := range input.TargetWords {
		builder.WriteString(fmt.Sprintf(`
%d. word="%s"
   normalized_form="%s"
   canonical_form="%s"
   lemma="%s"
   part_of_speech="%s"
   topic="%s"
   level="%s"
   english_meaning="%s"
   vietnamese_meaning="%s"
   example_sentence_1="%s"
   example_sentence_2="%s"
`, index+1, word.Word, word.NormalizedForm, word.CanonicalForm, word.Lemma, word.PartOfSpeech, word.Topic, word.Level, word.EnglishMeaning, word.VietnameseMeaning, word.ExampleSentence1, word.ExampleSentence2))
	}
	return builder.String()
}

func buildDynamicReviewPrompt(input service.DynamicReviewPromptGenerationInput) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`
Generate exactly %d prompt overrides for vocabulary review cards.

Requirements:
- return exactly one item for every requested word_id + review_mode pair
- keep word_id and review_mode exactly as provided
- review_mode can only be "multiple_choice" or "fill_in_blank"
- for multiple_choice:
  - write one fresh question stem or semantic cue only
  - do not include answer choices
  - do not reveal the answer word, canonical form, or lemma
- for fill_in_blank:
  - write one natural sentence or short passage fragment with the target replaced by "_____"
  - the prompt must contain "_____"
  - do not reveal the answer word, canonical form, or lemma
- do not copy the english meaning, vietnamese meaning, or example sentences verbatim
- keep prompts concise and CEFR-appropriate
- return strict JSON only

Output format:
{
  "items": [
    {
      "word_id": "uuid",
      "review_mode": "multiple_choice|fill_in_blank",
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
   normalized_form="%s"
   canonical_form="%s"
   lemma="%s"
   part_of_speech="%s"
   level="%s"
   topic="%s"
   english_meaning="%s"
   vietnamese_meaning="%s"
   example_sentence_1="%s"
   example_sentence_2="%s"
`, index+1, item.WordID, item.ReviewMode, item.Word.Word, item.Word.NormalizedForm, item.Word.CanonicalForm, item.Word.Lemma, item.Word.PartOfSpeech, item.Word.Level, item.Word.Topic, item.Word.EnglishMeaning, item.Word.VietnameseMeaning, item.Word.ExampleSentence1, item.Word.ExampleSentence2))
	}
	return builder.String()
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

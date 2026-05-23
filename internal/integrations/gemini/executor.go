package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

const defaultRateLimitCooldown = 30 * time.Second

type responseParser[T any] func([]byte) (T, string, error)
type requestFactory func(string) ([]byte, error)

type requestOperation struct {
	requestLog       string
	requestFailedLog string
	readFailedLog    string
	serverErrorLog   string
	clientErrorLog   string
	parseFailedLog   string
	successLog       string
	createErrPrefix  string
	requestErrPrefix string
	readErrPrefix    string
	serverErrPrefix  string
	clientErrPrefix  string
	parseErrPrefix   string
	extraFields      []any
}

type executionResult[T any] struct {
	value T
	text  string
	raw   string
	model string
}

type modelAvailability struct {
	model       string
	availableAt time.Time
	reason      string
}

type quotaCache struct {
	mu       sync.Mutex
	rpmLimit int
	rpdLimit int
	models   map[string]*modelQuotaState
}

type modelQuotaState struct {
	requests         []time.Time
	unavailableUntil time.Time
	lastReason       string
}

type rateLimitError struct {
	message string
}

func (e *rateLimitError) Error() string {
	return e.message
}

func (e *rateLimitError) Unwrap() error {
	return domain.ErrRateLimited
}

func newQuotaCache(rpmLimit int, rpdLimit int) *quotaCache {
	return &quotaCache{
		rpmLimit: rpmLimit,
		rpdLimit: rpdLimit,
		models:   map[string]*modelQuotaState{},
	}
}

func (q *quotaCache) reserve(model string, now time.Time) (bool, modelAvailability) {
	q.mu.Lock()
	defer q.mu.Unlock()

	state := q.modelState(model)
	state.prune(now)
	if availableAt, reason, blocked := state.blockedUntil(now, q.rpmLimit, q.rpdLimit); blocked {
		return false, modelAvailability{
			model:       model,
			availableAt: availableAt,
			reason:      reason,
		}
	}

	state.requests = append(state.requests, now)
	return true, modelAvailability{}
}

func (q *quotaCache) markRateLimited(model string, now time.Time, retryAfter time.Duration, reason string) modelAvailability {
	if retryAfter <= 0 {
		retryAfter = defaultRateLimitCooldown
	}
	if reason == "" {
		reason = "upstream rate limit"
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	state := q.modelState(model)
	state.prune(now)

	availableAt := now.Add(retryAfter)
	if availableAt.After(state.unavailableUntil) {
		state.unavailableUntil = availableAt
	}
	state.lastReason = reason
	return modelAvailability{
		model:       model,
		availableAt: state.unavailableUntil,
		reason:      state.lastReason,
	}
}

func (q *quotaCache) modelState(model string) *modelQuotaState {
	state, ok := q.models[model]
	if ok {
		return state
	}
	state = &modelQuotaState{}
	q.models[model] = state
	return state
}

func (s *modelQuotaState) prune(now time.Time) {
	cutoff := now.Add(-24 * time.Hour)
	idx := 0
	for idx < len(s.requests) && s.requests[idx].Before(cutoff) {
		idx++
	}
	if idx > 0 {
		s.requests = append([]time.Time(nil), s.requests[idx:]...)
	}
}

func (s *modelQuotaState) blockedUntil(now time.Time, rpmLimit int, rpdLimit int) (time.Time, string, bool) {
	var availableAt time.Time
	reason := ""

	if s.unavailableUntil.After(now) {
		availableAt = s.unavailableUntil
		reason = s.lastReason
	}

	if rpmLimit > 0 {
		count := 0
		var earliest time.Time
		minuteCutoff := now.Add(-time.Minute)
		for _, requestAt := range s.requests {
			if requestAt.Before(minuteCutoff) {
				continue
			}
			count++
			if earliest.IsZero() || requestAt.Before(earliest) {
				earliest = requestAt
			}
		}
		if count >= rpmLimit {
			candidate := earliest.Add(time.Minute)
			if candidate.After(availableAt) {
				availableAt = candidate
				reason = "local rpm limit"
			}
		}
	}

	if rpdLimit > 0 && len(s.requests) >= rpdLimit {
		candidate := s.requests[len(s.requests)-rpdLimit].Add(24 * time.Hour)
		if candidate.After(availableAt) {
			availableAt = candidate
			reason = "local rpd limit"
		}
	}

	if availableAt.After(now) {
		if reason == "" {
			reason = "model unavailable"
		}
		return availableAt, reason, true
	}
	return time.Time{}, "", false
}

func newRateLimitError(unavailable []modelAvailability) error {
	if len(unavailable) == 0 {
		return &rateLimitError{message: domain.ErrRateLimited.Error()}
	}

	parts := make([]string, 0, len(unavailable))
	for _, model := range unavailable {
		if model.availableAt.IsZero() {
			if model.reason != "" {
				parts = append(parts, fmt.Sprintf("%s (%s)", model.model, model.reason))
			} else {
				parts = append(parts, model.model)
			}
			continue
		}
		if model.reason != "" {
			parts = append(parts, fmt.Sprintf("%s until %s (%s)", model.model, model.availableAt.UTC().Format(time.RFC3339), model.reason))
		} else {
			parts = append(parts, fmt.Sprintf("%s until %s", model.model, model.availableAt.UTC().Format(time.RFC3339)))
		}
	}

	return &rateLimitError{
		message: fmt.Sprintf("%s: all configured DeepSeek models are unavailable: %s", domain.ErrRateLimited.Error(), strings.Join(parts, "; ")),
	}
}

func executeJSON[T any](c *Client, ctx context.Context, buildPayload requestFactory, operation requestOperation, parse responseParser[T]) (executionResult[T], error) {
	var result executionResult[T]
	unavailable := make([]modelAvailability, 0, len(c.models))

	for _, model := range c.models {
		reserved, availability := c.quotaCache.reserve(model, c.now())
		if !reserved {
			unavailable = append(unavailable, availability)
			c.logger.Info("deepseek model unavailable locally",
				"model", model,
				"available_at", availability.availableAt.UTC().Format(time.RFC3339),
				"reason", availability.reason,
			)
			continue
		}

		modelResult, rotate, rotatedModel, err := executeOnModel(c, ctx, model, buildPayload, operation, parse)
		if err == nil {
			return modelResult, nil
		}

		result.raw = modelResult.raw
		if rotate {
			unavailable = append(unavailable, rotatedModel)
			continue
		}
		return result, err
	}

	err := newRateLimitError(unavailable)
	c.logger.Warn("deepseek request failed: all configured models unavailable",
		"model_count", len(c.models),
		"error", err,
	)
	return result, err
}

func executeOnModel[T any](c *Client, ctx context.Context, model string, buildPayload requestFactory, operation requestOperation, parse responseParser[T]) (executionResult[T], bool, modelAvailability, error) {
	var result executionResult[T]
	url := fmt.Sprintf("%s/chat/completions", c.baseURL)
	var lastErr error

	for attempt := 1; attempt <= maxInt(c.maxRetries, 1); attempt++ {
		payload, payloadErr := buildPayload(model)
		if payloadErr != nil {
			return result, false, modelAvailability{}, payloadErr
		}
		start := time.Now()
		c.logger.Info(operation.requestLog, c.logFields(model, attempt, len(payload), operation.extraFields)...)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return result, false, modelAvailability{}, fmt.Errorf("%s: %w", operation.createErrPrefix, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", operation.requestErrPrefix, err)
			c.logger.Warn(operation.requestFailedLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields, "error", err)...,
			)
			c.backoff(attempt)
			continue
		}

		rawBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("%s: %w", operation.readErrPrefix, readErr)
			c.logger.Warn(operation.readFailedLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields,
					"status_code", resp.StatusCode,
					"error", readErr,
				)...,
			)
			c.backoff(attempt)
			continue
		}

		raw := string(rawBytes)
		result.raw = raw

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s: status=%d body=%s", operation.serverErrPrefix, resp.StatusCode, raw)
			c.logger.Warn(operation.serverErrorLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields,
					"status_code", resp.StatusCode,
					"response_size", len(rawBytes),
				)...,
			)
			c.backoff(attempt)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter, reason := parseDeepSeekRateLimit(rawBytes, resp.Header.Get("Retry-After"))
			availability := c.quotaCache.markRateLimited(model, c.now(), retryAfter, reason)
			c.logger.Warn(operation.clientErrorLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields,
					"status_code", resp.StatusCode,
					"response_size", len(rawBytes),
					"available_at", availability.availableAt.UTC().Format(time.RFC3339),
					"limit_reason", availability.reason,
				)...,
			)
			return result, true, availability, newRateLimitError([]modelAvailability{availability})
		}

		if resp.StatusCode >= 400 {
			c.logger.Warn(operation.clientErrorLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields,
					"status_code", resp.StatusCode,
					"response_size", len(rawBytes),
				)...,
			)
			return result, false, modelAvailability{}, fmt.Errorf("%s: status=%d body=%s", operation.clientErrPrefix, resp.StatusCode, raw)
		}

		parsed, text, err := parse(rawBytes)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", operation.parseErrPrefix, err)
			c.logger.Warn(operation.parseFailedLog,
				c.errorLogFields(model, attempt, time.Since(start), operation.extraFields,
					"status_code", resp.StatusCode,
					"response_size", len(rawBytes),
					"error", err,
				)...,
			)
			c.backoff(attempt)
			continue
		}

		result.value = parsed
		result.text = text
		result.model = model
		c.logger.Info(operation.successLog,
			c.successLogFields(model, attempt, time.Since(start), len(rawBytes), len(text), operation.extraFields)...,
		)
		return result, false, modelAvailability{}, nil
	}

	return result, false, modelAvailability{}, lastErr
}

func (c *Client) logFields(model string, attempt int, payloadSize int, extra []any) []any {
	fields := append([]any{
		"model", model,
		"attempt", attempt,
		"timeout_ms", c.timeout.Milliseconds(),
		"payload_size", payloadSize,
	}, extra...)
	return fields
}

func (c *Client) errorLogFields(model string, attempt int, duration time.Duration, extra []any, extraTail ...any) []any {
	fields := append([]any{
		"model", model,
		"attempt", attempt,
		"timeout_ms", c.timeout.Milliseconds(),
		"duration_ms", duration.Milliseconds(),
	}, extra...)
	fields = append(fields, extraTail...)
	return fields
}

func (c *Client) successLogFields(model string, attempt int, duration time.Duration, responseSize int, textSize int, extra []any, extraTail ...any) []any {
	fields := append([]any{
		"model", model,
		"attempt", attempt,
		"timeout_ms", c.timeout.Milliseconds(),
		"duration_ms", duration.Milliseconds(),
		"response_size", responseSize,
		"text_size", textSize,
	}, extra...)
	fields = append(fields, extraTail...)
	return fields
}

type deepSeekErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func parseDeepSeekRateLimit(raw []byte, retryAfterHeader string) (time.Duration, string) {
	var envelope deepSeekErrorEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return parseRetryAfter(retryAfterHeader), ""
	}

	retryAfter := parseRetryAfter(retryAfterHeader)
	reason := strings.TrimSpace(envelope.Error.Message)
	if reason == "" && strings.TrimSpace(envelope.Error.Type) != "" {
		reason = envelope.Error.Type
	}
	if reason == "" && envelope.Error.Code != nil {
		reason = fmt.Sprint(envelope.Error.Code)
	}

	return retryAfter, reason
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultRateLimitCooldown
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil && seconds > 0 {
		return seconds
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}
	return defaultRateLimitCooldown
}

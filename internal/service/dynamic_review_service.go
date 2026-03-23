package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	dynamicReviewPromptFamily    = "dynamic_review_mode23"
	dynamicReviewPromptSource    = "llm_daily_mode23"
	dynamicReviewPromptChunkSize = 25
	dynamicReviewTriggerPrewarm  = "prewarm"
	dynamicReviewTriggerBackfill = "backfill"
	dynamicReviewMetadataKey     = "dynamic_review"
)

type DynamicReviewService struct {
	promptRepo DynamicReviewPromptRepository
	llmRepo    LLMRunRepository
	generator  DynamicReviewPromptGenerator
	clock      Clock
	logger     *slog.Logger
}

type dynamicReviewKey struct {
	WordID     uuid.UUID
	ReviewMode domain.ReviewMode
}

type dynamicReviewCandidate struct {
	WordID     uuid.UUID
	ReviewMode domain.ReviewMode
	Word       domain.Word
	ItemType   domain.PoolItemType
	DueAt      *time.Time
	Ordinal    int
}

func NewDynamicReviewService(
	promptRepo DynamicReviewPromptRepository,
	llmRepo LLMRunRepository,
	generator DynamicReviewPromptGenerator,
	clock Clock,
	logger *slog.Logger,
) *DynamicReviewService {
	return &DynamicReviewService{
		promptRepo: promptRepo,
		llmRepo:    llmRepo,
		generator:  generator,
		clock:      clock,
		logger:     logger,
	}
}

func (s *DynamicReviewService) OverlayPoolItems(ctx context.Context, userID uuid.UUID, localDate string, items []domain.DailyLearningPoolItem) ([]domain.DailyLearningPoolItem, error) {
	promptMap, err := s.loadPromptMap(ctx, userID, localDate)
	if err != nil {
		return nil, err
	}
	return overlayDynamicPrompts(items, promptMap), nil
}

func (s *DynamicReviewService) OverlayCardOnly(ctx context.Context, userID uuid.UUID, localDate string, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, bool, error) {
	promptMap, err := s.loadPromptMap(ctx, userID, localDate)
	if err != nil {
		return domain.DailyLearningPoolItem{}, false, err
	}
	prompt, ok := promptMap[dynamicReviewKey{WordID: item.WordID, ReviewMode: item.ReviewMode}]
	if !ok {
		return copyPoolItem(item), false, nil
	}
	return applyDynamicPrompt(item, prompt.Payload), true, nil
}

func (s *DynamicReviewService) Prewarm(ctx context.Context, userID uuid.UUID, localDate string, items []domain.DailyLearningPoolItem) (DynamicReviewGenerationResult, error) {
	result := DynamicReviewGenerationResult{
		LocalDate:     localDate,
		EligibleCount: len(selectDynamicReviewCandidates(items)),
	}
	generated, err := s.ensurePrompts(ctx, userID, localDate, items, dynamicReviewTriggerPrewarm, nil)
	result.GeneratedCount = generated
	return result, err
}

func (s *DynamicReviewService) BackfillForCurrentCard(ctx context.Context, userID uuid.UUID, localDate string, items []domain.DailyLearningPoolItem, current domain.DailyLearningPoolItem) error {
	if current.ItemType != domain.PoolItemTypeShortTerm && current.ItemType != domain.PoolItemTypeReview {
		return nil
	}
	if current.ReviewMode != domain.ReviewModeMultipleChoice && current.ReviewMode != domain.ReviewModeFillBlank {
		return nil
	}
	_, err := s.ensurePrompts(ctx, userID, localDate, items, dynamicReviewTriggerBackfill, &dynamicReviewKey{
		WordID:     current.WordID,
		ReviewMode: current.ReviewMode,
	})
	return err
}

func (s *DynamicReviewService) ensurePrompts(
	ctx context.Context,
	userID uuid.UUID,
	localDate string,
	items []domain.DailyLearningPoolItem,
	trigger string,
	currentKey *dynamicReviewKey,
) (int, error) {
	candidates := selectDynamicReviewCandidates(items)
	if len(candidates) == 0 {
		return 0, nil
	}

	existing, err := s.loadPromptMap(ctx, userID, localDate)
	if err != nil {
		return 0, err
	}

	missing := make([]dynamicReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := existing[candidate.key()]; ok {
			continue
		}
		missing = append(missing, candidate)
	}
	if len(missing) == 0 {
		return 0, nil
	}

	if currentKey != nil {
		index := -1
		for idx, candidate := range missing {
			if candidate.key() == *currentKey {
				index = idx
				break
			}
		}
		if index < 0 {
			return 0, nil
		}
		start := (index / dynamicReviewPromptChunkSize) * dynamicReviewPromptChunkSize
		end := start + dynamicReviewPromptChunkSize
		if end > len(missing) {
			end = len(missing)
		}
		missing = missing[start:end]
	}

	generated := 0
	for start := 0; start < len(missing); start += dynamicReviewPromptChunkSize {
		end := start + dynamicReviewPromptChunkSize
		if end > len(missing) {
			end = len(missing)
		}
		saved, chunkErr := s.generateAndPersistChunk(ctx, userID, localDate, missing[start:end], trigger)
		if chunkErr != nil {
			if currentKey != nil {
				return generated, chunkErr
			}
			s.logger.Warn("dynamic review prompt chunk failed",
				"user_id", userID,
				"local_date", localDate,
				"trigger", trigger,
				"error", chunkErr,
			)
			continue
		}
		generated += len(saved)
		if currentKey != nil {
			break
		}
	}
	return generated, nil
}

func (s *DynamicReviewService) generateAndPersistChunk(
	ctx context.Context,
	userID uuid.UUID,
	localDate string,
	chunk []dynamicReviewCandidate,
	trigger string,
) ([]domain.DailyDynamicReviewPrompt, error) {
	if len(chunk) == 0 {
		return []domain.DailyDynamicReviewPrompt{}, nil
	}

	input := DynamicReviewPromptGenerationInput{
		UserID:    userID,
		LocalDate: localDate,
		Items:     buildDynamicReviewRequestItems(chunk),
	}
	payload, raw, genErr := s.generator.GenerateDynamicReviewPrompts(ctx, input)
	validationIssues := validateDynamicReviewPayload(payload, chunk)

	status := domain.LLMRunStatusSuccess
	errorMessage := ""
	acceptedCount := len(chunk)
	if genErr != nil {
		status = domain.LLMRunStatusFailed
		errorMessage = genErr.Error()
		acceptedCount = 0
	} else if len(validationIssues) > 0 {
		status = domain.LLMRunStatusPartial
		errorMessage = strings.Join(validationIssues, "; ")
		acceptedCount = 0
	}

	attempt, countErr := s.llmRepo.CountByUserLocalDateAndPrompt(ctx, userID, localDate, dynamicReviewPromptFamily)
	if countErr != nil {
		return nil, countErr
	}

	run, runErr := s.llmRepo.InsertReturning(ctx, domain.LLMGenerationRun{
		UserID:         userID,
		LocalDate:      localDate,
		Topic:          dynamicReviewChunkTopic(chunk),
		RequestedCount: len(chunk),
		AcceptedCount:  acceptedCount,
		Attempt:        attempt + 1,
		Status:         status,
		Provider:       domain.DefaultGeminiProvider,
		Model:          "dynamic",
		Prompt:         dynamicReviewPromptFamily,
		RawResponse: domain.JSONMap{
			"text":           raw,
			"trigger":        trigger,
			"requested_keys": dynamicReviewKeyLabels(chunk),
		},
		RejectionSummary: domain.JSONMap{
			"validation_issues": validationIssues,
		},
		ErrorMessage: errorMessage,
	})
	if runErr != nil {
		return nil, runErr
	}

	if genErr != nil {
		return nil, genErr
	}
	if len(validationIssues) > 0 {
		return nil, fmt.Errorf("dynamic review payload validation failed: %s", strings.Join(validationIssues, "; "))
	}

	now := s.clock.Now().UTC().Format(time.RFC3339)
	prompts := make([]domain.DailyDynamicReviewPrompt, 0, len(payload.Items))
	for _, item := range payload.Items {
		prompts = append(prompts, domain.DailyDynamicReviewPrompt{
			ID:         uuid.New(),
			UserID:     userID,
			LocalDate:  localDate,
			WordID:     item.WordID,
			ReviewMode: item.ReviewMode,
			Payload: domain.DynamicReviewPromptPayload{
				PromptText:  strings.TrimSpace(item.PromptText),
				Source:      dynamicReviewPromptSource,
				GeneratedAt: now,
			},
			LLMRunID: &run.ID,
		})
	}

	return s.promptRepo.UpsertBatch(ctx, prompts)
}

func (s *DynamicReviewService) loadPromptMap(ctx context.Context, userID uuid.UUID, localDate string) (map[dynamicReviewKey]domain.DailyDynamicReviewPrompt, error) {
	prompts, err := s.promptRepo.ListByLocalDate(ctx, userID, localDate)
	if err != nil {
		return nil, err
	}
	promptMap := make(map[dynamicReviewKey]domain.DailyDynamicReviewPrompt, len(prompts))
	for _, prompt := range prompts {
		promptMap[dynamicReviewKey{WordID: prompt.WordID, ReviewMode: prompt.ReviewMode}] = prompt
	}
	return promptMap, nil
}

func selectDynamicReviewCandidates(items []domain.DailyLearningPoolItem) []dynamicReviewCandidate {
	seen := map[dynamicReviewKey]struct{}{}
	candidates := make([]dynamicReviewCandidate, 0)
	for _, item := range items {
		if item.ItemType != domain.PoolItemTypeShortTerm && item.ItemType != domain.PoolItemTypeReview {
			continue
		}
		if item.ReviewMode != domain.ReviewModeMultipleChoice && item.ReviewMode != domain.ReviewModeFillBlank {
			continue
		}
		if item.Word == nil || strings.TrimSpace(item.Word.Word) == "" {
			continue
		}
		candidate := dynamicReviewCandidate{
			WordID:     item.WordID,
			ReviewMode: item.ReviewMode,
			Word:       *item.Word,
			ItemType:   item.ItemType,
			DueAt:      item.DueAt,
			Ordinal:    item.Ordinal,
		}
		if _, ok := seen[candidate.key()]; ok {
			continue
		}
		seen[candidate.key()] = struct{}{}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i int, j int) bool {
		return compareDynamicReviewCandidates(candidates[i], candidates[j]) < 0
	})
	return candidates
}

func overlayDynamicPrompts(items []domain.DailyLearningPoolItem, promptMap map[dynamicReviewKey]domain.DailyDynamicReviewPrompt) []domain.DailyLearningPoolItem {
	out := make([]domain.DailyLearningPoolItem, 0, len(items))
	for _, item := range items {
		prompt, ok := promptMap[dynamicReviewKey{WordID: item.WordID, ReviewMode: item.ReviewMode}]
		if !ok {
			out = append(out, copyPoolItem(item))
			continue
		}
		out = append(out, applyDynamicPrompt(item, prompt.Payload))
	}
	return out
}

func applyDynamicPrompt(item domain.DailyLearningPoolItem, payload domain.DynamicReviewPromptPayload) domain.DailyLearningPoolItem {
	copied := copyPoolItem(item)
	if copied.Metadata == nil {
		copied.Metadata = domain.JSONMap{}
	}
	copied.Metadata[dynamicReviewMetadataKey] = domain.JSONMap{
		"prompt_text":  payload.PromptText,
		"source":       payload.Source,
		"generated_at": payload.GeneratedAt,
	}
	return copied
}

func copyPoolItem(item domain.DailyLearningPoolItem) domain.DailyLearningPoolItem {
	copied := item
	if item.Metadata != nil {
		copied.Metadata = make(domain.JSONMap, len(item.Metadata))
		for key, value := range item.Metadata {
			copied.Metadata[key] = value
		}
	}
	return copied
}

func buildDynamicReviewRequestItems(chunk []dynamicReviewCandidate) []DynamicReviewPromptRequestItem {
	items := make([]DynamicReviewPromptRequestItem, 0, len(chunk))
	for _, candidate := range chunk {
		items = append(items, DynamicReviewPromptRequestItem{
			WordID:     candidate.WordID,
			ReviewMode: candidate.ReviewMode,
			Word:       candidate.Word,
		})
	}
	return items
}

func validateDynamicReviewPayload(payload domain.DynamicReviewPromptBatchPayload, requested []dynamicReviewCandidate) []string {
	if len(payload.Items) != len(requested) {
		return []string{fmt.Sprintf("payload item count %d did not match requested count %d", len(payload.Items), len(requested))}
	}

	requestedMap := make(map[dynamicReviewKey]dynamicReviewCandidate, len(requested))
	for _, candidate := range requested {
		requestedMap[candidate.key()] = candidate
	}

	seen := make(map[dynamicReviewKey]struct{}, len(payload.Items))
	issues := make([]string, 0)
	for _, item := range payload.Items {
		key := dynamicReviewKey{WordID: item.WordID, ReviewMode: item.ReviewMode}
		candidate, ok := requestedMap[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("unexpected prompt item %s|%s", item.WordID, item.ReviewMode))
			continue
		}
		if _, duplicated := seen[key]; duplicated {
			issues = append(issues, fmt.Sprintf("duplicate prompt item %s|%s", item.WordID, item.ReviewMode))
			continue
		}
		seen[key] = struct{}{}
		promptText := strings.TrimSpace(item.PromptText)
		if promptText == "" {
			issues = append(issues, fmt.Sprintf("blank prompt for %s|%s", item.WordID, item.ReviewMode))
			continue
		}
		if item.ReviewMode == domain.ReviewModeFillBlank && !strings.Contains(promptText, "_____") {
			issues = append(issues, fmt.Sprintf("fill_in_blank prompt missing blank marker for %s", item.WordID))
		}
		if leaksTarget(promptText, candidate.Word) {
			issues = append(issues, fmt.Sprintf("prompt leaked target spelling for %s", item.WordID))
		}
		if matchesStoredSource(promptText, candidate.Word) {
			issues = append(issues, fmt.Sprintf("prompt duplicated stored source text for %s", item.WordID))
		}
	}
	for key := range requestedMap {
		if _, ok := seen[key]; !ok {
			issues = append(issues, fmt.Sprintf("missing prompt for %s|%s", key.WordID, key.ReviewMode))
		}
	}
	return issues
}

func dynamicReviewKeyLabels(chunk []dynamicReviewCandidate) []string {
	labels := make([]string, 0, len(chunk))
	for _, candidate := range chunk {
		labels = append(labels, fmt.Sprintf("%s|%s", candidate.WordID, candidate.ReviewMode))
	}
	return labels
}

func dynamicReviewChunkTopic(chunk []dynamicReviewCandidate) string {
	if len(chunk) == 0 {
		return ""
	}
	return chunk[0].Word.Topic
}

func compareDynamicReviewCandidates(left dynamicReviewCandidate, right dynamicReviewCandidate) int {
	if diff := comparePoolItemTypePriority(left.ItemType, right.ItemType); diff != 0 {
		return diff
	}
	if diff := compareOptionalTimesAsc(left.DueAt, right.DueAt); diff != 0 {
		return diff
	}
	if diff := compareInts(left.Ordinal, right.Ordinal); diff != 0 {
		return diff
	}
	if diff := strings.Compare(left.Word.NormalizedForm, right.Word.NormalizedForm); diff != 0 {
		return diff
	}
	return strings.Compare(string(left.ReviewMode), string(right.ReviewMode))
}

func compareOptionalTimesAsc(left *time.Time, right *time.Time) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return 1
	case right == nil:
		return -1
	case left.Before(*right):
		return -1
	case left.After(*right):
		return 1
	default:
		return 0
	}
}

func comparePoolItemTypePriority(left domain.PoolItemType, right domain.PoolItemType) int {
	return compareInts(poolItemPriority(left), poolItemPriority(right))
}

func poolItemPriority(itemType domain.PoolItemType) int {
	switch itemType {
	case domain.PoolItemTypeShortTerm, domain.PoolItemTypeReview:
		return 0
	case domain.PoolItemTypeWeak:
		return 1
	default:
		return 2
	}
}

func leaksTarget(prompt string, word domain.Word) bool {
	normalizedPrompt := NormalizeWord(prompt)
	for _, candidate := range []string{word.Word, word.CanonicalForm, word.Lemma} {
		normalizedCandidate := NormalizeWord(candidate)
		if normalizedCandidate == "" {
			continue
		}
		if strings.Contains(" "+normalizedPrompt+" ", " "+normalizedCandidate+" ") {
			return true
		}
	}
	return false
}

func matchesStoredSource(prompt string, word domain.Word) bool {
	normalizedPrompt := NormalizeWord(prompt)
	for _, candidate := range []string{
		word.EnglishMeaning,
		word.VietnameseMeaning,
		word.ExampleSentence1,
		word.ExampleSentence2,
	} {
		normalizedCandidate := NormalizeWord(candidate)
		if normalizedCandidate == "" {
			continue
		}
		if normalizedPrompt == normalizedCandidate {
			return true
		}
	}
	return false
}

func (c dynamicReviewCandidate) key() dynamicReviewKey {
	return dynamicReviewKey{WordID: c.WordID, ReviewMode: c.ReviewMode}
}

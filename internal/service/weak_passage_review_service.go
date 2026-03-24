package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	mode4PromptFamily          = "mode4_weak_passage_review"
	mode4CandidateLimit        = 24
	mode4MaxWordCount          = 6
	mode4MinWordCount          = 4
	mode4RetryBackoff          = 30 * time.Minute
	mode4ReappearInterval      = 3 * time.Hour
	mode4SentenceLimit         = 10
	mode4TargetSelectionWindow = 6
)

var (
	mode4MarkedWordPattern = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mode4SentencePattern   = regexp.MustCompile(`[.!?]+`)
)

type WeakPassageReviewService struct {
	wordRepo   WordRepository
	stateRepo  WordStateRepository
	reviewRepo Mode4ReviewRepository
	eventRepo  LearningEventRepository
	llmRepo    LLMRunRepository
	generator  Mode4PassageGenerator
	clock      Clock
	logger     *slog.Logger
}

type mode4Candidate struct {
	state domain.UserWordState
	word  domain.Word
}

func NewWeakPassageReviewService(
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	reviewRepo Mode4ReviewRepository,
	eventRepo LearningEventRepository,
	llmRepo LLMRunRepository,
	generator Mode4PassageGenerator,
	clock Clock,
	logger *slog.Logger,
) *WeakPassageReviewService {
	return &WeakPassageReviewService{
		wordRepo:   wordRepo,
		stateRepo:  stateRepo,
		reviewRepo: reviewRepo,
		eventRepo:  eventRepo,
		llmRepo:    llmRepo,
		generator:  generator,
		clock:      clock,
		logger:     logger,
	}
}

func (s *WeakPassageReviewService) MaybeOverlayCard(ctx context.Context, user domain.User, fallback CardResponse) (CardResponse, error) {
	if fallback.PoolItem == nil || fallback.LocalDate == "" {
		return fallback, nil
	}

	now := s.clock.Now()
	state, err := s.reviewRepo.GetOrCreateState(ctx, user.ID)
	if err != nil {
		return fallback, err
	}

	activePassage, state, err := s.resolveActivePassage(ctx, state)
	if err != nil {
		return fallback, err
	}
	if activePassage != nil {
		if !mode4Eligible(state.NextEligibleAt, now) {
			return fallback, nil
		}
		card, err := s.buildMode4Card(ctx, *activePassage)
		if err != nil {
			return fallback, err
		}
		return CardResponse{
			CardType:  domain.LearnCardTypeMode4WeakPassageReview,
			LocalDate: fallback.LocalDate,
			Mode4:     &card,
		}, nil
	}

	if !mode4Eligible(state.NextEligibleAt, now) {
		return fallback, nil
	}

	passage, generateErr := s.generatePassageIfEligible(ctx, user.ID, fallback.LocalDate, state, now)
	if generateErr != nil {
		s.logWarn("mode4 generation failed", "user_id", user.ID, "local_date", fallback.LocalDate, "error", generateErr)
		retryAt := now.Add(mode4RetryBackoff)
		state.NextEligibleAt = &retryAt
		if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
			s.logWarn("mode4 set retry backoff", "user_id", user.ID, "error", err)
		}
		return fallback, nil
	}
	if passage == nil {
		return fallback, nil
	}

	card, err := s.buildMode4Card(ctx, *passage)
	if err != nil {
		return fallback, err
	}
	return CardResponse{
		CardType:  domain.LearnCardTypeMode4WeakPassageReview,
		LocalDate: fallback.LocalDate,
		Mode4:     &card,
	}, nil
}

func (s *WeakPassageReviewService) Complete(ctx context.Context, user domain.User, req Mode4CompletionRequest) error {
	state, err := s.reviewRepo.GetOrCreateState(ctx, user.ID)
	if err != nil {
		return err
	}
	if req.PassageID == uuid.Nil {
		return fmt.Errorf("%w: passage_id is required", domain.ErrValidation)
	}

	passage, err := s.reviewRepo.GetPassage(ctx, user.ID, req.PassageID)
	if err != nil {
		return err
	}
	if passage.Status != domain.Mode4ReviewPassageStatusActive {
		return fmt.Errorf("%w: passage is not active", domain.ErrValidation)
	}
	if state.ActivePassageID == nil || *state.ActivePassageID != passage.ID {
		return fmt.Errorf("%w: passage is not the current active mode4 passage", domain.ErrValidation)
	}

	now := s.clock.Now()
	nextEligibleAt := now.Add(mode4ReappearInterval)
	switch req.Action {
	case domain.Mode4ReviewActionSkip:
		if _, err := s.reviewRepo.UpdatePassageSkip(ctx, user.ID, passage.ID, passage.SkipCount+1, &now); err != nil {
			return err
		}
		state.NextEligibleAt = &nextEligibleAt
		if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
			return err
		}
		return s.recordMode4PassageEvent(ctx, user.ID, passage, req, now, nil)
	case domain.Mode4ReviewActionDone:
		finalRatings, err := s.applyMode4Ratings(ctx, user.ID, passage, req.Ratings, now, req.ResponseTimeMs)
		if err != nil {
			return err
		}
		if _, err := s.reviewRepo.UpdatePassageStatus(ctx, user.ID, passage.ID, domain.Mode4ReviewPassageStatusCompleted, &now); err != nil {
			return err
		}
		state.ActivePassageID = nil
		state.LastCompletedAt = &now
		state.NextEligibleAt = &nextEligibleAt
		if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
			return err
		}
		return s.recordMode4PassageEvent(ctx, user.ID, passage, req, now, finalRatings)
	default:
		return fmt.Errorf("%w: unsupported mode4 action", domain.ErrValidation)
	}
}

func (s *WeakPassageReviewService) resolveActivePassage(ctx context.Context, state domain.Mode4ReviewState) (*domain.Mode4ReviewPassage, domain.Mode4ReviewState, error) {
	if state.ActivePassageID != nil {
		passage, err := s.reviewRepo.GetPassage(ctx, state.UserID, *state.ActivePassageID)
		switch {
		case err == nil && passage.Status == domain.Mode4ReviewPassageStatusActive:
			return &passage, state, nil
		case err == nil:
			state.ActivePassageID = nil
			if _, upsertErr := s.reviewRepo.UpsertState(ctx, state); upsertErr != nil {
				return nil, state, upsertErr
			}
		case isNotFound(err):
			state.ActivePassageID = nil
			if _, upsertErr := s.reviewRepo.UpsertState(ctx, state); upsertErr != nil {
				return nil, state, upsertErr
			}
		default:
			return nil, state, err
		}
	}

	passage, err := s.reviewRepo.GetActivePassage(ctx, state.UserID)
	if err == nil {
		state.ActivePassageID = &passage.ID
		if _, upsertErr := s.reviewRepo.UpsertState(ctx, state); upsertErr != nil {
			return nil, state, upsertErr
		}
		return &passage, state, nil
	}
	if err != nil && !isNotFound(err) {
		return nil, state, err
	}
	return nil, state, nil
}

func (s *WeakPassageReviewService) generatePassageIfEligible(ctx context.Context, userID uuid.UUID, localDate string, state domain.Mode4ReviewState, now time.Time) (*domain.Mode4ReviewPassage, error) {
	candidates, err := s.selectCandidates(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(candidates) < mode4MinWordCount {
		retryAt := now.Add(mode4RetryBackoff)
		state.NextEligibleAt = &retryAt
		if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
			return nil, err
		}
		return nil, nil
	}

	nextGeneration := state.GenerationCount + 1
	selected, err := s.selectGenerationWords(ctx, userID, nextGeneration, candidates)
	if err != nil {
		return nil, err
	}
	if len(selected) < mode4MinWordCount {
		retryAt := now.Add(mode4RetryBackoff)
		state.NextEligibleAt = &retryAt
		if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
			return nil, err
		}
		return nil, nil
	}

	targetWords := make([]domain.Word, 0, len(selected))
	for _, candidate := range selected {
		targetWords = append(targetWords, candidate.word)
	}

	input := Mode4PassageGenerationInput{
		UserID:      userID,
		LocalDate:   localDate,
		TargetWords: targetWords,
	}
	payload, raw, generationErr := s.generator.GenerateMode4WeakPassage(ctx, input)
	spans, validationIssues := s.validateAndBuildPassage(selected, payload)

	status := domain.LLMRunStatusSuccess
	errorMessage := ""
	if generationErr != nil {
		status = domain.LLMRunStatusFailed
		errorMessage = generationErr.Error()
	} else if len(validationIssues) > 0 {
		status = domain.LLMRunStatusPartial
		errorMessage = strings.Join(validationIssues, "; ")
	}

	run, runErr := s.llmRepo.InsertReturning(ctx, domain.LLMGenerationRun{
		UserID:         userID,
		LocalDate:      localDate,
		Topic:          targetWords[0].Topic,
		RequestedCount: len(selected),
		AcceptedCount:  acceptedCountForMode4(status, len(selected)),
		Attempt:        nextGeneration,
		Status:         status,
		Provider:       domain.DefaultGeminiProvider,
		Model:          "dynamic",
		Prompt:         mode4PromptFamily,
		RawResponse: domain.JSONMap{
			"text":           raw,
			"selected_words": targetWordLabels(targetWords),
		},
		RejectionSummary: domain.JSONMap{
			"validation_issues": validationIssues,
		},
		ErrorMessage: errorMessage,
	})
	if runErr != nil {
		return nil, runErr
	}
	if generationErr != nil {
		return nil, generationErr
	}
	if len(validationIssues) > 0 {
		return nil, fmt.Errorf("mode4 payload validation failed: %s", strings.Join(validationIssues, "; "))
	}

	passage := domain.Mode4ReviewPassage{
		ID:                    uuid.New(),
		UserID:                userID,
		GenerationNumber:      nextGeneration,
		WordIDs:               extractMode4WordIDs(selected),
		SourceWords:           buildMode4SourceWords(selected),
		PlainPassageText:      strings.TrimSpace(payload.PlainPassageText),
		MarkedPassageMarkdown: strings.TrimSpace(payload.MarkedPassageMarkdown),
		PassageSpans:          spans,
		Status:                domain.Mode4ReviewPassageStatusActive,
		LLMRunID:              &run.ID,
	}
	created, err := s.reviewRepo.CreatePassage(ctx, passage)
	if err != nil {
		return nil, err
	}
	state.GenerationCount = nextGeneration
	state.ActivePassageID = &created.ID
	state.NextEligibleAt = nil
	if _, err := s.reviewRepo.UpsertState(ctx, state); err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *WeakPassageReviewService) buildMode4Card(ctx context.Context, passage domain.Mode4ReviewPassage) (domain.Mode4WeakPassageReviewCard, error) {
	words, err := s.wordRepo.ListWordsByIDs(ctx, passage.WordIDs)
	if err != nil {
		return domain.Mode4WeakPassageReviewCard{}, err
	}
	wordMap := make(map[uuid.UUID]domain.Word, len(words))
	for _, word := range words {
		wordMap[word.ID] = word
	}

	targetWords := make([]domain.Word, 0, len(passage.WordIDs))
	for _, wordID := range passage.WordIDs {
		word, ok := wordMap[wordID]
		if !ok {
			return domain.Mode4WeakPassageReviewCard{}, fmt.Errorf("%w: mode4 target word %s missing", domain.ErrNotFound, wordID)
		}
		targetWords = append(targetWords, word)
	}
	return domain.Mode4WeakPassageReviewCard{
		PassageID:             passage.ID,
		GenerationNumber:      passage.GenerationNumber,
		PlainPassageText:      passage.PlainPassageText,
		MarkedPassageMarkdown: passage.MarkedPassageMarkdown,
		PassageSpans:          append([]domain.Mode4PassageSpan(nil), passage.PassageSpans...),
		TargetWords:           targetWords,
	}, nil
}

func (s *WeakPassageReviewService) selectCandidates(ctx context.Context, userID uuid.UUID) ([]mode4Candidate, error) {
	states, err := s.stateRepo.ListMode4Candidates(ctx, userID, mode4CandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	words, err := s.wordRepo.ListWordsByIDs(ctx, extractStateWordIDs(states))
	if err != nil {
		return nil, err
	}
	wordMap := make(map[uuid.UUID]domain.Word, len(words))
	for _, word := range words {
		wordMap[word.ID] = word
	}

	candidates := make([]mode4Candidate, 0, len(states))
	for _, state := range states {
		word, ok := wordMap[state.WordID]
		if !ok || !isMode4EligibleWord(word) {
			continue
		}
		candidates = append(candidates, mode4Candidate{
			state: state,
			word:  word,
		})
	}
	return candidates, nil
}

func (s *WeakPassageReviewService) selectGenerationWords(ctx context.Context, userID uuid.UUID, generationNumber int, candidates []mode4Candidate) ([]mode4Candidate, error) {
	targetCount := determineMode4WordCount(candidates)
	if targetCount < mode4MinWordCount {
		return nil, nil
	}
	if generationNumber%2 == 1 {
		return append([]mode4Candidate(nil), candidates[:targetCount]...), nil
	}

	previous, err := s.reviewRepo.GetPassageByGeneration(ctx, userID, generationNumber-1)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	if err != nil && isNotFound(err) {
		return append([]mode4Candidate(nil), candidates[:targetCount]...), nil
	}

	previousWordIDs := make(map[uuid.UUID]struct{}, len(previous.WordIDs))
	for _, wordID := range previous.WordIDs {
		previousWordIDs[wordID] = struct{}{}
	}

	selected := make([]mode4Candidate, 0, targetCount)
	overlap := make([]mode4Candidate, 0, targetCount)
	for _, candidate := range candidates {
		if _, ok := previousWordIDs[candidate.word.ID]; ok {
			overlap = append(overlap, candidate)
			continue
		}
		selected = append(selected, candidate)
		if len(selected) == targetCount {
			return selected, nil
		}
	}
	for _, candidate := range overlap {
		selected = append(selected, candidate)
		if len(selected) == targetCount {
			break
		}
	}
	return selected, nil
}

func (s *WeakPassageReviewService) validateAndBuildPassage(selected []mode4Candidate, payload domain.Mode4WeakPassagePayload) ([]domain.Mode4PassageSpan, []string) {
	issues := make([]string, 0)
	plain := strings.TrimSpace(payload.PlainPassageText)
	marked := strings.TrimSpace(payload.MarkedPassageMarkdown)
	if plain == "" {
		issues = append(issues, "plain_passage_text is required")
	}
	if marked == "" {
		issues = append(issues, "marked_passage_markdown is required")
	}
	if plain != "" && countMode4Sentences(plain) > mode4SentenceLimit {
		issues = append(issues, "plain_passage_text must contain at most 10 sentences")
	}
	if plain != "" && containsNonEnglishLetters(plain) {
		issues = append(issues, "plain_passage_text must stay in English")
	}

	wordMap := make(map[string]mode4Candidate, len(selected))
	for _, candidate := range selected {
		wordMap[NormalizeWord(candidate.word.Word)] = candidate
	}
	spans, spanErr := buildMode4PassageSpans(marked, wordMap)
	if spanErr != nil {
		issues = append(issues, spanErr.Error())
		return nil, issues
	}

	seen := make(map[uuid.UUID]struct{}, len(selected))
	for _, span := range spans {
		if span.WordID != nil {
			seen[*span.WordID] = struct{}{}
		}
	}
	for _, candidate := range selected {
		if _, ok := seen[candidate.word.ID]; !ok {
			issues = append(issues, fmt.Sprintf("selected word %s is not marked in passage", candidate.word.Word))
		}
	}
	return spans, issues
}

func (s *WeakPassageReviewService) applyMode4Ratings(ctx context.Context, userID uuid.UUID, passage domain.Mode4ReviewPassage, ratings []Mode4WordRatingInput, now time.Time, responseTimeMs int) (map[uuid.UUID]domain.ReviewRating, error) {
	finalRatings := normalizeMode4Ratings(passage.WordIDs, ratings)
	for _, wordID := range passage.WordIDs {
		rating := finalRatings[wordID]
		if rating == domain.RatingNone {
			continue
		}
		state, err := s.stateRepo.Get(ctx, userID, wordID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		if state.Status == domain.WordStatusKnown {
			continue
		}
		updated := ApplyMode4WeakPassageOutcome(state, rating, now, responseTimeMs)
		if _, err := s.stateRepo.Upsert(ctx, updated); err != nil {
			return nil, err
		}
	}
	return finalRatings, nil
}

func (s *WeakPassageReviewService) recordMode4PassageEvent(ctx context.Context, userID uuid.UUID, passage domain.Mode4ReviewPassage, req Mode4CompletionRequest, now time.Time, finalRatings map[uuid.UUID]domain.ReviewRating) error {
	if len(passage.WordIDs) == 0 {
		return fmt.Errorf("%w: mode4 passage has no words", domain.ErrValidation)
	}

	payload := domain.JSONMap{
		"action":            req.Action,
		"passage_id":        passage.ID.String(),
		"generation_number": passage.GenerationNumber,
		"word_ids":          stringifyUUIDs(passage.WordIDs),
	}
	if len(finalRatings) > 0 {
		payload["final_ratings"] = stringifyMode4Ratings(passage.WordIDs, finalRatings)
	}
	if err := s.eventRepo.Insert(ctx, domain.LearningEvent{
		UserID:         userID,
		WordID:         passage.WordIDs[0],
		EventType:      domain.EventTypeMode4Passage,
		EventTime:      now,
		ResponseTimeMs: req.ResponseTimeMs,
		ClientEventID:  req.ClientEventID,
		Payload:        payload,
	}); err != nil {
		return err
	}

	if req.Action != domain.Mode4ReviewActionDone || len(finalRatings) == 0 {
		return nil
	}
	for _, wordID := range passage.WordIDs {
		rating := finalRatings[wordID]
		if rating == domain.RatingNone {
			continue
		}
		if err := s.eventRepo.Insert(ctx, domain.LearningEvent{
			UserID:         userID,
			WordID:         wordID,
			EventType:      domain.EventTypeMode4WordRating,
			EventTime:      now,
			ResponseTimeMs: req.ResponseTimeMs,
			ClientEventID:  "",
			Payload: domain.JSONMap{
				"passage_id":        passage.ID.String(),
				"generation_number": passage.GenerationNumber,
				"rating":            rating,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func determineMode4WordCount(candidates []mode4Candidate) int {
	if len(candidates) < mode4MinWordCount {
		return len(candidates)
	}
	windowCount := minInt(len(candidates), mode4TargetSelectionWindow)
	window := candidates[:windowCount]

	highRiskCount := 0
	mediumRiskCount := 0
	for _, candidate := range window {
		switch {
		case isHighRiskMode4Candidate(candidate.state):
			highRiskCount++
			mediumRiskCount++
		case isMediumRiskMode4Candidate(candidate.state):
			mediumRiskCount++
		}
	}
	switch {
	case highRiskCount >= 3:
		return 4
	case mediumRiskCount >= 3:
		if len(candidates) >= 5 {
			return 5
		}
		return len(candidates)
	default:
		return minInt(len(candidates), mode4MaxWordCount)
	}
}

func isMode4EligibleWord(word domain.Word) bool {
	return strings.TrimSpace(word.Word) != "" &&
		strings.TrimSpace(word.EnglishMeaning) != "" &&
		strings.TrimSpace(word.VietnameseMeaning) != ""
}

func isHighRiskMode4Candidate(state domain.UserWordState) bool {
	return state.Difficulty >= 0.80 ||
		state.WeaknessScore >= 1.80 ||
		state.LastRating == domain.RatingHard ||
		state.LearningStage <= 2
}

func isMediumRiskMode4Candidate(state domain.UserWordState) bool {
	return state.Difficulty >= 0.65 ||
		state.WeaknessScore >= 1.20 ||
		state.Status == domain.WordStatusLearning ||
		state.LastRating == domain.RatingHard
}

func buildMode4PassageSpans(marked string, wordMap map[string]mode4Candidate) ([]domain.Mode4PassageSpan, error) {
	matches := mode4MarkedWordPattern.FindAllStringSubmatchIndex(marked, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("marked_passage_markdown must mark at least one target word")
	}

	spans := make([]domain.Mode4PassageSpan, 0, len(matches)*2+1)
	lastIndex := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		wordStart, wordEnd := match[2], match[3]
		if start > lastIndex {
			spans = append(spans, domain.Mode4PassageSpan{Text: marked[lastIndex:start]})
		}
		targetText := strings.TrimSpace(marked[wordStart:wordEnd])
		normalized := NormalizeWord(targetText)
		candidate, ok := wordMap[normalized]
		if !ok {
			return nil, fmt.Errorf("marked_passage_markdown includes unknown target %q", targetText)
		}
		wordID := candidate.word.ID
		spans = append(spans, domain.Mode4PassageSpan{
			Text:       targetText,
			WordID:     &wordID,
			TargetWord: candidate.word.Word,
		})
		lastIndex = end
	}
	if lastIndex < len(marked) {
		spans = append(spans, domain.Mode4PassageSpan{Text: marked[lastIndex:]})
	}
	return spans, nil
}

func buildMode4SourceWords(selected []mode4Candidate) []domain.Mode4ReviewSourceWord {
	out := make([]domain.Mode4ReviewSourceWord, 0, len(selected))
	for _, candidate := range selected {
		out = append(out, domain.Mode4ReviewSourceWord{
			WordID:         candidate.word.ID,
			Word:           candidate.word.Word,
			NormalizedForm: candidate.word.NormalizedForm,
			Topic:          candidate.word.Topic,
			Level:          candidate.word.Level,
			WeaknessScore:  candidate.state.WeaknessScore,
			NextReviewAt:   candidate.state.NextReviewAt,
			Status:         candidate.state.Status,
			LastRating:     candidate.state.LastRating,
			Difficulty:     candidate.state.Difficulty,
			LearningStage:  candidate.state.LearningStage,
		})
	}
	return out
}

func targetWordLabels(words []domain.Word) []string {
	labels := make([]string, 0, len(words))
	for _, word := range words {
		labels = append(labels, word.Word)
	}
	return labels
}

func extractMode4WordIDs(selected []mode4Candidate) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(selected))
	for _, candidate := range selected {
		ids = append(ids, candidate.word.ID)
	}
	return ids
}

func normalizeMode4Ratings(wordIDs []uuid.UUID, ratings []Mode4WordRatingInput) map[uuid.UUID]domain.ReviewRating {
	allowed := make(map[uuid.UUID]struct{}, len(wordIDs))
	out := make(map[uuid.UUID]domain.ReviewRating, len(wordIDs))
	for _, wordID := range wordIDs {
		allowed[wordID] = struct{}{}
		out[wordID] = domain.RatingNone
	}
	for _, rating := range ratings {
		if _, ok := allowed[rating.WordID]; !ok {
			continue
		}
		switch rating.Rating {
		case domain.RatingEasy, domain.RatingMedium, domain.RatingHard:
			out[rating.WordID] = rating.Rating
		}
	}
	return out
}

func stringifyMode4Ratings(wordIDs []uuid.UUID, ratings map[uuid.UUID]domain.ReviewRating) map[string]string {
	out := make(map[string]string, len(wordIDs))
	for _, wordID := range wordIDs {
		out[wordID.String()] = string(ratings[wordID])
	}
	return out
}

func stringifyUUIDs(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func countMode4Sentences(text string) int {
	return len(mode4SentencePattern.FindAllString(text, -1))
}

func containsNonEnglishLetters(text string) bool {
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

func mode4Eligible(nextEligibleAt *time.Time, now time.Time) bool {
	return nextEligibleAt == nil || !now.Before(*nextEligibleAt)
}

func acceptedCountForMode4(status domain.LLMRunStatus, wordCount int) int {
	if status != domain.LLMRunStatusSuccess {
		return 0
	}
	return wordCount
}

func (s *WeakPassageReviewService) logWarn(message string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(message, args...)
	}
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	exerciseGenerationPrompt              = "context_cluster_exercise"
	exerciseInsufficientWordsMessage      = "You need more weak/review words before starting an exercise."
	exerciseUnavailableMessage            = "Today's exercise is not ready yet. Please try again in a moment."
	exerciseReadyMessage                  = "Exercise ready."
	exerciseReusedMessage                 = "Using today's saved exercise pack."
	exerciseWeakCandidateLimit            = 12
	exerciseDefaultClusterSize            = 5
	exerciseMaxClusterSize                = 6
	exerciseMinClusterSize                = 4
	exerciseMaxDailyGenerationsPerUserDay = 2
)

type ExerciseService struct {
	settingsRepo SettingsRepository
	wordRepo     WordRepository
	stateRepo    WordStateRepository
	packRepo     ExercisePackRepository
	llmRepo      LLMRunRepository
	generator    ExercisePackGenerator
	clock        Clock
	logger       *slog.Logger
}

type exerciseCandidate struct {
	state domain.UserWordState
	word  domain.Word
}

func NewExerciseService(
	settingsRepo SettingsRepository,
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	packRepo ExercisePackRepository,
	llmRepo LLMRunRepository,
	generator ExercisePackGenerator,
	clock Clock,
	logger *slog.Logger,
) *ExerciseService {
	return &ExerciseService{
		settingsRepo: settingsRepo,
		wordRepo:     wordRepo,
		stateRepo:    stateRepo,
		packRepo:     packRepo,
		llmRepo:      llmRepo,
		generator:    generator,
		clock:        clock,
		logger:       logger,
	}
}

func (s *ExerciseService) StartSession(ctx context.Context, user domain.User) (domain.ExerciseSessionResponse, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return domain.ExerciseSessionResponse{}, err
	}

	now := s.clock.Now()
	localDate, _, _, _, err := domain.BoundsForLocalDate(now, settings.Timezone)
	if err != nil {
		return domain.ExerciseSessionResponse{}, err
	}

	selected, err := s.selectWeakCluster(ctx, user.ID)
	if err != nil {
		return domain.ExerciseSessionResponse{}, err
	}
	if len(selected) < exerciseMinClusterSize {
		return domain.ExerciseSessionResponse{
			State:   domain.ExerciseSessionStateInsufficientWords,
			Message: exerciseInsufficientWordsMessage,
		}, nil
	}

	selectedWords := make([]domain.Word, 0, len(selected))
	for _, candidate := range selected {
		selectedWords = append(selectedWords, candidate.word)
	}
	clusterHash := buildExerciseClusterHash(selectedWords)

	existingPack, err := s.packRepo.GetByClusterHash(ctx, user.ID, localDate, clusterHash, domain.ExercisePackTypeContextClusterChallenge)
	if err == nil {
		return buildExerciseReadyResponse(existingPack, false, true, exerciseReusedMessage), nil
	}
	if err != nil && !isNotFound(err) {
		return domain.ExerciseSessionResponse{}, err
	}

	generationCount, err := s.llmRepo.CountByUserLocalDateAndPrompt(ctx, user.ID, localDate, exerciseGenerationPrompt)
	if err != nil {
		return domain.ExerciseSessionResponse{}, err
	}
	if generationCount >= exerciseMaxDailyGenerationsPerUserDay {
		reusedPack, reuseErr := s.packRepo.GetLatestReadyByLocalDate(ctx, user.ID, localDate, domain.ExercisePackTypeContextClusterChallenge)
		if reuseErr == nil {
			return buildExerciseReadyResponse(reusedPack, false, true, exerciseReusedMessage), nil
		}
		if reuseErr != nil && !isNotFound(reuseErr) {
			return domain.ExerciseSessionResponse{}, reuseErr
		}
		return domain.ExerciseSessionResponse{
			State:   domain.ExerciseSessionStateUnavailable,
			Message: exerciseUnavailableMessage,
		}, nil
	}

	anchor := selected[0]
	input := ExercisePackGenerationInput{
		UserID:       user.ID,
		LocalDate:    localDate,
		Topic:        anchor.word.Topic,
		CEFRLevel:    anchor.word.Level,
		ClusterWords: selectedWords,
	}

	payload, raw, generationErr := s.generator.GenerateContextExercisePack(ctx, input)
	validationIssues := validateExercisePayload(payload, selected)

	status := domain.LLMRunStatusSuccess
	errorMessage := ""
	acceptedCount := len(payload.Questions)
	if generationErr != nil {
		status = domain.LLMRunStatusFailed
		errorMessage = generationErr.Error()
		acceptedCount = 0
	} else if len(validationIssues) > 0 {
		status = domain.LLMRunStatusPartial
		errorMessage = strings.Join(validationIssues, "; ")
		acceptedCount = 0
	}

	var llmRunID *uuid.UUID
	run, runErr := s.llmRepo.InsertReturning(ctx, domain.LLMGenerationRun{
		UserID:         user.ID,
		LocalDate:      localDate,
		Topic:          input.Topic,
		RequestedCount: len(selected),
		AcceptedCount:  acceptedCount,
		Attempt:        generationCount + 1,
		Status:         status,
		Provider:       domain.DefaultGeminiProvider,
		Model:          "dynamic",
		Prompt:         exerciseGenerationPrompt,
		RawResponse: domain.JSONMap{
			"text":          raw,
			"cluster_hash":  clusterHash,
			"cluster_words": sourceWordLabels(selected),
			"topic":         input.Topic,
			"cefr_level":    input.CEFRLevel,
		},
		RejectionSummary: domain.JSONMap{
			"validation_issues": validationIssues,
		},
		ErrorMessage: errorMessage,
	})
	if runErr != nil {
		s.logWarn("record exercise llm run", "user_id", user.ID, "local_date", localDate, "error", runErr)
	} else {
		llmRunID = &run.ID
	}

	if generationErr != nil || len(validationIssues) > 0 {
		s.logWarn(
			"exercise generation unavailable",
			"user_id", user.ID,
			"local_date", localDate,
			"cluster_hash", clusterHash,
			"error", errorMessage,
		)
		return domain.ExerciseSessionResponse{
			State:   domain.ExerciseSessionStateUnavailable,
			Message: exerciseUnavailableMessage,
		}, nil
	}

	packID := uuid.New()
	payload.PackID = packID.String()
	pack := domain.ContextExercisePack{
		ID:          packID,
		UserID:      &user.ID,
		LocalDate:   localDate,
		Topic:       payload.Topic,
		CEFRLevel:   payload.CEFRLevel,
		PackType:    payload.PackType,
		ClusterHash: clusterHash,
		SourceWords: buildExerciseSourceWords(selected),
		Payload:     payload,
		Status:      domain.ExercisePackStatusReady,
		LLMRunID:    llmRunID,
	}

	createdPack, err := s.packRepo.Create(ctx, pack)
	if err != nil {
		return domain.ExerciseSessionResponse{}, err
	}
	return buildExerciseReadyResponse(createdPack, true, false, exerciseReadyMessage), nil
}

func (s *ExerciseService) selectWeakCluster(ctx context.Context, userID uuid.UUID) ([]exerciseCandidate, error) {
	states, err := s.stateRepo.ListWeakCandidates(ctx, userID, nil, exerciseWeakCandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}

	wordMap, err := s.loadExerciseWordMap(ctx, extractStateWordIDs(states))
	if err != nil {
		return nil, err
	}

	candidates := make([]exerciseCandidate, 0, len(states))
	for _, state := range states {
		word, ok := wordMap[state.WordID]
		if !ok || !isExerciseEligibleWord(word) {
			continue
		}
		candidates = append(candidates, exerciseCandidate{
			state: state,
			word:  word,
		})
	}
	sort.Slice(candidates, func(i int, j int) bool {
		return compareExerciseCandidateRank(candidates[i], candidates[j]) < 0
	})
	if len(candidates) <= exerciseMinClusterSize {
		return candidates, nil
	}

	anchor := candidates[0]
	targetCount := determineExerciseClusterSize(candidates, anchor)
	if targetCount > len(candidates) {
		targetCount = len(candidates)
	}
	if targetCount == 0 {
		return nil, nil
	}

	rest := append([]exerciseCandidate(nil), candidates[1:]...)
	sort.Slice(rest, func(i int, j int) bool {
		return compareExerciseClusterAffinity(anchor, rest[i], rest[j]) < 0
	})

	selected := []exerciseCandidate{anchor}
	selected = append(selected, rest[:targetCount-1]...)
	return selected, nil
}

func (s *ExerciseService) loadExerciseWordMap(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Word, error) {
	words, err := s.wordRepo.ListWordsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	wordMap := make(map[uuid.UUID]domain.Word, len(words))
	for _, word := range words {
		wordMap[word.ID] = word
	}
	return wordMap, nil
}

func determineExerciseClusterSize(candidates []exerciseCandidate, anchor exerciseCandidate) int {
	if len(candidates) < exerciseMinClusterSize {
		return len(candidates)
	}
	if len(candidates) == exerciseMinClusterSize {
		return exerciseMinClusterSize
	}

	sameTopicCount := 0
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate.word.Topic), strings.TrimSpace(anchor.word.Topic)) {
			sameTopicCount++
		}
	}
	if sameTopicCount >= exerciseMaxClusterSize && len(candidates) >= exerciseMaxClusterSize {
		return exerciseMaxClusterSize
	}
	if len(candidates) < exerciseDefaultClusterSize {
		return len(candidates)
	}
	return exerciseDefaultClusterSize
}

func buildExerciseReadyResponse(pack domain.ContextExercisePack, generatedNow bool, reused bool, message string) domain.ExerciseSessionResponse {
	payload := pack.Payload
	if strings.TrimSpace(payload.PackID) == "" {
		payload.PackID = pack.ID.String()
	}
	return domain.ExerciseSessionResponse{
		State:   domain.ExerciseSessionStateReady,
		Message: message,
		Session: &domain.ExerciseSession{
			SessionID:    uuid.NewString(),
			LocalDate:    pack.LocalDate,
			GeneratedNow: generatedNow,
			Reused:       reused,
			ClusterHash:  pack.ClusterHash,
			ClusterWords: append([]string{}, payload.ClusterWords...),
			PackType:     pack.PackType,
			Topic:        pack.Topic,
			CEFRLevel:    pack.CEFRLevel,
		},
		Pack: &payload,
	}
}

func buildExerciseSourceWords(selected []exerciseCandidate) []domain.ContextExerciseSourceWord {
	sourceWords := make([]domain.ContextExerciseSourceWord, 0, len(selected))
	for _, candidate := range selected {
		sourceWords = append(sourceWords, domain.ContextExerciseSourceWord{
			WordID:         candidate.word.ID,
			Word:           candidate.word.Word,
			NormalizedForm: candidate.word.NormalizedForm,
			Topic:          candidate.word.Topic,
			Level:          candidate.word.Level,
			WeaknessScore:  candidate.state.WeaknessScore,
		})
	}
	return sourceWords
}

func sourceWordLabels(selected []exerciseCandidate) []string {
	labels := make([]string, 0, len(selected))
	for _, candidate := range selected {
		labels = append(labels, candidate.word.Word)
	}
	return labels
}

func buildExerciseClusterHash(words []domain.Word) string {
	keys := make([]string, 0, len(words))
	for _, word := range words {
		normalized := word.NormalizedForm
		if normalized == "" {
			normalized = NormalizeWord(word.Word)
		}
		keys = append(keys, normalized+"|"+NormalizeWord(word.PartOfSpeech))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}

func validateExercisePayload(payload domain.ContextExercisePayload, selected []exerciseCandidate) []string {
	issues := make([]string, 0)

	if payload.PackType != domain.ExercisePackTypeContextClusterChallenge {
		issues = append(issues, "pack_type must be context_cluster_challenge")
	}
	if !isSupportedExerciseCEFRLevel(payload.CEFRLevel) {
		issues = append(issues, "invalid cefr_level")
	}
	if strings.TrimSpace(payload.Topic) == "" {
		issues = append(issues, "topic is required")
	}
	if strings.TrimSpace(payload.Title) == "" {
		issues = append(issues, "title is required")
	}
	if strings.TrimSpace(payload.Passage) == "" {
		issues = append(issues, "passage is required")
	}
	if len(payload.ClusterWords) < exerciseMinClusterSize || len(payload.ClusterWords) > exerciseMaxClusterSize {
		issues = append(issues, "cluster_words must contain 4-6 words")
	}
	if len(payload.ClusterWords) != len(selected) {
		issues = append(issues, "cluster_words must match the selected weak-word count")
	}
	if len(payload.Questions) < len(selected) || len(payload.Questions) > exerciseMaxClusterSize {
		issues = append(issues, "questions must cover all selected words with 4-6 items")
	}

	expectedWords := make(map[string]bool, len(selected))
	for _, candidate := range selected {
		expectedWords[NormalizeWord(candidate.word.Word)] = false
	}
	clusterSet := make(map[string]struct{}, len(payload.ClusterWords))
	for _, clusterWord := range payload.ClusterWords {
		normalized := NormalizeWord(clusterWord)
		if normalized == "" {
			issues = append(issues, "cluster_words cannot contain blank values")
			continue
		}
		clusterSet[normalized] = struct{}{}
	}
	for normalized := range expectedWords {
		if _, ok := clusterSet[normalized]; !ok {
			issues = append(issues, fmt.Sprintf("cluster_words missing selected word %s", normalized))
		}
	}

	for index, question := range payload.Questions {
		prefix := fmt.Sprintf("question %d", index+1)
		if strings.TrimSpace(question.ID) == "" {
			issues = append(issues, prefix+": id is required")
		}
		if !isSupportedExerciseQuestionType(question.Type) {
			issues = append(issues, prefix+": unsupported type")
		}
		if strings.TrimSpace(question.TargetWord) == "" {
			issues = append(issues, prefix+": target_word is required")
		}
		if strings.TrimSpace(question.Prompt) == "" {
			issues = append(issues, prefix+": prompt is required")
		}
		if strings.TrimSpace(question.Explanation) == "" {
			issues = append(issues, prefix+": explanation is required")
		}
		if len(question.Options) != 4 {
			issues = append(issues, prefix+": options must contain exactly 4 choices")
		}
		answerMatches := 0
		for _, option := range question.Options {
			if option == question.Answer {
				answerMatches++
			}
		}
		if answerMatches != 1 {
			issues = append(issues, prefix+": answer must match exactly one option")
		}
		normalizedTarget := NormalizeWord(question.TargetWord)
		if _, ok := expectedWords[normalizedTarget]; ok {
			expectedWords[normalizedTarget] = true
		}
	}
	for normalized, covered := range expectedWords {
		if !covered {
			issues = append(issues, fmt.Sprintf("selected word %s is not targeted by any question", normalized))
		}
	}
	return issues
}

func isExerciseEligibleWord(word domain.Word) bool {
	return strings.TrimSpace(word.Word) != "" &&
		strings.TrimSpace(word.Topic) != "" &&
		word.Level != "" &&
		strings.TrimSpace(word.EnglishMeaning) != "" &&
		strings.TrimSpace(word.VietnameseMeaning) != ""
}

func isSupportedExerciseCEFRLevel(level domain.CEFRLevel) bool {
	switch level {
	case domain.CEFRB1, domain.CEFRB2, domain.CEFRC1, domain.CEFRC2:
		return true
	default:
		return false
	}
}

func isSupportedExerciseQuestionType(questionType domain.ExerciseQuestionType) bool {
	switch questionType {
	case domain.ExerciseQuestionTypeBestFit,
		domain.ExerciseQuestionTypeMeaningMatch,
		domain.ExerciseQuestionTypeDefinitionMatch,
		domain.ExerciseQuestionTypeSentenceUsage,
		domain.ExerciseQuestionTypePassageUnderstanding,
		domain.ExerciseQuestionTypeConfusableChoice:
		return true
	default:
		return false
	}
}

func compareExerciseClusterAffinity(anchor exerciseCandidate, left exerciseCandidate, right exerciseCandidate) int {
	if diff := compareInts(exerciseAffinityRank(anchor, left), exerciseAffinityRank(anchor, right)); diff != 0 {
		return diff
	}
	return compareExerciseCandidateRank(left, right)
}

func exerciseAffinityRank(anchor exerciseCandidate, candidate exerciseCandidate) int {
	sameTopic := strings.EqualFold(strings.TrimSpace(anchor.word.Topic), strings.TrimSpace(candidate.word.Topic))
	sameLevel := anchor.word.Level == candidate.word.Level
	switch {
	case sameTopic && sameLevel:
		return 0
	case sameTopic:
		return 1
	case sameLevel:
		return 2
	default:
		return 3
	}
}

func compareExerciseCandidateRank(left exerciseCandidate, right exerciseCandidate) int {
	if diff := compareInts(exerciseStatusPriority(left.state.Status), exerciseStatusPriority(right.state.Status)); diff != 0 {
		return diff
	}
	if diff := compareInts(right.state.LearningStage, left.state.LearningStage); diff != 0 {
		return diff
	}
	if diff := compareFloatsDesc(left.state.WeaknessScore, right.state.WeaknessScore); diff != 0 {
		return diff
	}
	if diff := compareTimesAsc(lastSeenOrCreated(left.state), lastSeenOrCreated(right.state)); diff != 0 {
		return diff
	}
	return strings.Compare(left.state.WordID.String(), right.state.WordID.String())
}

func exerciseStatusPriority(status domain.WordStatus) int {
	switch status {
	case domain.WordStatusReview:
		return 0
	case domain.WordStatusLearning:
		return 1
	default:
		return 2
	}
}

func lastSeenOrCreated(state domain.UserWordState) time.Time {
	if state.LastSeenAt != nil {
		return *state.LastSeenAt
	}
	return state.CreatedAt
}

func compareInts(left int, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareFloatsDesc(left float64, right float64) int {
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func compareTimesAsc(left time.Time, right time.Time) int {
	switch {
	case left.Before(right):
		return -1
	case left.After(right):
		return 1
	default:
		return 0
	}
}

func (s *ExerciseService) logWarn(message string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(message, args...)
	}
}

package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"log/slog"

	"wordbit-advanced-app/backend/internal/auth"
	"wordbit-advanced-app/backend/internal/config"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

type memoryUserRepo struct {
	user domain.User
}

func (m *memoryUserRepo) GetOrCreateByExternalSubject(ctx context.Context, subject string, email string) (domain.User, error) {
	if m.user.ID == uuid.Nil {
		m.user = domain.User{
			ID:              uuid.New(),
			ExternalSubject: subject,
			Email:           email,
			LastActiveAt:    time.Now().UTC(),
		}
	}
	return m.user, nil
}
func (m *memoryUserRepo) TouchLastActive(ctx context.Context, userID uuid.UUID, at time.Time) error {
	return nil
}
func (m *memoryUserRepo) ListActiveUsers(ctx context.Context, since time.Time) ([]domain.User, error) {
	return []domain.User{m.user}, nil
}

type memorySettingsRepo struct {
	values map[uuid.UUID]domain.UserSettings
}

func (m *memorySettingsRepo) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	if m.values == nil {
		m.values = map[uuid.UUID]domain.UserSettings{}
	}
	if settings, ok := m.values[userID]; ok {
		return settings, nil
	}
	settings := domain.DefaultUserSettings(userID)
	m.values[userID] = settings
	return settings, nil
}

func (m *memorySettingsRepo) Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error) {
	if m.values == nil {
		m.values = map[uuid.UUID]domain.UserSettings{}
	}
	m.values[settings.UserID] = settings
	return settings, nil
}

type memoryWordRepo struct {
	byID map[uuid.UUID]domain.Word
}

func (m *memoryWordRepo) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	if word, ok := m.byID[wordID]; ok {
		return word, nil
	}
	return domain.Word{}, domain.ErrNotFound
}

func (m *memoryWordRepo) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	if m.byID == nil {
		m.byID = map[uuid.UUID]domain.Word{}
	}
	word := domain.Word{
		ID:                 uuid.New(),
		Word:               candidate.Word,
		NormalizedForm:     service.NormalizeWord(candidate.Word),
		CanonicalForm:      candidate.CanonicalForm,
		Lemma:              candidate.Lemma,
		ConfusableGroupKey: candidate.ConfusableGroupKey,
		PartOfSpeech:       candidate.PartOfSpeech,
		Level:              candidate.Level,
		Topic:              candidate.Topic,
		VietnameseMeaning:  candidate.VietnameseMeaning,
		EnglishMeaning:     candidate.EnglishMeaning,
		CommonRate:         candidate.CommonRate,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	m.byID[word.ID] = word
	return word, nil
}
func (m *memoryWordRepo) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	word, ok := m.byID[wordID]
	if !ok {
		return domain.Word{}, domain.ErrNotFound
	}
	word.Word = candidate.Word
	word.NormalizedForm = service.NormalizeWord(candidate.Word)
	word.CanonicalForm = candidate.CanonicalForm
	word.Lemma = candidate.Lemma
	word.WordFamily = candidate.WordFamily
	word.ConfusableGroupKey = candidate.ConfusableGroupKey
	word.PartOfSpeech = candidate.PartOfSpeech
	word.Level = candidate.Level
	word.Topic = candidate.Topic
	word.IPA = candidate.IPA
	word.PronunciationHint = candidate.PronunciationHint
	word.VietnameseMeaning = candidate.VietnameseMeaning
	word.EnglishMeaning = candidate.EnglishMeaning
	word.ExampleSentence1 = candidate.ExampleSentence1
	word.ExampleSentence2 = candidate.ExampleSentence2
	word.CommonRate = candidate.CommonRate
	word.UpdatedAt = time.Now().UTC()
	m.byID[wordID] = word
	return word, nil
}
func (m *memoryWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}
func (m *memoryWordRepo) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	if limit <= 0 {
		return []domain.Word{}, nil
	}
	excluded := make(map[uuid.UUID]struct{}, len(excludeWordIDs))
	for _, id := range excludeWordIDs {
		excluded[id] = struct{}{}
	}
	words := make([]domain.Word, 0, len(m.byID))
	for _, word := range m.byID {
		if word.Level != level || word.Topic != topic {
			continue
		}
		if _, skip := excluded[word.ID]; skip {
			continue
		}
		words = append(words, word)
	}
	return words, nil
}
func (m *memoryWordRepo) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	var words []domain.Word
	for _, id := range ids {
		if word, ok := m.byID[id]; ok {
			words = append(words, word)
		}
	}
	return words, nil
}

type memoryStateRepo struct {
	weakCandidates    []domain.UserWordState
	dueLearningStates []domain.UserWordState
	dueReviewStates   []domain.UserWordState
}

func (m *memoryStateRepo) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	return domain.UserWordState{}, domain.ErrNotFound
}
func (m *memoryStateRepo) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	if learningOnly {
		return append([]domain.UserWordState(nil), m.dueLearningStates...), nil
	}
	return append([]domain.UserWordState(nil), m.dueReviewStates...), nil
}
func (m *memoryStateRepo) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	if limit <= 0 || limit >= len(m.weakCandidates) {
		return append([]domain.UserWordState(nil), m.weakCandidates...), nil
	}
	return append([]domain.UserWordState(nil), m.weakCandidates[:limit]...), nil
}
func (m *memoryStateRepo) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	return []domain.UserWordState{}, nil
}
func (m *memoryStateRepo) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error) {
	return []domain.DictionaryEntry{}, nil
}
func (m *memoryStateRepo) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	return state, nil
}
func (m *memoryStateRepo) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}
func (m *memoryStateRepo) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type memoryPoolRepo struct {
	pool  domain.DailyLearningPool
	items []domain.DailyLearningPoolItem
}

func (m *memoryPoolRepo) GetByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	if m.pool.ID == uuid.Nil || m.pool.LocalDate != localDate {
		return domain.DailyLearningPool{}, nil, domain.ErrNotFound
	}
	return m.pool, m.items, nil
}
func (m *memoryPoolRepo) CreatePoolWithItems(ctx context.Context, pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	pool.ID = uuid.New()
	for i := range items {
		items[i].ID = uuid.New()
		items[i].PoolID = pool.ID
	}
	m.pool = pool
	m.items = items
	return pool, items, nil
}
func (m *memoryPoolRepo) AcquireDailyPoolLock(ctx context.Context, userID uuid.UUID, localDate string) error {
	return nil
}
func (m *memoryPoolRepo) GetNextActionableCard(ctx context.Context, userID uuid.UUID, localDate string, now time.Time) (*domain.DailyLearningPoolItem, error) {
	return nil, nil
}
func (m *memoryPoolRepo) GetPoolItem(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.DailyLearningPoolItem, error) {
	return domain.DailyLearningPoolItem{}, domain.ErrNotFound
}
func (m *memoryPoolRepo) GetLatestCompletedPoolItem(ctx context.Context, userID uuid.UUID, poolID uuid.UUID) (domain.DailyLearningPoolItem, error) {
	return domain.DailyLearningPoolItem{}, domain.ErrNotFound
}
func (m *memoryPoolRepo) MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error {
	return nil
}
func (m *memoryPoolRepo) ReopenPoolItem(ctx context.Context, itemID uuid.UUID) error {
	return nil
}
func (m *memoryPoolRepo) UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error {
	return nil
}
func (m *memoryPoolRepo) AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error) {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	m.items = append(m.items, item)
	return item, nil
}
func (m *memoryPoolRepo) GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error) {
	return len(m.items), nil
}
func (m *memoryPoolRepo) IncrementScheduledCounts(ctx context.Context, poolID uuid.UUID, dueReviewDelta int, shortTermDelta int) error {
	m.pool.DueReviewCount += dueReviewDelta
	m.pool.ShortTermCount += shortTermDelta
	return nil
}
func (m *memoryPoolRepo) IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	m.pool.NewCount += delta
	return nil
}
func (m *memoryPoolRepo) IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	m.pool.WeakCount += delta
	return nil
}
func (m *memoryPoolRepo) DeletePoolItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error {
	return nil
}
func (m *memoryPoolRepo) DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}
func (m *memoryPoolRepo) ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error {
	m.pool = domain.DailyLearningPool{}
	m.items = nil
	return nil
}

type memoryEventRepo struct{}

func (m *memoryEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error { return nil }
func (m *memoryEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

type memoryLLMRepo struct{}

func (m *memoryLLMRepo) Insert(ctx context.Context, run domain.LLMGenerationRun) error { return nil }
func (m *memoryLLMRepo) InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error) {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	return run, nil
}
func (m *memoryLLMRepo) CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error) {
	return 0, nil
}
func (m *memoryLLMRepo) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	return nil, nil
}

type memoryExercisePackRepo struct {
	byCluster map[string]domain.ContextExercisePack
}

func (m *memoryExercisePackRepo) GetByClusterHash(ctx context.Context, userID uuid.UUID, localDate string, clusterHash string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	pack, ok := m.byCluster[userID.String()+"|"+localDate+"|"+clusterHash]
	if !ok {
		return domain.ContextExercisePack{}, domain.ErrNotFound
	}
	return pack, nil
}

func (m *memoryExercisePackRepo) GetLatestReadyByLocalDate(ctx context.Context, userID uuid.UUID, localDate string, packType domain.ExercisePackType) (domain.ContextExercisePack, error) {
	return domain.ContextExercisePack{}, domain.ErrNotFound
}

func (m *memoryExercisePackRepo) Create(ctx context.Context, pack domain.ContextExercisePack) (domain.ContextExercisePack, error) {
	if m.byCluster == nil {
		m.byCluster = map[string]domain.ContextExercisePack{}
	}
	m.byCluster[pack.UserID.String()+"|"+pack.LocalDate+"|"+pack.ClusterHash] = pack
	return pack, nil
}

type staticExerciseGenerator struct{}

func (g *staticExerciseGenerator) GenerateContextExercisePack(ctx context.Context, input service.ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error) {
	clusterWords := make([]string, 0, len(input.ClusterWords))
	questions := make([]domain.ContextExerciseQuestion, 0, len(input.ClusterWords))
	for _, word := range input.ClusterWords {
		clusterWords = append(clusterWords, word.Word)
		questions = append(questions, domain.ContextExerciseQuestion{
			ID:          "q-" + word.NormalizedForm,
			Type:        domain.ExerciseQuestionTypeBestFit,
			TargetWord:  word.Word,
			Prompt:      "Choose the best word for " + word.Word,
			Options:     []string{word.Word, "alpha", "beta", "gamma"},
			Answer:      word.Word,
			Explanation: word.Word + " is the correct answer.",
		})
	}
	return domain.ContextExercisePayload{
		Topic:        input.Topic,
		CEFRLevel:    input.CEFRLevel,
		PackType:     domain.ExercisePackTypeContextClusterChallenge,
		ClusterWords: clusterWords,
		Title:        input.Topic + " Challenge",
		Passage:      "A coherent passage for the exercise pack.",
		Questions:    questions,
		SummaryTip:   "Review the cluster in one scenario.",
	}, "{}", nil
}

type staticGenerator struct{}

func (g *staticGenerator) GenerateCandidates(ctx context.Context, input service.GenerationInput) ([]domain.CandidateWord, string, error) {
	base := []struct {
		word string
		en   string
		vi   string
	}{
		{word: "sustain", en: "maintain", vi: "duy trì"},
		{word: "convey", en: "communicate", vi: "truyền đạt"},
		{word: "adapt", en: "adjust", vi: "thích nghi"},
		{word: "retain", en: "keep", vi: "giữ lại"},
		{word: "expand", en: "grow", vi: "mở rộng"},
		{word: "clarify", en: "make clear", vi: "làm rõ"},
		{word: "outline", en: "summarize", vi: "phác thảo"},
		{word: "execute", en: "carry out", vi: "thực hiện"},
	}
	candidates := make([]domain.CandidateWord, 0, min(input.RequestedCount, len(base)))
	for _, item := range base {
		rate := domain.WordCommonRateCommon
		candidates = append(candidates, domain.CandidateWord{
			Word:              item.word,
			CanonicalForm:     item.word,
			Lemma:             item.word,
			Level:             input.CEFRLevel,
			Topic:             input.Topic,
			EnglishMeaning:    item.en,
			VietnameseMeaning: item.vi,
			CommonRate:        &rate,
		})
		if len(candidates) >= input.RequestedCount {
			break
		}
	}
	return candidates, "{}", nil
}

type failingGenerator struct{}

func (g *failingGenerator) GenerateCandidates(ctx context.Context, input service.GenerationInput) ([]domain.CandidateWord, string, error) {
	return nil, "", errors.New("gemini unavailable")
}

type memoryDynamicReviewPromptRepo struct {
	prompts []domain.DailyDynamicReviewPrompt
}

func (m *memoryDynamicReviewPromptRepo) ListByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) ([]domain.DailyDynamicReviewPrompt, error) {
	out := make([]domain.DailyDynamicReviewPrompt, 0, len(m.prompts))
	for _, prompt := range m.prompts {
		if prompt.UserID == userID && prompt.LocalDate == localDate {
			out = append(out, prompt)
		}
	}
	return out, nil
}

func (m *memoryDynamicReviewPromptRepo) UpsertBatch(ctx context.Context, prompts []domain.DailyDynamicReviewPrompt) ([]domain.DailyDynamicReviewPrompt, error) {
	for _, prompt := range prompts {
		replaced := false
		for idx := range m.prompts {
			if m.prompts[idx].UserID == prompt.UserID && m.prompts[idx].LocalDate == prompt.LocalDate && m.prompts[idx].WordID == prompt.WordID && m.prompts[idx].ReviewMode == prompt.ReviewMode {
				m.prompts[idx] = prompt
				replaced = true
				break
			}
		}
		if !replaced {
			m.prompts = append(m.prompts, prompt)
		}
	}
	return prompts, nil
}

type staticDynamicReviewGenerator struct{}

func (g *staticDynamicReviewGenerator) GenerateDynamicReviewPrompts(ctx context.Context, input service.DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error) {
	items := make([]domain.DynamicReviewPromptBatchItem, 0, len(input.Items))
	for _, item := range input.Items {
		promptText := "Choose the best word for this context."
		if item.ReviewMode == domain.ReviewModeFillBlank {
			promptText = "The company used _____ to enter a more complex market."
		}
		items = append(items, domain.DynamicReviewPromptBatchItem{
			WordID:     item.WordID,
			ReviewMode: item.ReviewMode,
			PromptText: promptText,
		})
	}
	return domain.DynamicReviewPromptBatchPayload{Items: items}, "{}", nil
}

func TestRouterWithDevAuthSettingsAndPool(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	userRepo := &memoryUserRepo{}
	settingsRepo := &memorySettingsRepo{}
	wordRepo := &memoryWordRepo{}
	stateRepo := &memoryStateRepo{}
	poolRepo := &memoryPoolRepo{}
	eventRepo := &memoryEventRepo{}
	llmRepo := &memoryLLMRepo{}
	clock := service.RealClock{}

	identity := service.NewIdentityService(userRepo, clock)
	settingsService := service.NewSettingsService(settingsRepo)
	dictionaryService := service.NewDictionaryService(settingsRepo, wordRepo, stateRepo, poolRepo, clock)
	poolService := service.NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, eventRepo, llmRepo, &staticGenerator{}, clock, logger, true)
	learningService := service.NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, clock, logger, true)
	exerciseService := service.NewExerciseService(settingsRepo, wordRepo, stateRepo, &memoryExercisePackRepo{}, llmRepo, &staticExerciseGenerator{}, clock, logger)
	dynamicReviewService := service.NewDynamicReviewService(&memoryDynamicReviewPromptRepo{}, llmRepo, &staticDynamicReviewGenerator{}, clock, logger)
	verifier := auth.NewVerifier(config.AuthConfig{DevBypass: true, DevSubject: "dev-user", DevEmail: "dev@example.com"}, logger)

	router := NewRouter(config.Config{AdminToken: "secret"}, logger, nil, verifier, identity, settingsService, dictionaryService, poolService, learningService, exerciseService, dynamicReviewService, llmRepo, BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "/v1/me/settings", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for get settings, got %d", resp.Code)
	}

	updateBody := bytes.NewBufferString(`{"cefr_level":"C1","daily_new_word_limit":2,"preferred_meaning_language":"vi","timezone":"Asia/Ho_Chi_Minh","pronunciation_enabled":true,"lock_screen_enabled":false}`)
	req = httptest.NewRequest(http.MethodPut, "/v1/me/settings", updateBody)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for update settings, got %d", resp.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/me/daily-pool", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for daily pool, got %d", resp.Code)
	}

	var payload struct {
		Pool struct {
			NewCount int `json:"new_count"`
		} `json:"pool"`
		Items []struct {
			Word struct {
				CommonRate *string `json:"common_rate"`
			} `json:"word"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode daily pool response: %v", err)
	}
	if payload.Pool.NewCount == 0 || len(payload.Items) == 0 {
		t.Fatalf("expected generated new words in pool, got %+v", payload.Pool)
	}
	if payload.Items[0].Word.CommonRate == nil || *payload.Items[0].Word.CommonRate != "common" {
		t.Fatalf("expected generated word common_rate=common, got %+v", payload.Items[0].Word.CommonRate)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/me/daily-pool/more-words", bytes.NewBufferString(`{"topic":"Society"}`))
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for append more words, got %d body=%s", resp.Code, resp.Body.String())
	}
	var morePayload struct {
		Pool struct {
			NewCount int `json:"new_count"`
		} `json:"pool"`
		Items       []json.RawMessage `json:"items"`
		AppendedNew int               `json:"appended_new"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&morePayload); err != nil {
		t.Fatalf("decode append more words response: %v", err)
	}
	if morePayload.AppendedNew != 2 {
		t.Fatalf("expected appended_new=2, got %d", morePayload.AppendedNew)
	}
	if morePayload.Pool.NewCount <= payload.Pool.NewCount {
		t.Fatalf("expected new_count to increase after append, got before=%d after=%d", payload.Pool.NewCount, morePayload.Pool.NewCount)
	}
	if len(morePayload.Items) <= len(payload.Items) {
		t.Fatalf("expected more pool items after append, got before=%d after=%d", len(payload.Items), len(morePayload.Items))
	}
}

func TestGenerateDynamicReviewPromptsEndpoint(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	userRepo := &memoryUserRepo{}
	settingsRepo := &memorySettingsRepo{}
	wordID := uuid.New()
	wordRepo := &memoryWordRepo{
		byID: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "forecast",
				NormalizedForm:    service.NormalizeWord("forecast"),
				CanonicalForm:     "forecast",
				Lemma:             "forecast",
				Level:             domain.CEFRB2,
				Topic:             "Business",
				EnglishMeaning:    "a prediction",
				VietnameseMeaning: "du bao",
				ExampleSentence1:  "The team prepared a forecast before the launch.",
			},
		},
	}
	stateRepo := &memoryStateRepo{}
	poolRepo := &memoryPoolRepo{}
	eventRepo := &memoryEventRepo{}
	llmRepo := &memoryLLMRepo{}
	promptRepo := &memoryDynamicReviewPromptRepo{}
	clock := service.RealClock{}

	identity := service.NewIdentityService(userRepo, clock)
	settingsService := service.NewSettingsService(settingsRepo)
	dictionaryService := service.NewDictionaryService(settingsRepo, wordRepo, stateRepo, poolRepo, clock)
	poolService := service.NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, eventRepo, llmRepo, &staticGenerator{}, clock, logger, true)
	learningService := service.NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, clock, logger, true)
	exerciseService := service.NewExerciseService(settingsRepo, wordRepo, stateRepo, &memoryExercisePackRepo{}, llmRepo, &staticExerciseGenerator{}, clock, logger)
	dynamicReviewService := service.NewDynamicReviewService(promptRepo, llmRepo, &staticDynamicReviewGenerator{}, clock, logger)
	verifier := auth.NewVerifier(config.AuthConfig{DevBypass: true, DevSubject: "dev-user", DevEmail: "dev@example.com"}, logger)

	router := NewRouter(config.Config{AdminToken: "secret"}, logger, nil, verifier, identity, settingsService, dictionaryService, poolService, learningService, exerciseService, dynamicReviewService, llmRepo, BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "/v1/me/settings", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for get settings, got %d", resp.Code)
	}

	dueAt := time.Now().UTC().Add(-30 * time.Minute)
	stateRepo.dueReviewStates = []domain.UserWordState{{
		UserID:        userRepo.user.ID,
		WordID:        wordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &dueAt,
		WeaknessScore: 0.2,
		Difficulty:    0.3,
	}}

	req = httptest.NewRequest(http.MethodPost, "/v1/me/daily-pool/dynamic-review/generate", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for dynamic prompt generation, got %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		LocalDate      string `json:"local_date"`
		EligibleCount  int    `json:"eligible_count"`
		GeneratedCount int    `json:"generated_count"`
		Message        string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode dynamic prompt generation response: %v", err)
	}
	if payload.EligibleCount != 1 || payload.GeneratedCount != 1 {
		t.Fatalf("expected eligible/generated counts to be 1, got %+v", payload)
	}
	if len(promptRepo.prompts) != 1 {
		t.Fatalf("expected one cached prompt, got %d", len(promptRepo.prompts))
	}
}

func TestDailyPoolFailsWhenInitialGenerationProducesNoCards(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	userRepo := &memoryUserRepo{}
	settingsRepo := &memorySettingsRepo{}
	wordRepo := &memoryWordRepo{}
	stateRepo := &memoryStateRepo{}
	poolRepo := &memoryPoolRepo{}
	eventRepo := &memoryEventRepo{}
	llmRepo := &memoryLLMRepo{}
	clock := service.RealClock{}

	identity := service.NewIdentityService(userRepo, clock)
	settingsService := service.NewSettingsService(settingsRepo)
	dictionaryService := service.NewDictionaryService(settingsRepo, wordRepo, stateRepo, poolRepo, clock)
	poolService := service.NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, eventRepo, llmRepo, &failingGenerator{}, clock, logger, true)
	learningService := service.NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, clock, logger, true)
	exerciseService := service.NewExerciseService(settingsRepo, wordRepo, stateRepo, &memoryExercisePackRepo{}, llmRepo, &staticExerciseGenerator{}, clock, logger)
	dynamicReviewService := service.NewDynamicReviewService(&memoryDynamicReviewPromptRepo{}, llmRepo, &staticDynamicReviewGenerator{}, clock, logger)
	verifier := auth.NewVerifier(config.AuthConfig{DevBypass: true, DevSubject: "dev-user", DevEmail: "dev@example.com"}, logger)

	router := NewRouter(config.Config{AdminToken: "secret"}, logger, nil, verifier, identity, settingsService, dictionaryService, poolService, learningService, exerciseService, dynamicReviewService, llmRepo, BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "/v1/me/daily-pool", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for empty initial pool generation, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestExerciseStartReturnsReadyPack(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	userRepo := &memoryUserRepo{}
	settingsRepo := &memorySettingsRepo{}
	wordRepo := &memoryWordRepo{byID: map[uuid.UUID]domain.Word{}}
	stateRepo := &memoryStateRepo{}
	poolRepo := &memoryPoolRepo{}
	eventRepo := &memoryEventRepo{}
	llmRepo := &memoryLLMRepo{}
	clock := service.RealClock{}

	user, _ := userRepo.GetOrCreateByExternalSubject(context.Background(), "dev-user", "dev@example.com")
	settingsRepo.values = map[uuid.UUID]domain.UserSettings{
		user.ID: {
			UserID:                   user.ID,
			CEFRLevel:                domain.CEFRB2,
			DailyNewWordLimit:        10,
			PreferredMeaningLanguage: domain.MeaningLanguageVietnamese,
			Timezone:                 "Asia/Ho_Chi_Minh",
			PronunciationEnabled:     true,
			LockScreenEnabled:        false,
		},
	}

	baseTime := time.Now().UTC().Add(-24 * time.Hour)
	for _, wordText := range []string{"allocate", "forecast", "revenue", "strategy"} {
		wordID := uuid.New()
		wordRepo.byID[wordID] = domain.Word{
			ID:                wordID,
			Word:              wordText,
			NormalizedForm:    service.NormalizeWord(wordText),
			CanonicalForm:     wordText,
			Lemma:             wordText,
			PartOfSpeech:      "noun",
			Level:             domain.CEFRB2,
			Topic:             "Business",
			VietnameseMeaning: wordText + " vi",
			EnglishMeaning:    wordText + " en",
			ExampleSentence1:  "Example one for " + wordText,
			ExampleSentence2:  "Example two for " + wordText,
		}
		lastSeen := baseTime
		stateRepo.weakCandidates = append(stateRepo.weakCandidates, domain.UserWordState{
			UserID:        user.ID,
			WordID:        wordID,
			Status:        domain.WordStatusReview,
			WeaknessScore: 2.0,
			LastSeenAt:    &lastSeen,
			CreatedAt:     baseTime.Add(-24 * time.Hour),
		})
		baseTime = baseTime.Add(time.Hour)
	}

	identity := service.NewIdentityService(userRepo, clock)
	settingsService := service.NewSettingsService(settingsRepo)
	dictionaryService := service.NewDictionaryService(settingsRepo, wordRepo, stateRepo, poolRepo, clock)
	poolService := service.NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, eventRepo, llmRepo, &staticGenerator{}, clock, logger, true)
	learningService := service.NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, clock, logger, true)
	exerciseService := service.NewExerciseService(settingsRepo, wordRepo, stateRepo, &memoryExercisePackRepo{}, llmRepo, &staticExerciseGenerator{}, clock, logger)
	dynamicReviewService := service.NewDynamicReviewService(&memoryDynamicReviewPromptRepo{}, llmRepo, &staticDynamicReviewGenerator{}, clock, logger)
	verifier := auth.NewVerifier(config.AuthConfig{DevBypass: true, DevSubject: "dev-user", DevEmail: "dev@example.com"}, logger)

	router := NewRouter(config.Config{AdminToken: "secret"}, logger, nil, verifier, identity, settingsService, dictionaryService, poolService, learningService, exerciseService, dynamicReviewService, llmRepo, BuildInfo{})

	req := httptest.NewRequest(http.MethodPost, "/v1/me/exercise/start", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for exercise start, got %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		State   string `json:"state"`
		Message string `json:"message"`
		Session struct {
			GeneratedNow bool     `json:"generated_now"`
			ClusterWords []string `json:"cluster_words"`
		} `json:"session"`
		Pack struct {
			PackType  string `json:"pack_type"`
			Topic     string `json:"topic"`
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		} `json:"pack"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode exercise response: %v", err)
	}
	if payload.State != "ready" {
		t.Fatalf("expected ready state, got %q", payload.State)
	}
	if !payload.Session.GeneratedNow {
		t.Fatalf("expected generated_now=true, got %+v", payload.Session)
	}
	if payload.Pack.PackType != "context_cluster_challenge" {
		t.Fatalf("expected context pack type, got %q", payload.Pack.PackType)
	}
	if payload.Pack.Topic != "Business" {
		t.Fatalf("expected Business topic, got %q", payload.Pack.Topic)
	}
	if len(payload.Session.ClusterWords) != 4 || len(payload.Pack.Questions) != 4 {
		t.Fatalf("expected 4 cluster words and 4 questions, got words=%d questions=%d", len(payload.Session.ClusterWords), len(payload.Pack.Questions))
	}
}

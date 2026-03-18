package service

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type replenishSettingsRepo struct {
	settings domain.UserSettings
}

func (r *replenishSettingsRepo) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	return r.settings, nil
}

func (r *replenishSettingsRepo) Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error) {
	r.settings = settings
	return settings, nil
}

type replenishWordRepo struct {
	words map[uuid.UUID]domain.Word
}

func (r *replenishWordRepo) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	word, ok := r.words[wordID]
	if !ok {
		return domain.Word{}, domain.ErrNotFound
	}
	return word, nil
}

func (r *replenishWordRepo) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	if r.words == nil {
		r.words = map[uuid.UUID]domain.Word{}
	}
	word := domain.Word{
		ID:                uuid.New(),
		Word:              candidate.Word,
		NormalizedForm:    NormalizeWord(candidate.Word),
		CanonicalForm:     candidate.CanonicalForm,
		Lemma:             candidate.Lemma,
		Level:             candidate.Level,
		Topic:             candidate.Topic,
		EnglishMeaning:    candidate.EnglishMeaning,
		VietnameseMeaning: candidate.VietnameseMeaning,
	}
	r.words[word.ID] = word
	return word, nil
}

func (r *replenishWordRepo) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	word, ok := r.words[wordID]
	if !ok {
		return domain.Word{}, domain.ErrNotFound
	}
	word.Word = candidate.Word
	word.NormalizedForm = NormalizeWord(candidate.Word)
	word.CanonicalForm = candidate.CanonicalForm
	word.Lemma = candidate.Lemma
	word.Level = candidate.Level
	word.Topic = candidate.Topic
	word.EnglishMeaning = candidate.EnglishMeaning
	word.VietnameseMeaning = candidate.VietnameseMeaning
	r.words[wordID] = word
	return word, nil
}

func (r *replenishWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}

func (r *replenishWordRepo) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	out := make([]domain.Word, 0, len(ids))
	for _, id := range ids {
		if word, ok := r.words[id]; ok {
			out = append(out, word)
		}
	}
	return out, nil
}

type replenishStateRepo struct {
	states map[uuid.UUID]domain.UserWordState
}

func (r *replenishStateRepo) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	state, ok := r.states[wordID]
	if !ok {
		return domain.UserWordState{}, domain.ErrNotFound
	}
	return state, nil
}

func (r *replenishStateRepo) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	return nil, nil
}

func (r *replenishStateRepo) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	return nil, nil
}

func (r *replenishStateRepo) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	out := make([]domain.UserWordState, 0, len(r.states))
	for _, state := range r.states {
		out = append(out, state)
	}
	return out, nil
}

func (r *replenishStateRepo) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error) {
	return nil, nil
}

func (r *replenishStateRepo) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	if r.states == nil {
		r.states = map[uuid.UUID]domain.UserWordState{}
	}
	r.states[state.WordID] = state
	return state, nil
}

func (r *replenishStateRepo) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	delete(r.states, wordID)
	return nil
}

func (r *replenishStateRepo) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type replenishPoolRepo struct {
	pool  domain.DailyLearningPool
	items []domain.DailyLearningPoolItem
}

func (r *replenishPoolRepo) GetByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	if r.pool.ID == uuid.Nil || r.pool.LocalDate != localDate {
		return domain.DailyLearningPool{}, nil, domain.ErrNotFound
	}
	return r.pool, r.items, nil
}

func (r *replenishPoolRepo) CreatePoolWithItems(ctx context.Context, pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	return domain.DailyLearningPool{}, nil, domain.ErrNotFound
}

func (r *replenishPoolRepo) AcquireDailyPoolLock(ctx context.Context, userID uuid.UUID, localDate string) error {
	return nil
}

func (r *replenishPoolRepo) GetNextActionableCard(ctx context.Context, userID uuid.UUID, localDate string, now time.Time) (*domain.DailyLearningPoolItem, error) {
	return nil, nil
}

func (r *replenishPoolRepo) GetPoolItem(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.DailyLearningPoolItem, error) {
	return domain.DailyLearningPoolItem{}, domain.ErrNotFound
}

func (r *replenishPoolRepo) MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error {
	return nil
}

func (r *replenishPoolRepo) UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error {
	return nil
}

func (r *replenishPoolRepo) AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error) {
	item.ID = uuid.New()
	wordCopy := item.Word
	if wordCopy == nil {
		for _, existing := range r.items {
			if existing.WordID == item.WordID && existing.Word != nil {
				word := *existing.Word
				wordCopy = &word
				break
			}
		}
	}
	item.Word = wordCopy
	r.items = append(r.items, item)
	return item, nil
}

func (r *replenishPoolRepo) GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error) {
	maxOrdinal := 0
	for _, item := range r.items {
		if item.Ordinal > maxOrdinal {
			maxOrdinal = item.Ordinal
		}
	}
	return maxOrdinal, nil
}

func (r *replenishPoolRepo) IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	r.pool.NewCount += delta
	return nil
}

func (r *replenishPoolRepo) DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}

func (r *replenishPoolRepo) ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error {
	return nil
}

type replenishEventRepo struct{}

func (r *replenishEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	return nil
}
func (r *replenishEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

type replenishLLMRepo struct{}

func (r *replenishLLMRepo) Insert(ctx context.Context, run domain.LLMGenerationRun) error { return nil }
func (r *replenishLLMRepo) ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error) {
	return nil, nil
}

type replenishGenerator struct{}

func (g *replenishGenerator) GenerateCandidates(ctx context.Context, input GenerationInput) ([]domain.CandidateWord, string, error) {
	return []domain.CandidateWord{
		{
			Word:              "career",
			CanonicalForm:     "career",
			Lemma:             "career",
			Level:             input.CEFRLevel,
			Topic:             input.Topic,
			EnglishMeaning:    "profession",
			VietnameseMeaning: "su nghiep",
		},
		{
			Word:              "network",
			CanonicalForm:     "network",
			Lemma:             "network",
			Level:             input.CEFRLevel,
			Topic:             input.Topic,
			EnglishMeaning:    "professional connection",
			VietnameseMeaning: "mang luoi",
		},
	}, "{}", nil
}

type replenishClock struct {
	now time.Time
}

func (c replenishClock) Now() time.Time { return c.now }

func TestGetNextCardReplenishesUnknownDailySlotsAtPoolEnd(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID1 := uuid.New()
	wordID2 := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID1: {ID: wordID1, Word: "apply", NormalizedForm: "apply", CanonicalForm: "apply", Lemma: "apply", Level: domain.CEFRB1, Topic: "Work/Career", EnglishMeaning: "request", VietnameseMeaning: "ung tuyen"},
			wordID2: {ID: wordID2, Word: "deadline", NormalizedForm: "deadline", CanonicalForm: "deadline", Lemma: "deadline", Level: domain.CEFRB1, Topic: "Work/Career", EnglishMeaning: "time limit", VietnameseMeaning: "han chot"},
		},
	}
	word1 := wordRepo.words[wordID1]
	word2 := wordRepo.words[wordID2]
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID1: {UserID: userID, WordID: wordID1, Status: domain.WordStatusKnown},
			wordID2: {UserID: userID, WordID: wordID2, Status: domain.WordStatusKnown},
		},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-19",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			NewCount:  2,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:                    uuid.New(),
				PoolID:                poolID,
				UserID:                userID,
				WordID:                wordID1,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				Status:                domain.PoolItemStatusCompleted,
				FirstExposureRequired: true,
				Word:                  &word1,
			},
			{
				ID:                    uuid.New(),
				PoolID:                poolID,
				UserID:                userID,
				WordID:                wordID2,
				Ordinal:               2,
				ItemType:              domain.PoolItemTypeNew,
				Status:                domain.PoolItemStatusCompleted,
				FirstExposureRequired: true,
				Word:                  &word2,
			},
		},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.DefaultUserSettings(userID),
	}

	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		poolRepo,
		&replenishEventRepo{},
		&replenishLLMRepo{},
		&replenishGenerator{},
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)

	card, err := service.GetNextCard(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("GetNextCard returned error: %v", err)
	}
	if card.PoolItem == nil {
		t.Fatalf("expected replenished next card, got nil")
	}
	if len(poolRepo.items) != 4 {
		t.Fatalf("expected 4 items after replenishment, got %d", len(poolRepo.items))
	}
	pendingNew := 0
	for _, item := range poolRepo.items {
		if item.ItemType == domain.PoolItemTypeNew && item.Status == domain.PoolItemStatusPending {
			pendingNew++
		}
	}
	if pendingNew != 2 {
		t.Fatalf("expected 2 replenished pending new items, got %d", pendingNew)
	}
}

package service

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
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
	words       map[uuid.UUID]domain.Word
	bankWordIDs []uuid.UUID
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
		CommonRate:        candidate.CommonRate,
	}
	r.words[word.ID] = word
	r.bankWordIDs = append(r.bankWordIDs, word.ID)
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
	word.CommonRate = candidate.CommonRate
	r.words[wordID] = word
	return word, nil
}

func (r *replenishWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}

func (r *replenishWordRepo) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	if limit <= 0 {
		return []domain.Word{}, nil
	}
	excluded := make(map[uuid.UUID]struct{}, len(excludeWordIDs))
	for _, id := range excludeWordIDs {
		excluded[id] = struct{}{}
	}
	ids := append([]uuid.UUID{}, r.bankWordIDs...)
	sort.Slice(ids, func(i, j int) bool {
		return r.words[ids[i]].Word < r.words[ids[j]].Word
	})

	out := make([]domain.Word, 0, limit)
	for _, id := range ids {
		word := r.words[id]
		if word.Level != level || word.Topic != topic {
			continue
		}
		if _, skip := excluded[id]; skip {
			continue
		}
		out = append(out, word)
		if len(out) == limit {
			break
		}
	}
	return out, nil
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
	states         map[uuid.UUID]domain.UserWordState
	weakCandidates []domain.UserWordState
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
	if limit <= 0 || len(r.weakCandidates) == 0 {
		return nil, nil
	}
	excluded := make(map[uuid.UUID]struct{}, len(excludeWordIDs))
	for _, id := range excludeWordIDs {
		excluded[id] = struct{}{}
	}
	capacity := limit
	if len(r.weakCandidates) < capacity {
		capacity = len(r.weakCandidates)
	}
	out := make([]domain.UserWordState, 0, capacity)
	for _, state := range r.weakCandidates {
		if _, skip := excluded[state.WordID]; skip {
			continue
		}
		out = append(out, state)
		if len(out) == limit {
			break
		}
	}
	return out, nil
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
	for _, item := range r.items {
		if item.ID == itemID && item.UserID == userID {
			return item, nil
		}
	}
	return domain.DailyLearningPoolItem{}, domain.ErrNotFound
}

func (r *replenishPoolRepo) GetLatestCompletedPoolItem(ctx context.Context, userID uuid.UUID, poolID uuid.UUID) (domain.DailyLearningPoolItem, error) {
	var latest domain.DailyLearningPoolItem
	var found bool
	for _, item := range r.items {
		if item.UserID != userID || item.PoolID != poolID || item.Status != domain.PoolItemStatusCompleted || item.CompletedAt == nil {
			continue
		}
		if !found || item.CompletedAt.After(*latest.CompletedAt) || (item.CompletedAt.Equal(*latest.CompletedAt) && item.Ordinal > latest.Ordinal) {
			latest = item
			found = true
		}
	}
	if !found {
		return domain.DailyLearningPoolItem{}, domain.ErrNotFound
	}
	return latest, nil
}

func (r *replenishPoolRepo) MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error {
	for i := range r.items {
		if r.items[i].ID != itemID {
			continue
		}
		r.items[i].Status = domain.PoolItemStatusCompleted
		r.items[i].CompletedAt = &completedAt
		return nil
	}
	return nil
}

func (r *replenishPoolRepo) ReopenPoolItem(ctx context.Context, itemID uuid.UUID) error {
	for i := range r.items {
		if r.items[i].ID != itemID {
			continue
		}
		r.items[i].Status = domain.PoolItemStatusPending
		r.items[i].CompletedAt = nil
		return nil
	}
	return domain.ErrNotFound
}

func (r *replenishPoolRepo) UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error {
	for i := range r.items {
		if r.items[i].ID != itemID {
			continue
		}
		switch kind {
		case domain.RevealKindMeaning:
			r.items[i].RevealedMeaning = true
		case domain.RevealKindExample:
			r.items[i].RevealedExample = true
		}
		return nil
	}
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

func (r *replenishPoolRepo) IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	r.pool.WeakCount += delta
	return nil
}

func (r *replenishPoolRepo) DeletePoolItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error {
	if len(itemIDs) == 0 {
		return nil
	}
	targets := make(map[uuid.UUID]struct{}, len(itemIDs))
	for _, itemID := range itemIDs {
		targets[itemID] = struct{}{}
	}
	filtered := r.items[:0]
	removedReview := 0
	removedShortTerm := 0
	removedWeak := 0
	removedNew := 0
	for _, item := range r.items {
		if item.UserID == userID {
			if _, remove := targets[item.ID]; remove {
				switch item.ItemType {
				case domain.PoolItemTypeReview:
					removedReview++
				case domain.PoolItemTypeShortTerm:
					removedShortTerm++
				case domain.PoolItemTypeWeak:
					removedWeak++
				case domain.PoolItemTypeNew:
					removedNew++
				}
				continue
			}
		}
		filtered = append(filtered, item)
	}
	r.items = filtered
	r.pool.DueReviewCount = maxInt(r.pool.DueReviewCount-removedReview, 0)
	r.pool.ShortTermCount = maxInt(r.pool.ShortTermCount-removedShortTerm, 0)
	r.pool.WeakCount = maxInt(r.pool.WeakCount-removedWeak, 0)
	r.pool.NewCount = maxInt(r.pool.NewCount-removedNew, 0)
	return nil
}

func (r *replenishPoolRepo) DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	filtered := r.items[:0]
	removedNew := 0
	removedWeak := 0
	for _, item := range r.items {
		if item.UserID == userID && item.WordID == wordID {
			switch item.ItemType {
			case domain.PoolItemTypeNew:
				removedNew++
			case domain.PoolItemTypeWeak:
				removedWeak++
			}
			continue
		}
		filtered = append(filtered, item)
	}
	r.items = filtered
	r.pool.NewCount = maxInt(r.pool.NewCount-removedNew, 0)
	r.pool.WeakCount = maxInt(r.pool.WeakCount-removedWeak, 0)
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

type trackingGenerator struct {
	calls      int
	candidates []domain.CandidateWord
}

func (g *trackingGenerator) GenerateCandidates(ctx context.Context, input GenerationInput) ([]domain.CandidateWord, string, error) {
	g.calls++
	if len(g.candidates) > 0 {
		return g.candidates, "{}", nil
	}
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
	settingsRepo.settings.DailyNewWordLimit = 2

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

func TestAppendMoreNewWordsAddsLimitSizedBatch(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	now := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)
	poolID := uuid.New()

	wordRepo := &replenishWordRepo{}
	stateRepo := &replenishStateRepo{}
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-20",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
		},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.DefaultUserSettings(userID),
	}
	settingsRepo.settings.DailyNewWordLimit = 2
	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		poolRepo,
		&replenishEventRepo{},
		&replenishLLMRepo{},
		&trackingGenerator{
			candidates: []domain.CandidateWord{
				{
					Word:              "deadline",
					CanonicalForm:     "deadline",
					Lemma:             "deadline",
					Level:             domain.CEFRB1,
					Topic:             "Work/Career",
					EnglishMeaning:    "time limit",
					VietnameseMeaning: "han chot",
				},
				{
					Word:              "promotion",
					CanonicalForm:     "promotion",
					Lemma:             "promotion",
					Level:             domain.CEFRB1,
					Topic:             "Work/Career",
					EnglishMeaning:    "advancement",
					VietnameseMeaning: "thang tien",
				},
			},
		},
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)

	view, err := service.AppendMoreNewWords(context.Background(), domain.User{ID: userID}, "Work/Career")
	if err != nil {
		t.Fatalf("AppendMoreNewWords returned error: %v", err)
	}
	if view.Pool.NewCount != 2 {
		t.Fatalf("expected new_count to increase to 2, got %d", view.Pool.NewCount)
	}
	if view.AppendedNew != 2 {
		t.Fatalf("expected appended_new=2, got %d", view.AppendedNew)
	}
	if len(view.Items) != 2 {
		t.Fatalf("expected 2 appended new items, got %d", len(view.Items))
	}
	for _, item := range view.Items {
		if item.ItemType != domain.PoolItemTypeNew {
			t.Fatalf("expected appended item type new, got %s", item.ItemType)
		}
		if !item.FirstExposureRequired {
			t.Fatalf("expected appended new item to require first exposure")
		}
	}
}

func TestGetNextCardReplenishesWeakFallbackWhenPoolIsExhausted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	completedWordID := uuid.New()
	fallbackWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(36 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			completedWordID: {
				ID:                completedWordID,
				Word:              "apply",
				NormalizedForm:    "apply",
				CanonicalForm:     "apply",
				Lemma:             "apply",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "request",
				VietnameseMeaning: "ung tuyen",
			},
			fallbackWordID: {
				ID:                fallbackWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
		},
	}
	completedWord := wordRepo.words[completedWordID]
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			fallbackWordID: {
				UserID:        userID,
				WordID:        fallbackWordID,
				Status:        domain.WordStatusReview,
				NextReviewAt:  &futureReview,
				WeaknessScore: 2.3,
			},
		},
		weakCandidates: []domain.UserWordState{
			{
				UserID:        userID,
				WordID:        fallbackWordID,
				Status:        domain.WordStatusReview,
				NextReviewAt:  &futureReview,
				WeaknessScore: 2.3,
			},
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
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:                    uuid.New(),
				PoolID:                poolID,
				UserID:                userID,
				WordID:                completedWordID,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				Status:                domain.PoolItemStatusCompleted,
				FirstExposureRequired: true,
				Word:                  &completedWord,
			},
		},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.DefaultUserSettings(userID),
	}
	settingsRepo.settings.DailyNewWordLimit = 0

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
		t.Fatalf("expected weak fallback card, got nil")
	}
	if card.PoolItem.WordID != fallbackWordID {
		t.Fatalf("expected fallback word %s, got %s", fallbackWordID, card.PoolItem.WordID)
	}
	if card.PoolItem.ItemType != domain.PoolItemTypeWeak {
		t.Fatalf("expected weak fallback item, got %s", card.PoolItem.ItemType)
	}
	if len(poolRepo.items) != 2 {
		t.Fatalf("expected appended weak fallback item, got %d pool items", len(poolRepo.items))
	}
	if poolRepo.pool.WeakCount != 1 {
		t.Fatalf("expected weak count incremented to 1, got %d", poolRepo.pool.WeakCount)
	}
}

func TestGetNextCardPrioritizesUnknownReplenishmentBeforeBonusPractice(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	completedWordID := uuid.New()
	weakWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(36 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			completedWordID: {
				ID:                completedWordID,
				Word:              "apply",
				NormalizedForm:    "apply",
				CanonicalForm:     "apply",
				Lemma:             "apply",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "request",
				VietnameseMeaning: "ung tuyen",
			},
			weakWordID: {
				ID:                weakWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
		},
	}
	completedWord := wordRepo.words[completedWordID]
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			completedWordID: {UserID: userID, WordID: completedWordID, Status: domain.WordStatusKnown},
			weakWordID:      {UserID: userID, WordID: weakWordID, Status: domain.WordStatusReview, NextReviewAt: &futureReview, WeaknessScore: 2.3},
		},
		weakCandidates: []domain.UserWordState{
			{UserID: userID, WordID: weakWordID, Status: domain.WordStatusReview, NextReviewAt: &futureReview, WeaknessScore: 2.3},
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
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:                    uuid.New(),
				PoolID:                poolID,
				UserID:                userID,
				WordID:                completedWordID,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				Status:                domain.PoolItemStatusCompleted,
				FirstExposureRequired: true,
				Word:                  &completedWord,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}
	generator := &trackingGenerator{
		candidates: []domain.CandidateWord{
			{
				Word:              "deadline",
				CanonicalForm:     "deadline",
				Lemma:             "deadline",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "time limit",
				VietnameseMeaning: "han chot",
			},
		},
	}

	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		poolRepo,
		&replenishEventRepo{},
		&replenishLLMRepo{},
		generator,
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)

	card, err := service.GetNextCard(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("GetNextCard returned error: %v", err)
	}
	if card.PoolItem == nil {
		t.Fatalf("expected replenished new card, got nil")
	}
	if card.PoolItem.ItemType != domain.PoolItemTypeNew {
		t.Fatalf("expected replenished new card before weak fallback, got %s", card.PoolItem.ItemType)
	}
	if generator.calls != 1 {
		t.Fatalf("expected generator to run once for unknown replenishment, got %d", generator.calls)
	}
}

func TestGetNextCardReusesStoredBankWordsBeforeGeneratingAgain(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	completedWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			completedWordID: {
				ID:                completedWordID,
				Word:              "apply",
				NormalizedForm:    "apply",
				CanonicalForm:     "apply",
				Lemma:             "apply",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "request",
				VietnameseMeaning: "ung tuyen",
			},
		},
	}
	completedWord := wordRepo.words[completedWordID]
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			completedWordID: {UserID: userID, WordID: completedWordID, Status: domain.WordStatusKnown},
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
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:                    uuid.New(),
				PoolID:                poolID,
				UserID:                userID,
				WordID:                completedWordID,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				Status:                domain.PoolItemStatusCompleted,
				FirstExposureRequired: true,
				Word:                  &completedWord,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}
	generator := &trackingGenerator{
		candidates: []domain.CandidateWord{
			{
				Word:              "career",
				CanonicalForm:     "career",
				Lemma:             "career",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "profession",
				VietnameseMeaning: "su nghiep",
			},
			{
				Word:              "network",
				CanonicalForm:     "network",
				Lemma:             "network",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "professional connection",
				VietnameseMeaning: "mang luoi",
			},
		},
	}

	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		poolRepo,
		&replenishEventRepo{},
		&replenishLLMRepo{},
		generator,
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	)

	firstCard, err := service.GetNextCard(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("first GetNextCard returned error: %v", err)
	}
	if firstCard.PoolItem == nil || firstCard.PoolItem.ItemType != domain.PoolItemTypeNew {
		t.Fatalf("expected first replenished new card, got %#v", firstCard.PoolItem)
	}
	if generator.calls != 1 {
		t.Fatalf("expected one generator call after first replenish, got %d", generator.calls)
	}
	if len(wordRepo.words) != 3 {
		t.Fatalf("expected overflow candidate stored in bank, got %d total words", len(wordRepo.words))
	}

	appendedWordID := poolRepo.items[len(poolRepo.items)-1].WordID
	stateRepo.states[appendedWordID] = domain.UserWordState{UserID: userID, WordID: appendedWordID, Status: domain.WordStatusKnown}
	poolRepo.items[len(poolRepo.items)-1].Status = domain.PoolItemStatusCompleted

	secondCard, err := service.GetNextCard(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("second GetNextCard returned error: %v", err)
	}
	if secondCard.PoolItem == nil || secondCard.PoolItem.ItemType != domain.PoolItemTypeNew {
		t.Fatalf("expected second replenished card from bank, got %#v", secondCard.PoolItem)
	}
	if generator.calls != 1 {
		t.Fatalf("expected bank reuse without extra generator calls, got %d", generator.calls)
	}
}

func TestGetNextCardCreatesBonusPracticeFromFutureReviewItem(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	futureWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(18 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			futureWordID: {
				ID:                futureWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
		},
	}
	futureWord := wordRepo.words[futureWordID]
	state := domain.UserWordState{
		UserID:        userID,
		WordID:        futureWordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &futureReview,
		WeaknessScore: 1.9,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			futureWordID: state,
		},
		weakCandidates: []domain.UserWordState{state},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-19",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			WeakCount: 1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        futureWordID,
				Ordinal:       1,
				ItemType:      domain.PoolItemTypeReview,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				DueAt:         &futureReview,
				Status:        domain.PoolItemStatusPending,
				IsReview:      true,
				BonusPractice: false,
				Metadata:      domain.JSONMap{"weakness_score": 1.9},
				Word:          &futureWord,
			},
		},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.DefaultUserSettings(userID),
	}
	settingsRepo.settings.DailyNewWordLimit = 0

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
		t.Fatalf("expected bonus practice card, got nil")
	}
	if !card.PoolItem.BonusPractice {
		t.Fatalf("expected returned card to be bonus practice")
	}
	if card.PoolItem.DueAt != nil {
		t.Fatalf("expected bonus practice card due_at to be nil, got %v", *card.PoolItem.DueAt)
	}
	if len(poolRepo.items) != 2 {
		t.Fatalf("expected cloned bonus practice item to be appended, got %d items", len(poolRepo.items))
	}
}

func TestGetNextCardBonusPracticeUsesUnseenCandidatesBeforeRepeating(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	firstWordID := uuid.New()
	secondWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(12 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			firstWordID: {
				ID:                firstWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
			secondWordID: {
				ID:                secondWordID,
				Word:              "salary",
				NormalizedForm:    "salary",
				CanonicalForm:     "salary",
				Lemma:             "salary",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "pay from a job",
				VietnameseMeaning: "luong",
			},
		},
	}
	firstWord := wordRepo.words[firstWordID]
	firstState := domain.UserWordState{
		UserID:        userID,
		WordID:        firstWordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &futureReview,
		WeaknessScore: 2.5,
	}
	secondState := domain.UserWordState{
		UserID:        userID,
		WordID:        secondWordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &futureReview,
		WeaknessScore: 1.9,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			firstWordID:  firstState,
			secondWordID: secondState,
		},
		weakCandidates: []domain.UserWordState{firstState, secondState},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-19",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			WeakCount: 1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        firstWordID,
				Ordinal:       1,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 2.5},
				Word:          &firstWord,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}

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

	candidates, err := service.listBonusPracticeCandidates(context.Background(), userID, poolRepo.items, 1)
	if err != nil {
		t.Fatalf("listBonusPracticeCandidates returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one unseen candidate, got %d", len(candidates))
	}
	if candidates[0].WordID != secondWordID {
		t.Fatalf("expected unseen bonus practice word %s, got %s", secondWordID, candidates[0].WordID)
	}
}

func TestGetNextCardBonusPracticeRepeatsAfterCycleIsExhausted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	firstWordID := uuid.New()
	secondWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(12 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			firstWordID: {
				ID:                firstWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
			secondWordID: {
				ID:                secondWordID,
				Word:              "salary",
				NormalizedForm:    "salary",
				CanonicalForm:     "salary",
				Lemma:             "salary",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "pay from a job",
				VietnameseMeaning: "luong",
			},
		},
	}
	firstWord := wordRepo.words[firstWordID]
	secondWord := wordRepo.words[secondWordID]
	firstState := domain.UserWordState{
		UserID:        userID,
		WordID:        firstWordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &futureReview,
		WeaknessScore: 2.5,
	}
	secondState := domain.UserWordState{
		UserID:        userID,
		WordID:        secondWordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &futureReview,
		WeaknessScore: 1.9,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			firstWordID:  firstState,
			secondWordID: secondState,
		},
		weakCandidates: []domain.UserWordState{firstState, secondState},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-19",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			WeakCount: 2,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        firstWordID,
				Ordinal:       1,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 2.5},
				Word:          &firstWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        secondWordID,
				Ordinal:       2,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 1.9},
				Word:          &secondWord,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 0
	settingsRepo := &replenishSettingsRepo{settings: settings}

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

	candidates, err := service.listBonusPracticeCandidates(context.Background(), userID, poolRepo.items, 1)
	if err != nil {
		t.Fatalf("listBonusPracticeCandidates returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one recycled candidate, got %d", len(candidates))
	}
	if candidates[0].WordID != firstWordID {
		t.Fatalf("expected highest-priority recycled bonus word %s, got %s", firstWordID, candidates[0].WordID)
	}
}

func TestGetNextCardBonusPracticeRecyclePrefersLeastRecentWords(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	firstWordID := uuid.New()
	secondWordID := uuid.New()
	thirdWordID := uuid.New()
	fourthWordID := uuid.New()
	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	futureReview := now.Add(12 * time.Hour)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			firstWordID: {
				ID:                firstWordID,
				Word:              "mentor",
				NormalizedForm:    "mentor",
				CanonicalForm:     "mentor",
				Lemma:             "mentor",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "experienced advisor",
				VietnameseMeaning: "nguoi huong dan",
			},
			secondWordID: {
				ID:                secondWordID,
				Word:              "salary",
				NormalizedForm:    "salary",
				CanonicalForm:     "salary",
				Lemma:             "salary",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "pay from a job",
				VietnameseMeaning: "luong",
			},
			thirdWordID: {
				ID:                thirdWordID,
				Word:              "promotion",
				NormalizedForm:    "promotion",
				CanonicalForm:     "promotion",
				Lemma:             "promotion",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "rise to a higher job level",
				VietnameseMeaning: "thang chuc",
			},
			fourthWordID: {
				ID:                fourthWordID,
				Word:              "initiative",
				NormalizedForm:    "initiative",
				CanonicalForm:     "initiative",
				Lemma:             "initiative",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "ability to act without being told",
				VietnameseMeaning: "su chu dong",
			},
		},
	}
	firstWord := wordRepo.words[firstWordID]
	secondWord := wordRepo.words[secondWordID]
	thirdWord := wordRepo.words[thirdWordID]
	fourthWord := wordRepo.words[fourthWordID]
	firstState := domain.UserWordState{UserID: userID, WordID: firstWordID, Status: domain.WordStatusReview, NextReviewAt: &futureReview, WeaknessScore: 4.2}
	secondState := domain.UserWordState{UserID: userID, WordID: secondWordID, Status: domain.WordStatusReview, NextReviewAt: &futureReview, WeaknessScore: 3.8}
	thirdState := domain.UserWordState{UserID: userID, WordID: thirdWordID, Status: domain.WordStatusReview, NextReviewAt: &futureReview, WeaknessScore: 2.4}
	fourthState := domain.UserWordState{UserID: userID, WordID: fourthWordID, Status: domain.WordStatusLearning, LearningStage: 2, NextReviewAt: &futureReview, WeaknessScore: 2.2}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			firstWordID:  firstState,
			secondWordID: secondState,
			thirdWordID:  thirdState,
			fourthWordID: fourthState,
		},
		weakCandidates: []domain.UserWordState{firstState, secondState, thirdState, fourthState},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-19",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			WeakCount: 6,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        firstWordID,
				Ordinal:       1,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 4.2},
				Word:          &firstWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        secondWordID,
				Ordinal:       2,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 3.8},
				Word:          &secondWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        thirdWordID,
				Ordinal:       3,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 2.4},
				Word:          &thirdWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        fourthWordID,
				Ordinal:       4,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeReveal,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 2.2},
				Word:          &fourthWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        firstWordID,
				Ordinal:       5,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 4.2},
				Word:          &firstWord,
			},
			{
				ID:            uuid.New(),
				PoolID:        poolID,
				UserID:        userID,
				WordID:        secondWordID,
				Ordinal:       6,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusCompleted,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 3.8},
				Word:          &secondWord,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 0
	settingsRepo := &replenishSettingsRepo{settings: settings}

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

	candidates, err := service.listBonusPracticeCandidates(context.Background(), userID, poolRepo.items, 2)
	if err != nil {
		t.Fatalf("listBonusPracticeCandidates returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected two recycled candidates, got %d", len(candidates))
	}
	if candidates[0].WordID != thirdWordID {
		t.Fatalf("expected least-recent recycled word %s first, got %s", thirdWordID, candidates[0].WordID)
	}
	if candidates[1].WordID != fourthWordID {
		t.Fatalf("expected second recycled word %s, got %s", fourthWordID, candidates[1].WordID)
	}
}

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

func TestGetOrCreateDailyPoolIncludesOverdueShortTermFromBeforeLocalDayStart(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	now := time.Date(2026, 3, 22, 17, 30, 0, 0, time.UTC)
	dueAt := time.Date(2026, 3, 22, 15, 2, 6, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "merger",
				NormalizedForm:    "merger",
				CanonicalForm:     "merger",
				Lemma:             "merger",
				Level:             domain.CEFRB2,
				Topic:             "Business",
				EnglishMeaning:    "a combination of companies",
				VietnameseMeaning: "su sap nhap",
			},
		},
	}
	stateRepo := &replenishStateRepo{
		dueLearningStates: []domain.UserWordState{{
			UserID:        userID,
			WordID:        wordID,
			Status:        domain.WordStatusLearning,
			LearningStage: 3,
			NextReviewAt:  &dueAt,
			CreatedAt:     now.Add(-48 * time.Hour),
		}},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.UserSettings{
			UserID:                   userID,
			CEFRLevel:                domain.CEFRB2,
			DailyNewWordLimit:        0,
			PreferredMeaningLanguage: domain.MeaningLanguageVietnamese,
			Timezone:                 "Asia/Ho_Chi_Minh",
		},
	}
	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		&replenishPoolRepo{},
		&memoryEventRepoForPoolTests{},
		&replenishLLMRepo{},
		&replenishGenerator{},
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
		true,
	)

	view, err := service.GetOrCreateDailyPool(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("GetOrCreateDailyPool() error = %v", err)
	}
	if view.Pool.ShortTermCount != 1 {
		t.Fatalf("expected short_term_count=1, got %d", view.Pool.ShortTermCount)
	}
	if len(view.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(view.Items))
	}
	if view.Items[0].ItemType != domain.PoolItemTypeShortTerm {
		t.Fatalf("expected short_term item, got %s", view.Items[0].ItemType)
	}
	if view.Items[0].WordID != wordID {
		t.Fatalf("expected overdue word %s, got %s", wordID, view.Items[0].WordID)
	}
}

func TestGetOrCreateDailyPoolIncludesOverdueReviewFromBeforeLocalDayStart(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	now := time.Date(2026, 3, 22, 17, 30, 0, 0, time.UTC)
	dueAt := time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "forecast",
				NormalizedForm:    "forecast",
				CanonicalForm:     "forecast",
				Lemma:             "forecast",
				Level:             domain.CEFRB2,
				Topic:             "Business",
				EnglishMeaning:    "a prediction",
				VietnameseMeaning: "du bao",
			},
		},
	}
	stateRepo := &replenishStateRepo{
		dueReviewStates: []domain.UserWordState{{
			UserID:        userID,
			WordID:        wordID,
			Status:        domain.WordStatusReview,
			LearningStage: 0,
			NextReviewAt:  &dueAt,
			CreatedAt:     now.Add(-72 * time.Hour),
		}},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.UserSettings{
			UserID:                   userID,
			CEFRLevel:                domain.CEFRB2,
			DailyNewWordLimit:        0,
			PreferredMeaningLanguage: domain.MeaningLanguageVietnamese,
			Timezone:                 "Asia/Ho_Chi_Minh",
		},
	}
	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		&replenishPoolRepo{},
		&memoryEventRepoForPoolTests{},
		&replenishLLMRepo{},
		&replenishGenerator{},
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
		true,
	)

	view, err := service.GetOrCreateDailyPool(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("GetOrCreateDailyPool() error = %v", err)
	}
	if view.Pool.DueReviewCount != 1 {
		t.Fatalf("expected due_review_count=1, got %d", view.Pool.DueReviewCount)
	}
	if len(view.Items) != 1 || view.Items[0].ItemType != domain.PoolItemTypeReview {
		t.Fatalf("expected one review item, got %+v", view.Items)
	}
}

func TestGetOrCreateDailyPoolReconcilesMissingScheduledItemEvenWhenWeakExists(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	poolID := uuid.New()
	now := time.Date(2026, 3, 22, 17, 30, 0, 0, time.UTC)
	dueAt := time.Date(2026, 3, 22, 15, 2, 6, 0, time.UTC)
	word := domain.Word{
		ID:                wordID,
		Word:              "merger",
		NormalizedForm:    "merger",
		CanonicalForm:     "merger",
		Lemma:             "merger",
		Level:             domain.CEFRB2,
		Topic:             "Business",
		EnglishMeaning:    "a combination of companies",
		VietnameseMeaning: "su sap nhap",
	}

	wordRepo := &replenishWordRepo{words: map[uuid.UUID]domain.Word{wordID: word}}
	stateRepo := &replenishStateRepo{
		dueLearningStates: []domain.UserWordState{{
			UserID:        userID,
			WordID:        wordID,
			Status:        domain.WordStatusLearning,
			LearningStage: 3,
			NextReviewAt:  &dueAt,
			CreatedAt:     now.Add(-48 * time.Hour),
		}},
	}
	settingsRepo := &replenishSettingsRepo{
		settings: domain.UserSettings{
			UserID:                   userID,
			CEFRLevel:                domain.CEFRB2,
			DailyNewWordLimit:        0,
			PreferredMeaningLanguage: domain.MeaningLanguageVietnamese,
			Timezone:                 "Asia/Ho_Chi_Minh",
		},
	}
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-23",
			Timezone:  "Asia/Ho_Chi_Minh",
			Topic:     "Business",
			WeakCount: 1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:         uuid.New(),
			PoolID:     poolID,
			UserID:     userID,
			WordID:     wordID,
			Ordinal:    1,
			ItemType:   domain.PoolItemTypeWeak,
			ReviewMode: domain.ReviewModeFillBlank,
			Status:     domain.PoolItemStatusPending,
			IsReview:   true,
			Word:       &word,
		}},
	}
	service := NewPoolService(
		settingsRepo,
		wordRepo,
		stateRepo,
		poolRepo,
		&memoryEventRepoForPoolTests{},
		&replenishLLMRepo{},
		&replenishGenerator{},
		replenishClock{now: now},
		slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
		true,
	)

	view, err := service.GetOrCreateDailyPool(context.Background(), domain.User{ID: userID})
	if err != nil {
		t.Fatalf("GetOrCreateDailyPool() error = %v", err)
	}
	if view.Pool.ShortTermCount != 1 {
		t.Fatalf("expected short_term_count=1 after reconciliation, got %d", view.Pool.ShortTermCount)
	}
	shortTermCount := 0
	weakCount := 0
	for _, item := range view.Items {
		switch item.ItemType {
		case domain.PoolItemTypeShortTerm:
			shortTermCount++
		case domain.PoolItemTypeWeak:
			weakCount++
		}
	}
	if shortTermCount != 1 || weakCount != 1 {
		t.Fatalf("expected weak and short_term items to coexist, got %+v", view.Items)
	}
}

type memoryEventRepoForPoolTests struct{}

func (m *memoryEventRepoForPoolTests) Insert(ctx context.Context, event domain.LearningEvent) error {
	return nil
}

func (m *memoryEventRepoForPoolTests) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

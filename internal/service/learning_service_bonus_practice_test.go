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

type captureEventRepo struct {
	events []domain.LearningEvent
}

func (r *captureEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *captureEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

func TestSubmitReviewBonusPracticeDoesNotChangeNextReviewAt(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	nextReview := now.Add(48 * time.Hour)

	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID: {
				UserID:        userID,
				WordID:        wordID,
				Status:        domain.WordStatusReview,
				NextReviewAt:  &nextReview,
				WeaknessScore: 2.0,
				Stability:     1.1,
				Difficulty:    0.7,
			},
		},
	}
	poolRepo := &replenishPoolRepo{
		items: []domain.DailyLearningPoolItem{
			{
				ID:            itemID,
				UserID:        userID,
				WordID:        wordID,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusPending,
				IsReview:      true,
				BonusPractice: true,
				Metadata: domain.JSONMap{
					"bonus_practice": true,
					"weakness_score": 2.0,
				},
			},
		},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(
		&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)},
		stateRepo,
		poolRepo,
		eventRepo,
		nil,
		replenishClock{now: now},
		nil,
	)

	err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     itemID,
		Rating:         domain.RatingEasy,
		ModeUsed:       domain.ReviewModeMultipleChoice,
		ResponseTimeMs: 1800,
		ClientEventID:  "bonus-practice-1",
	})
	if err != nil {
		t.Fatalf("SubmitReview returned error: %v", err)
	}

	updated := stateRepo.states[wordID]
	if updated.NextReviewAt == nil || !updated.NextReviewAt.Equal(nextReview) {
		t.Fatalf("expected next review to remain unchanged, got %#v", updated.NextReviewAt)
	}
	if updated.WeaknessScore >= 2.0 {
		t.Fatalf("expected weakness score to improve after bonus practice, got %.2f", updated.WeaknessScore)
	}
	if len(eventRepo.events) != 1 {
		t.Fatalf("expected one event, got %d", len(eventRepo.events))
	}
	if eventRepo.events[0].EventType != domain.EventTypeBonusPractice {
		t.Fatalf("expected bonus practice event, got %s", eventRepo.events[0].EventType)
	}
	if eventRepo.events[0].Payload["bonus_practice"] != true {
		t.Fatalf("expected bonus_practice payload flag, got %#v", eventRepo.events[0].Payload["bonus_practice"])
	}
}

func TestSubmitRevealBonusPracticeDoesNotChangeWeaknessOrCounters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		kind              domain.RevealKind
		wantMeaningReveal bool
		wantExampleReveal bool
	}{
		{
			name:              "meaning",
			kind:              domain.RevealKindMeaning,
			wantMeaningReveal: true,
		},
		{
			name:              "example",
			kind:              domain.RevealKindExample,
			wantExampleReveal: true,
		},
		{
			name: "hint",
			kind: domain.RevealKindHint,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			wordID := uuid.New()
			itemID := uuid.New()
			now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)

			initial := domain.UserWordState{
				UserID:             userID,
				WordID:             wordID,
				Status:             domain.WordStatusReview,
				WeaknessScore:      3.4,
				RevealMeaningCount: 7,
				RevealExampleCount: 5,
				HintUsedCount:      2,
			}
			stateRepo := &replenishStateRepo{
				states: map[uuid.UUID]domain.UserWordState{
					wordID: initial,
				},
			}
			poolRepo := &replenishPoolRepo{
				items: []domain.DailyLearningPoolItem{
					{
						ID:            itemID,
						UserID:        userID,
						WordID:        wordID,
						ItemType:      domain.PoolItemTypeWeak,
						ReviewMode:    domain.ReviewModeReveal,
						Status:        domain.PoolItemStatusPending,
						IsReview:      true,
						BonusPractice: true,
						Metadata: domain.JSONMap{
							"bonus_practice": true,
							"weakness_score": initial.WeaknessScore,
						},
					},
				},
			}
			eventRepo := &captureEventRepo{}
			service := NewLearningService(
				&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)},
				stateRepo,
				poolRepo,
				eventRepo,
				nil,
				replenishClock{now: now},
				nil,
			)

			err := service.SubmitReveal(context.Background(), domain.User{ID: userID}, RevealRequest{
				PoolItemID:     itemID,
				Kind:           tc.kind,
				ModeUsed:       domain.ReviewModeReveal,
				ResponseTimeMs: 900,
				ClientEventID:  "bonus-reveal-" + tc.name,
			})
			if err != nil {
				t.Fatalf("SubmitReveal returned error: %v", err)
			}

			updated := stateRepo.states[wordID]
			if updated.WeaknessScore != initial.WeaknessScore {
				t.Fatalf("expected weakness to remain %.2f, got %.2f", initial.WeaknessScore, updated.WeaknessScore)
			}
			if updated.RevealMeaningCount != initial.RevealMeaningCount {
				t.Fatalf("expected reveal meaning count to remain %d, got %d", initial.RevealMeaningCount, updated.RevealMeaningCount)
			}
			if updated.RevealExampleCount != initial.RevealExampleCount {
				t.Fatalf("expected reveal example count to remain %d, got %d", initial.RevealExampleCount, updated.RevealExampleCount)
			}
			if updated.HintUsedCount != initial.HintUsedCount {
				t.Fatalf("expected hint count to remain %d, got %d", initial.HintUsedCount, updated.HintUsedCount)
			}
			if poolRepo.items[0].RevealedMeaning != tc.wantMeaningReveal {
				t.Fatalf("expected pool-item revealed_meaning=%v, got %v", tc.wantMeaningReveal, poolRepo.items[0].RevealedMeaning)
			}
			if poolRepo.items[0].RevealedExample != tc.wantExampleReveal {
				t.Fatalf("expected pool-item revealed_example=%v, got %v", tc.wantExampleReveal, poolRepo.items[0].RevealedExample)
			}
			if len(eventRepo.events) != 1 {
				t.Fatalf("expected one reveal event, got %d", len(eventRepo.events))
			}
		})
	}
}

func TestSubmitReviewBonusPracticeRepeatedRevealAndReviewDoesNotInflateWeakness(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	firstItemID := uuid.New()
	secondItemID := uuid.New()
	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	nextReview := now.Add(48 * time.Hour)

	initial := domain.UserWordState{
		UserID:        userID,
		WordID:        wordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &nextReview,
		WeaknessScore: 2.4,
		Stability:     1.1,
		Difficulty:    0.7,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID: initial,
		},
	}
	poolRepo := &replenishPoolRepo{
		items: []domain.DailyLearningPoolItem{
			{
				ID:            firstItemID,
				UserID:        userID,
				WordID:        wordID,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeReveal,
				Status:        domain.PoolItemStatusPending,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": initial.WeaknessScore},
			},
			{
				ID:            secondItemID,
				UserID:        userID,
				WordID:        wordID,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeReveal,
				Status:        domain.PoolItemStatusPending,
				IsReview:      true,
				BonusPractice: true,
				Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": initial.WeaknessScore},
			},
		},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(
		&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)},
		stateRepo,
		poolRepo,
		eventRepo,
		nil,
		replenishClock{now: now},
		nil,
	)

	if err := service.SubmitReveal(context.Background(), domain.User{ID: userID}, RevealRequest{
		PoolItemID:     firstItemID,
		Kind:           domain.RevealKindMeaning,
		ModeUsed:       domain.ReviewModeReveal,
		ResponseTimeMs: 1000,
		ClientEventID:  "bonus-cycle-1-reveal",
	}); err != nil {
		t.Fatalf("first SubmitReveal returned error: %v", err)
	}
	if err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     firstItemID,
		Rating:         domain.RatingMedium,
		ModeUsed:       domain.ReviewModeReveal,
		ResponseTimeMs: 1800,
		ClientEventID:  "bonus-cycle-1-review",
	}); err != nil {
		t.Fatalf("first SubmitReview returned error: %v", err)
	}
	if err := service.SubmitReveal(context.Background(), domain.User{ID: userID}, RevealRequest{
		PoolItemID:     secondItemID,
		Kind:           domain.RevealKindMeaning,
		ModeUsed:       domain.ReviewModeReveal,
		ResponseTimeMs: 1000,
		ClientEventID:  "bonus-cycle-2-reveal",
	}); err != nil {
		t.Fatalf("second SubmitReveal returned error: %v", err)
	}
	if err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     secondItemID,
		Rating:         domain.RatingEasy,
		ModeUsed:       domain.ReviewModeReveal,
		ResponseTimeMs: 1500,
		ClientEventID:  "bonus-cycle-2-review",
	}); err != nil {
		t.Fatalf("second SubmitReview returned error: %v", err)
	}

	updated := stateRepo.states[wordID]
	if updated.WeaknessScore >= initial.WeaknessScore {
		t.Fatalf("expected repeated bonus review to reduce weakness below %.2f, got %.2f", initial.WeaknessScore, updated.WeaknessScore)
	}
	if updated.RevealMeaningCount != 0 {
		t.Fatalf("expected bonus reveals to leave reveal_meaning_count unchanged, got %d", updated.RevealMeaningCount)
	}
	if len(eventRepo.events) != 4 {
		t.Fatalf("expected four bonus events, got %d", len(eventRepo.events))
	}
}

func TestSubmitFirstExposureKnownAppendsReplacementNewCard(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	knownWordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			knownWordID: {
				ID:                knownWordID,
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
	knownWord := wordRepo.words[knownWordID]
	stateRepo := &replenishStateRepo{}
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
				ID:                    itemID,
				PoolID:                poolID,
				UserID:                userID,
				WordID:                knownWordID,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				ReviewMode:            domain.ReviewModeReveal,
				Status:                domain.PoolItemStatusPending,
				FirstExposureRequired: true,
				Word:                  &knownWord,
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
	poolService := NewPoolService(
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
	service := NewLearningService(
		settingsRepo,
		stateRepo,
		poolRepo,
		&captureEventRepo{},
		poolService,
		replenishClock{now: now},
		nil,
	)

	err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionKnown,
		ResponseTimeMs: 900,
		ClientEventID:  "known-top-up",
	})
	if err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}

	if generator.calls != 1 {
		t.Fatalf("expected replacement generation after known exposure, got %d calls", generator.calls)
	}
	if len(poolRepo.items) != 2 {
		t.Fatalf("expected replacement new card appended, got %d pool items", len(poolRepo.items))
	}
	appended := poolRepo.items[1]
	if appended.ItemType != domain.PoolItemTypeNew || appended.Status != domain.PoolItemStatusPending {
		t.Fatalf("expected appended pending new item, got %#v", appended)
	}
	if !appended.FirstExposureRequired {
		t.Fatalf("expected appended card to require first exposure")
	}
}

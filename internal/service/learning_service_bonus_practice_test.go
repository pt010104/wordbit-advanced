package service

import (
	"bytes"
	"context"
	"errors"
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
	events := make([]domain.LearningEvent, 0, len(r.events))
	for i := len(r.events) - 1; i >= 0; i-- {
		event := r.events[i]
		if event.PoolItemID == nil || *event.PoolItemID != itemID {
			continue
		}
		events = append(events, event)
	}
	return events, nil
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

func TestSubmitFirstExposureDontLearnRemovesWordWithoutSavingState(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "competence",
				NormalizedForm:    "competence",
				CanonicalForm:     "competence",
				Lemma:             "competence",
				Level:             domain.CEFRB1,
				Topic:             "Society",
				EnglishMeaning:    "ability",
				VietnameseMeaning: "nang luc",
			},
		},
	}
	word := wordRepo.words[wordID]
	stateRepo := &replenishStateRepo{}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-20",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Society",
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{
			{
				ID:                    itemID,
				PoolID:                poolID,
				UserID:                userID,
				WordID:                wordID,
				Ordinal:               1,
				ItemType:              domain.PoolItemTypeNew,
				ReviewMode:            domain.ReviewModeReveal,
				Status:                domain.PoolItemStatusPending,
				FirstExposureRequired: true,
				Word:                  &word,
			},
		},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}
	generator := &trackingGenerator{
		candidates: []domain.CandidateWord{
			{
				Word:              "citizenship",
				CanonicalForm:     "citizenship",
				Lemma:             "citizenship",
				Level:             domain.CEFRB1,
				Topic:             "Society",
				EnglishMeaning:    "membership of a state",
				VietnameseMeaning: "quyen cong dan",
			},
		},
	}
	eventRepo := &captureEventRepo{}
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
		eventRepo,
		poolService,
		replenishClock{now: now},
		nil,
	)

	err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionDontLearn,
		ResponseTimeMs: 400,
		ClientEventID:  "discard-new-word",
	})
	if err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}
	if len(eventRepo.events) != 1 {
		t.Fatalf("expected one undoable event for dont_learn, got %d", len(eventRepo.events))
	}
	if eventRepo.events[0].EventType != domain.EventTypeFirstExposure {
		t.Fatalf("expected first exposure event for dont_learn, got %s", eventRepo.events[0].EventType)
	}
	if _, ok := stateRepo.states[wordID]; ok {
		t.Fatalf("expected no persisted state for discarded word")
	}
	if len(poolRepo.items) != 2 {
		t.Fatalf("expected discarded card to stay completed and one replacement to be appended, got %d items", len(poolRepo.items))
	}
	if poolRepo.items[0].WordID != wordID || poolRepo.items[0].Status != domain.PoolItemStatusCompleted {
		t.Fatalf("expected original discarded card to stay completed for undo, got %#v", poolRepo.items[0])
	}
	if poolRepo.items[1].WordID == wordID {
		t.Fatalf("expected replacement card to use a different word")
	}
}

func TestUndoLastAnswerKnownRestoresPendingCardAndRemovesReplacement(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 8, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "delegate",
				NormalizedForm:    "delegate",
				CanonicalForm:     "delegate",
				Lemma:             "delegate",
				Level:             domain.CEFRB1,
				Topic:             "Work/Career",
				EnglishMeaning:    "assign work",
				VietnameseMeaning: "uy quyen",
			},
		},
	}
	word := wordRepo.words[wordID]
	stateRepo := &replenishStateRepo{}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Work/Career",
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:                    itemID,
			PoolID:                poolID,
			UserID:                userID,
			WordID:                wordID,
			Ordinal:               1,
			ItemType:              domain.PoolItemTypeNew,
			ReviewMode:            domain.ReviewModeReveal,
			Status:                domain.PoolItemStatusPending,
			FirstExposureRequired: true,
			Word:                  &word,
		}},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}
	generator := &trackingGenerator{
		candidates: []domain.CandidateWord{{
			Word:              "budget",
			CanonicalForm:     "budget",
			Lemma:             "budget",
			Level:             domain.CEFRB1,
			Topic:             "Work/Career",
			EnglishMeaning:    "financial plan",
			VietnameseMeaning: "ngan sach",
		}},
	}
	eventRepo := &captureEventRepo{}
	poolService := NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, &replenishEventRepo{}, &replenishLLMRepo{}, generator, replenishClock{now: now}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
	service := NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, replenishClock{now: now}, nil)

	if err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionKnown,
		ResponseTimeMs: 900,
		ClientEventID:  "known-undo-1",
	}); err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}

	if err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID}); err != nil {
		t.Fatalf("UndoLastAnswer returned error: %v", err)
	}

	if len(poolRepo.items) != 1 {
		t.Fatalf("expected replacement card to be removed after undo, got %d items", len(poolRepo.items))
	}
	if poolRepo.items[0].Status != domain.PoolItemStatusPending {
		t.Fatalf("expected original card to reopen, got %#v", poolRepo.items[0])
	}
	if _, ok := stateRepo.states[wordID]; ok {
		t.Fatalf("expected restored state to be absent for original first exposure")
	}
	if len(eventRepo.events) != 2 || eventRepo.events[1].EventType != domain.EventTypeAnswerUndo {
		t.Fatalf("expected undo audit event, got %#v", eventRepo.events)
	}
}

func TestUndoLastAnswerUnknownRestoresPendingCardAndDeletesFollowUp(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 9, 0, 0, 0, time.UTC)

	word := domain.Word{
		ID:                wordID,
		Word:              "resilient",
		NormalizedForm:    "resilient",
		CanonicalForm:     "resilient",
		Lemma:             "resilient",
		Level:             domain.CEFRB1,
		Topic:             "Society",
		EnglishMeaning:    "able to recover quickly",
		VietnameseMeaning: "kien cuong",
	}
	stateRepo := &replenishStateRepo{}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Society",
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:                    itemID,
			PoolID:                poolID,
			UserID:                userID,
			WordID:                wordID,
			Ordinal:               1,
			ItemType:              domain.PoolItemTypeNew,
			ReviewMode:            domain.ReviewModeReveal,
			Status:                domain.PoolItemStatusPending,
			FirstExposureRequired: true,
			Word:                  &word,
		}},
	}
	settingsRepo := &replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, nil, replenishClock{now: now}, nil)

	if err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionUnknown,
		ResponseTimeMs: 1200,
		ClientEventID:  "unknown-undo-1",
	}); err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}

	if len(poolRepo.items) != 2 {
		t.Fatalf("expected same-day follow-up after unknown, got %d items", len(poolRepo.items))
	}
	if err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID}); err != nil {
		t.Fatalf("UndoLastAnswer returned error: %v", err)
	}

	if len(poolRepo.items) != 1 {
		t.Fatalf("expected follow-up to be deleted after undo, got %d items", len(poolRepo.items))
	}
	if poolRepo.items[0].Status != domain.PoolItemStatusPending {
		t.Fatalf("expected original card to reopen, got %#v", poolRepo.items[0])
	}
	if _, ok := stateRepo.states[wordID]; ok {
		t.Fatalf("expected unknown state to be removed after undo")
	}
}

func TestUndoLastAnswerDontLearnRestoresPendingCardAndDeletesReplacement(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)

	wordRepo := &replenishWordRepo{
		words: map[uuid.UUID]domain.Word{
			wordID: {
				ID:                wordID,
				Word:              "coverage",
				NormalizedForm:    "coverage",
				CanonicalForm:     "coverage",
				Lemma:             "coverage",
				Level:             domain.CEFRB1,
				Topic:             "Media",
				EnglishMeaning:    "reporting",
				VietnameseMeaning: "su dua tin",
			},
		},
	}
	word := wordRepo.words[wordID]
	stateRepo := &replenishStateRepo{}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Media",
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:                    itemID,
			PoolID:                poolID,
			UserID:                userID,
			WordID:                wordID,
			Ordinal:               1,
			ItemType:              domain.PoolItemTypeNew,
			ReviewMode:            domain.ReviewModeReveal,
			Status:                domain.PoolItemStatusPending,
			FirstExposureRequired: true,
			Word:                  &word,
		}},
	}
	settings := domain.DefaultUserSettings(userID)
	settings.DailyNewWordLimit = 1
	settingsRepo := &replenishSettingsRepo{settings: settings}
	generator := &trackingGenerator{
		candidates: []domain.CandidateWord{{
			Word:              "headline",
			CanonicalForm:     "headline",
			Lemma:             "headline",
			Level:             domain.CEFRB1,
			Topic:             "Media",
			EnglishMeaning:    "news title",
			VietnameseMeaning: "tieu de",
		}},
	}
	eventRepo := &captureEventRepo{}
	poolService := NewPoolService(settingsRepo, wordRepo, stateRepo, poolRepo, &replenishEventRepo{}, &replenishLLMRepo{}, generator, replenishClock{now: now}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
	service := NewLearningService(settingsRepo, stateRepo, poolRepo, eventRepo, poolService, replenishClock{now: now}, nil)

	if err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionDontLearn,
		ResponseTimeMs: 400,
		ClientEventID:  "dont-learn-undo-1",
	}); err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}
	if err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID}); err != nil {
		t.Fatalf("UndoLastAnswer returned error: %v", err)
	}
	if len(poolRepo.items) != 1 {
		t.Fatalf("expected replacement card deleted after undo, got %d items", len(poolRepo.items))
	}
	if poolRepo.items[0].Status != domain.PoolItemStatusPending || poolRepo.items[0].WordID != wordID {
		t.Fatalf("expected original dont-learn card reopened, got %#v", poolRepo.items[0])
	}
}

func TestUndoLastAnswerReviewRestoresPreviousStateAndRemovesFollowUp(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 7, 0, 0, 0, time.UTC)
	previousReview := now.Add(-2 * time.Hour)

	word := domain.Word{
		ID:                wordID,
		Word:              "brief",
		NormalizedForm:    "brief",
		CanonicalForm:     "brief",
		Lemma:             "brief",
		Level:             domain.CEFRB1,
		Topic:             "Communication",
		EnglishMeaning:    "short summary",
		VietnameseMeaning: "ban tom tat",
	}
	previousState := domain.UserWordState{
		UserID:          userID,
		WordID:          wordID,
		Status:          domain.WordStatusLearning,
		LastSeenAt:      &previousReview,
		NextReviewAt:    &previousReview,
		IntervalSeconds: int((10 * time.Minute).Seconds()),
		LearningStage:   1,
		Stability:       0.5,
		Difficulty:      0.5,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID: previousState,
		},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:             poolID,
			UserID:         userID,
			LocalDate:      "2026-03-21",
			Timezone:       domain.DefaultTimezone,
			Topic:          "Communication",
			ShortTermCount: 1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:         itemID,
			PoolID:     poolID,
			UserID:     userID,
			WordID:     wordID,
			Ordinal:    1,
			ItemType:   domain.PoolItemTypeShortTerm,
			ReviewMode: domain.ReviewModeReveal,
			Status:     domain.PoolItemStatusPending,
			IsReview:   true,
			Word:       &word,
		}},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)}, stateRepo, poolRepo, eventRepo, nil, replenishClock{now: now}, nil)

	if err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     itemID,
		Rating:         domain.RatingMedium,
		ModeUsed:       domain.ReviewModeReveal,
		ResponseTimeMs: 2000,
		ClientEventID:  "review-undo-1",
	}); err != nil {
		t.Fatalf("SubmitReview returned error: %v", err)
	}
	if len(poolRepo.items) != 2 {
		t.Fatalf("expected follow-up review item appended, got %d items", len(poolRepo.items))
	}
	if err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID}); err != nil {
		t.Fatalf("UndoLastAnswer returned error: %v", err)
	}

	restored := stateRepo.states[wordID]
	if restored.LearningStage != previousState.LearningStage || restored.IntervalSeconds != previousState.IntervalSeconds {
		t.Fatalf("expected previous learning state restored, got %#v", restored)
	}
	if len(poolRepo.items) != 1 || poolRepo.items[0].Status != domain.PoolItemStatusPending {
		t.Fatalf("expected original review item reopened and follow-up removed, got %#v", poolRepo.items)
	}
}

func TestUndoLastAnswerBonusPracticeRestoresPreviousWeakness(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	nextReview := now.Add(48 * time.Hour)

	previousState := domain.UserWordState{
		UserID:        userID,
		WordID:        wordID,
		Status:        domain.WordStatusReview,
		NextReviewAt:  &nextReview,
		WeaknessScore: 2.4,
		Stability:     1.2,
		Difficulty:    0.7,
	}
	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID: previousState,
		},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Mixed Review/Weak",
		},
		items: []domain.DailyLearningPoolItem{{
			ID:            itemID,
			PoolID:        poolID,
			UserID:        userID,
			WordID:        wordID,
			Ordinal:       1,
			ItemType:      domain.PoolItemTypeWeak,
			ReviewMode:    domain.ReviewModeMultipleChoice,
			Status:        domain.PoolItemStatusPending,
			IsReview:      true,
			BonusPractice: true,
		}},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)}, stateRepo, poolRepo, eventRepo, nil, replenishClock{now: now}, nil)

	if err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     itemID,
		Rating:         domain.RatingEasy,
		ModeUsed:       domain.ReviewModeMultipleChoice,
		ResponseTimeMs: 1800,
		ClientEventID:  "bonus-undo-1",
	}); err != nil {
		t.Fatalf("SubmitReview returned error: %v", err)
	}
	if err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID}); err != nil {
		t.Fatalf("UndoLastAnswer returned error: %v", err)
	}
	restored := stateRepo.states[wordID]
	if restored.WeaknessScore != previousState.WeaknessScore {
		t.Fatalf("expected bonus weakness restored, got %.2f want %.2f", restored.WeaknessScore, previousState.WeaknessScore)
	}
	if restored.NextReviewAt == nil || !restored.NextReviewAt.Equal(*previousState.NextReviewAt) {
		t.Fatalf("expected next review restored, got %#v", restored.NextReviewAt)
	}
}

func TestUndoLastAnswerRejectsWhenNotLatestCompleted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID1 := uuid.New()
	wordID2 := uuid.New()
	itemID1 := uuid.New()
	itemID2 := uuid.New()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	completedAt1 := now.Add(-2 * time.Minute)
	completedAt2 := now.Add(-time.Minute)
	previousState := domain.UserWordState{
		UserID:          userID,
		WordID:          wordID1,
		Status:          domain.WordStatusReview,
		IntervalSeconds: int((24 * time.Hour).Seconds()),
		Stability:       1.0,
		Difficulty:      0.6,
	}

	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID1: previousState,
		},
	}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Technology",
		},
		items: []domain.DailyLearningPoolItem{
			{ID: itemID1, PoolID: poolID, UserID: userID, WordID: wordID1, Ordinal: 1, ItemType: domain.PoolItemTypeReview, Status: domain.PoolItemStatusCompleted, IsReview: true, CompletedAt: &completedAt1},
			{ID: itemID2, PoolID: poolID, UserID: userID, WordID: wordID2, Ordinal: 2, ItemType: domain.PoolItemTypeReview, Status: domain.PoolItemStatusCompleted, IsReview: true, CompletedAt: &completedAt2},
		},
	}
	payload := domain.JSONMap{"rating": domain.RatingMedium}
	appendUndoSnapshotPayload(payload, &previousState, true, nil)
	eventRepo := &captureEventRepo{
		events: []domain.LearningEvent{{
			UserID:     userID,
			WordID:     wordID1,
			PoolItemID: &itemID1,
			EventType:  domain.EventTypeReviewAnswer,
			EventTime:  completedAt1,
			Payload:    payload,
		}},
	}
	service := NewLearningService(&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)}, stateRepo, poolRepo, eventRepo, nil, replenishClock{now: now}, nil)

	err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID1})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error for non-latest undo, got %v", err)
	}
}

func TestUndoLastAnswerRejectsWhenDependentItemAlreadyCompleted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 21, 13, 0, 0, 0, time.UTC)

	word := domain.Word{
		ID:                wordID,
		Word:              "policy",
		NormalizedForm:    "policy",
		CanonicalForm:     "policy",
		Lemma:             "policy",
		Level:             domain.CEFRB1,
		Topic:             "Law/Government",
		EnglishMeaning:    "official plan",
		VietnameseMeaning: "chinh sach",
	}
	stateRepo := &replenishStateRepo{}
	poolID := uuid.New()
	poolRepo := &replenishPoolRepo{
		pool: domain.DailyLearningPool{
			ID:        poolID,
			UserID:    userID,
			LocalDate: "2026-03-21",
			Timezone:  domain.DefaultTimezone,
			Topic:     "Law/Government",
			NewCount:  1,
		},
		items: []domain.DailyLearningPoolItem{{
			ID:                    itemID,
			PoolID:                poolID,
			UserID:                userID,
			WordID:                wordID,
			Ordinal:               1,
			ItemType:              domain.PoolItemTypeNew,
			Status:                domain.PoolItemStatusPending,
			FirstExposureRequired: true,
			Word:                  &word,
		}},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)}, stateRepo, poolRepo, eventRepo, nil, replenishClock{now: now}, nil)

	if err := service.SubmitFirstExposure(context.Background(), domain.User{ID: userID}, FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         domain.ExposureActionUnknown,
		ResponseTimeMs: 1300,
		ClientEventID:  "unknown-dependent-complete",
	}); err != nil {
		t.Fatalf("SubmitFirstExposure returned error: %v", err)
	}
	poolRepo.items[1].Status = domain.PoolItemStatusCompleted
	completedAt := now.Add(time.Minute)
	poolRepo.items[1].CompletedAt = &completedAt

	err := service.UndoLastAnswer(context.Background(), domain.User{ID: userID}, UndoLastAnswerRequest{PoolItemID: itemID})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error when dependent item was already answered, got %v", err)
	}
}

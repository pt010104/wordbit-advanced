package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type statisticsClock struct {
	now time.Time
}

func (c statisticsClock) Now() time.Time { return c.now }

type statisticsSettingsRepo struct {
	settings domain.UserSettings
}

func (r *statisticsSettingsRepo) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	return r.settings, nil
}

func (r *statisticsSettingsRepo) Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error) {
	r.settings = settings
	return settings, nil
}

type statisticsWordRepo struct {
	words map[uuid.UUID]domain.Word
}

func (r *statisticsWordRepo) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, nil
}
func (r *statisticsWordRepo) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	return r.words[wordID], nil
}
func (r *statisticsWordRepo) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	return domain.Word{}, nil
}
func (r *statisticsWordRepo) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *statisticsWordRepo) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	return nil, nil
}
func (r *statisticsWordRepo) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	out := make([]domain.Word, 0, len(ids))
	for _, id := range ids {
		if word, ok := r.words[id]; ok {
			out = append(out, word)
		}
	}
	return out, nil
}

type statisticsStateRepo struct {
	states []domain.UserWordState
}

func (r *statisticsStateRepo) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	return domain.UserWordState{}, domain.ErrNotFound
}
func (r *statisticsStateRepo) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *statisticsStateRepo) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *statisticsStateRepo) ListMode4Candidates(ctx context.Context, userID uuid.UUID, limit int) ([]domain.UserWordState, error) {
	return nil, nil
}
func (r *statisticsStateRepo) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	return append([]domain.UserWordState(nil), r.states...), nil
}
func (r *statisticsStateRepo) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, setID *uuid.UUID, limit int, offset int) ([]domain.DictionaryEntry, error) {
	return nil, nil
}
func (r *statisticsStateRepo) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	return state, nil
}
func (r *statisticsStateRepo) SetWordSetForWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID, setID uuid.UUID) error {
	return nil
}
func (r *statisticsStateRepo) BackfillDefaultWordSet(ctx context.Context, userID uuid.UUID, defaultSetID uuid.UUID) error {
	return nil
}
func (r *statisticsStateRepo) GetWordSetIDsForWords(ctx context.Context, userID uuid.UUID, wordIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return nil, nil
}
func (r *statisticsStateRepo) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	return nil
}
func (r *statisticsStateRepo) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	return nil
}

type statisticsEventRepo struct {
	events []domain.LearningEvent
}

func (r *statisticsEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	return nil
}
func (r *statisticsEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}
func (r *statisticsEventRepo) ListByUserTimeRange(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time) ([]domain.LearningEvent, error) {
	out := make([]domain.LearningEvent, 0, len(r.events))
	for _, event := range r.events {
		if event.UserID != userID {
			continue
		}
		if event.EventTime.Before(start) || !event.EventTime.Before(end) {
			continue
		}
		out = append(out, event)
	}
	return out, nil
}

func TestStatisticsServiceAggregatesDashboardData(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID1 := uuid.New()
	wordID2 := uuid.New()
	now := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)
	service := NewStatisticsService(
		&statisticsSettingsRepo{settings: domain.UserSettings{UserID: userID, Timezone: domain.DefaultTimezone}},
		&statisticsWordRepo{words: map[uuid.UUID]domain.Word{
			wordID1: {ID: wordID1, Word: "retain", VietnameseMeaning: "giu lai", EnglishMeaning: "keep"},
			wordID2: {ID: wordID2, Word: "clarify", VietnameseMeaning: "lam ro", EnglishMeaning: "make clear"},
		}},
		&statisticsStateRepo{states: []domain.UserWordState{
			{UserID: userID, WordID: wordID1, Status: domain.WordStatusLearning, LearningStage: 3, WeaknessScore: 2.8, ReviewCount: 6},
			{UserID: userID, WordID: wordID2, Status: domain.WordStatusReview, LearningStage: 1, WeaknessScore: 1.2, ReviewCount: 3},
		}},
		&statisticsEventRepo{events: []domain.LearningEvent{
			{UserID: userID, WordID: wordID1, EventType: domain.EventTypeFirstExposure, EventTime: time.Date(2026, 5, 22, 3, 0, 0, 0, time.UTC)},
			{UserID: userID, WordID: wordID1, EventType: domain.EventTypeReviewAnswer, EventTime: time.Date(2026, 5, 22, 4, 0, 0, 0, time.UTC), ModeUsed: domain.ReviewModeBuildWord},
			{UserID: userID, WordID: wordID2, EventType: domain.EventTypeReviewAnswer, EventTime: time.Date(2026, 5, 23, 4, 0, 0, 0, time.UTC), ModeUsed: domain.ReviewModeFillBlank},
		}},
		statisticsClock{now: now},
	)

	stats, err := service.GetUserStatistics(context.Background(), userID, "7d")
	if err != nil {
		t.Fatalf("GetUserStatistics() error = %v", err)
	}

	if stats.Summary.TotalLearnedWords != 2 {
		t.Fatalf("expected total learned words = 2, got %d", stats.Summary.TotalLearnedWords)
	}
	if stats.Summary.ActiveReviewWords != 2 {
		t.Fatalf("expected active review words = 2, got %d", stats.Summary.ActiveReviewWords)
	}
	if stats.Summary.NewWordCount != 1 || stats.Summary.ReviewCount != 2 {
		t.Fatalf("unexpected summary counts: %+v", stats.Summary)
	}
	if stats.Summary.CurrentStreakDays != 2 {
		t.Fatalf("expected streak = 2, got %d", stats.Summary.CurrentStreakDays)
	}
	if len(stats.ActivitySeries) != 7 {
		t.Fatalf("expected 7 activity points, got %d", len(stats.ActivitySeries))
	}
	if len(stats.TopDifficultWords) != 2 || stats.TopDifficultWords[0].Word != "retain" {
		t.Fatalf("unexpected top difficult words: %+v", stats.TopDifficultWords)
	}
	if got := stats.ModeDistribution[2].Count; got != 1 {
		t.Fatalf("expected build_word count = 1, got %d", got)
	}
	if got := stats.ModeDistribution[3].Count; got != 1 {
		t.Fatalf("expected fill_in_blank count = 1, got %d", got)
	}
}

func TestStatisticsServiceRejectsUnsupportedRange(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	service := NewStatisticsService(
		&statisticsSettingsRepo{settings: domain.UserSettings{UserID: userID, Timezone: domain.DefaultTimezone}},
		&statisticsWordRepo{},
		&statisticsStateRepo{},
		&statisticsEventRepo{},
		statisticsClock{now: time.Now().UTC()},
	)

	if _, err := service.GetUserStatistics(context.Background(), userID, "2d"); err == nil {
		t.Fatal("expected error for unsupported range")
	}
}

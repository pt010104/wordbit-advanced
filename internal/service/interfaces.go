package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type UserRepository interface {
	GetOrCreateByExternalSubject(ctx context.Context, subject string, email string) (domain.User, error)
	TouchLastActive(ctx context.Context, userID uuid.UUID, at time.Time) error
	ListActiveUsers(ctx context.Context, since time.Time) ([]domain.User, error)
}

type SettingsRepository interface {
	Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error)
	Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error)
}

type WordRepository interface {
	UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error)
	GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error)
	UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error)
	ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error)
	ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error)
}

type WordStateRepository interface {
	Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error)
	ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error)
	ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error)
	ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error)
	ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error)
	Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error)
	Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error
	RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error
}

type PoolRepository interface {
	GetByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error)
	CreatePoolWithItems(ctx context.Context, pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error)
	AcquireDailyPoolLock(ctx context.Context, userID uuid.UUID, localDate string) error
	GetNextActionableCard(ctx context.Context, userID uuid.UUID, localDate string, now time.Time) (*domain.DailyLearningPoolItem, error)
	GetPoolItem(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.DailyLearningPoolItem, error)
	MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error
	UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error
	AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error)
	GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error)
	IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error
	IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error
	DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error
	ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error
}

type LearningEventRepository interface {
	Insert(ctx context.Context, event domain.LearningEvent) error
	ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error)
}

type LLMRunRepository interface {
	Insert(ctx context.Context, run domain.LLMGenerationRun) error
	ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error)
}

type CandidateGenerator interface {
	GenerateCandidates(ctx context.Context, input GenerationInput) ([]domain.CandidateWord, string, error)
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type AuthSubject struct {
	Subject string
	Email   string
}

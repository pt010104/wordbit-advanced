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
	ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error)
	ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error)
}

type WordStateRepository interface {
	Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error)
	ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error)
	ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error)
	ListMode4Candidates(ctx context.Context, userID uuid.UUID, limit int) ([]domain.UserWordState, error)
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
	GetLatestCompletedPoolItem(ctx context.Context, userID uuid.UUID, poolID uuid.UUID) (domain.DailyLearningPoolItem, error)
	MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error
	ReopenPoolItem(ctx context.Context, itemID uuid.UUID) error
	UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error
	AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error)
	GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error)
	IncrementScheduledCounts(ctx context.Context, poolID uuid.UUID, dueReviewDelta int, shortTermDelta int) error
	IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error
	IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error
	DeletePoolItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error
	DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error
	ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error
}

type LearningEventRepository interface {
	Insert(ctx context.Context, event domain.LearningEvent) error
	ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error)
}

type LLMRunRepository interface {
	Insert(ctx context.Context, run domain.LLMGenerationRun) error
	InsertReturning(ctx context.Context, run domain.LLMGenerationRun) (domain.LLMGenerationRun, error)
	CountByUserLocalDateAndPrompt(ctx context.Context, userID uuid.UUID, localDate string, prompt string) (int, error)
	ListRecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.LLMGenerationRun, error)
}

type CandidateGenerator interface {
	GenerateCandidates(ctx context.Context, input GenerationInput) ([]domain.CandidateWord, string, error)
}

type ExercisePackRepository interface {
	GetByClusterHash(ctx context.Context, userID uuid.UUID, localDate string, clusterHash string, packType domain.ExercisePackType) (domain.ContextExercisePack, error)
	GetLatestReadyByLocalDate(ctx context.Context, userID uuid.UUID, localDate string, packType domain.ExercisePackType) (domain.ContextExercisePack, error)
	Create(ctx context.Context, pack domain.ContextExercisePack) (domain.ContextExercisePack, error)
}

type Mode4ReviewRepository interface {
	GetOrCreateState(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewState, error)
	UpsertState(ctx context.Context, state domain.Mode4ReviewState) (domain.Mode4ReviewState, error)
	GetActivePassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error)
	GetPassage(ctx context.Context, userID uuid.UUID, passageID uuid.UUID) (domain.Mode4ReviewPassage, error)
	GetPassageByGeneration(ctx context.Context, userID uuid.UUID, generationNumber int) (domain.Mode4ReviewPassage, error)
	GetLatestPassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error)
	CreatePassage(ctx context.Context, passage domain.Mode4ReviewPassage) (domain.Mode4ReviewPassage, error)
	UpdatePassageSkip(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, skipCount int, skippedAt *time.Time) (domain.Mode4ReviewPassage, error)
	UpdatePassageStatus(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, status domain.Mode4ReviewPassageStatus, completedAt *time.Time) (domain.Mode4ReviewPassage, error)
}

type ExercisePackGenerator interface {
	GenerateContextExercisePack(ctx context.Context, input ExercisePackGenerationInput) (domain.ContextExercisePayload, string, error)
}

type Mode4PassageGenerator interface {
	GenerateMode4WeakPassage(ctx context.Context, input Mode4PassageGenerationInput) (domain.Mode4WeakPassagePayload, string, error)
}

type DynamicReviewPromptRepository interface {
	ListByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) ([]domain.DailyDynamicReviewPrompt, error)
	UpsertBatch(ctx context.Context, prompts []domain.DailyDynamicReviewPrompt) ([]domain.DailyDynamicReviewPrompt, error)
}

type DynamicReviewPromptGenerator interface {
	GenerateDynamicReviewPrompts(ctx context.Context, input DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error)
}

type UnknownDailyQuotaManager interface {
	EnsureUnknownDailyQuota(ctx context.Context, user domain.User, sourcePoolItemID *uuid.UUID) ([]uuid.UUID, error)
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

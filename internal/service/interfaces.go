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
	AppendGeneratedExamples(ctx context.Context, wordID uuid.UUID, examples []string, maxGeneratedExamples int) ([]string, error)
}

type WordStateRepository interface {
	Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error)
	ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error)
	ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error)
	ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error)
	ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, setID *uuid.UUID, limit int, offset int) ([]domain.DictionaryEntry, error)
	Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error)
	SetWordSetForWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID, setID uuid.UUID) error
	BackfillDefaultWordSet(ctx context.Context, userID uuid.UUID, defaultSetID uuid.UUID) error
	GetWordSetIDsForWords(ctx context.Context, userID uuid.UUID, wordIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error
	RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error
}

type WordSetRepository interface {
	List(ctx context.Context, userID uuid.UUID) ([]domain.WordSet, error)
	Get(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (domain.WordSet, error)
	GetDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error)
	Create(ctx context.Context, set domain.WordSet) (domain.WordSet, error)
	Update(ctx context.Context, set domain.WordSet) (domain.WordSet, error)
	Delete(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error
	EnsureDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error)
}

type RecordingRepository interface {
	Upsert(ctx context.Context, recording domain.UserWordRecording) (domain.UserWordRecording, error)
	Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordRecording, error)
}

type ObjectStorage interface {
	Put(ctx context.Context, objectKey string, contentType string, data []byte) error
	PresignDownload(ctx context.Context, objectKey string, expiresIn time.Duration) (string, error)
}

type WordImportBufferRepository interface {
	List(ctx context.Context, userID uuid.UUID, setID *uuid.UUID) ([]domain.WordImportBufferItem, error)
	Get(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.WordImportBufferItem, error)
	Create(ctx context.Context, item domain.WordImportBufferItem) (domain.WordImportBufferItem, error)
	UpdateCandidate(ctx context.Context, userID uuid.UUID, itemID uuid.UUID, candidate domain.CandidateWord) (domain.WordImportBufferItem, error)
	MarkImported(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.WordImportBufferItem, error)
	Delete(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) error
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
	UpdatePendingPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) error
	AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error)
	GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error)
	IncrementScheduledCounts(ctx context.Context, poolID uuid.UUID, dueReviewDelta int, shortTermDelta int) error
	IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error
	IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error
	DeletePoolItems(ctx context.Context, userID uuid.UUID, itemIDs []uuid.UUID) error
	DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error
	DeletePendingItemsBeforeLocalDate(ctx context.Context, userID uuid.UUID, localDate string) (int64, error)
	ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error
}

type LearningEventRepository interface {
	Insert(ctx context.Context, event domain.LearningEvent) error
	ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error)
	ListByUserTimeRange(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time) ([]domain.LearningEvent, error)
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

type DynamicReviewPromptRepository interface {
	ListByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) ([]domain.DailyDynamicReviewPrompt, error)
	ListLatestForUserWords(ctx context.Context, userID uuid.UUID, wordIDs []uuid.UUID) ([]domain.DailyDynamicReviewPrompt, error)
	UpsertBatch(ctx context.Context, prompts []domain.DailyDynamicReviewPrompt) ([]domain.DailyDynamicReviewPrompt, error)
}

type DynamicReviewPromptGenerator interface {
	GenerateDynamicReviewPrompts(ctx context.Context, input DynamicReviewPromptGenerationInput) (domain.DynamicReviewPromptBatchPayload, string, error)
}

type PromptTester interface {
	GeneratePromptResponse(ctx context.Context, prompt string) (string, string, string, error)
}

type UnknownDailyQuotaManager interface {
	ReconcileUnknownDailyBuffer(ctx context.Context, user domain.User) (UnknownDailyBufferMutation, error)
	MaintainNewWordBuffer(ctx context.Context, user domain.User) (UnknownDailyBufferMutation, error)
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

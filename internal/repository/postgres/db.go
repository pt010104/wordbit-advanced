package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type dbtx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repositories struct {
	Users    *UserRepository
	Settings *SettingsRepository
	Words    *WordRepository
	States   *WordStateRepository
	Pools    *PoolRepository
	Events   *LearningEventRepository
	LLMRuns  *LLMRunRepository
}

func NewRepositories(pool *pgxpool.Pool) *Repositories {
	return &Repositories{
		Users:    &UserRepository{pool: pool},
		Settings: &SettingsRepository{pool: pool},
		Words:    &WordRepository{pool: pool},
		States:   &WordStateRepository{pool: pool},
		Pools:    &PoolRepository{pool: pool},
		Events:   &LearningEventRepository{pool: pool},
		LLMRuns:  &LLMRunRepository{pool: pool},
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idx_learning_events_client_event") {
			return domain.ErrDuplicateClientEvent
		}
	}
	return err
}

func toJSONMap(value []byte) domain.JSONMap {
	if len(value) == 0 {
		return domain.JSONMap{}
	}
	var out domain.JSONMap
	if err := json.Unmarshal(value, &out); err != nil {
		return domain.JSONMap{}
	}
	return out
}

func fromJSONMap(value domain.JSONMap) []byte {
	if len(value) == 0 {
		return []byte("{}")
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return bytes
}

func nullableUUIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func inClauseUUIDs(ids []uuid.UUID) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func joinPlaceholders(offset int, count int) string {
	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		parts = append(parts, fmt.Sprintf("$%d", offset+i))
	}
	return strings.Join(parts, ", ")
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.ExternalSubject,
		&user.Email,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastActiveAt,
	)
	return user, mapError(err)
}

func scanSettings(row pgx.Row) (domain.UserSettings, error) {
	var settings domain.UserSettings
	err := row.Scan(
		&settings.UserID,
		&settings.CEFRLevel,
		&settings.DailyNewWordLimit,
		&settings.PreferredMeaningLanguage,
		&settings.Timezone,
		&settings.PronunciationEnabled,
		&settings.LockScreenEnabled,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	return settings, mapError(err)
}

func scanWord(row pgx.Row) (domain.Word, error) {
	var word domain.Word
	var metadata []byte
	err := row.Scan(
		&word.ID,
		&word.Word,
		&word.NormalizedForm,
		&word.CanonicalForm,
		&word.Lemma,
		&word.WordFamily,
		&word.ConfusableGroupKey,
		&word.PartOfSpeech,
		&word.Level,
		&word.Topic,
		&word.IPA,
		&word.PronunciationHint,
		&word.VietnameseMeaning,
		&word.EnglishMeaning,
		&word.ExampleSentence1,
		&word.ExampleSentence2,
		&word.SourceProvider,
		&metadata,
		&word.CreatedAt,
		&word.UpdatedAt,
	)
	word.SourceMetadata = toJSONMap(metadata)
	return word, mapError(err)
}

func scanState(row pgx.Row) (domain.UserWordState, error) {
	var state domain.UserWordState
	var lastRating string
	var lastMode string
	err := row.Scan(
		&state.UserID,
		&state.WordID,
		&state.Status,
		&state.FirstSeenAt,
		&state.LastSeenAt,
		&lastRating,
		&state.NextReviewAt,
		&state.IntervalSeconds,
		&state.Stability,
		&state.Difficulty,
		&state.ReviewCount,
		&state.WrongCount,
		&state.EasyCount,
		&state.MediumCount,
		&state.HardCount,
		&state.HintUsedCount,
		&state.RevealMeaningCount,
		&state.RevealExampleCount,
		&state.AvgResponseTimeMs,
		&state.WeaknessScore,
		&state.LearningStage,
		&lastMode,
		&state.KnownAt,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	state.LastRating = domain.ReviewRating(lastRating)
	state.LastMode = domain.ReviewMode(lastMode)
	return state, mapError(err)
}

func scanPool(row pgx.Row) (domain.DailyLearningPool, error) {
	var pool domain.DailyLearningPool
	var localDate time.Time
	err := row.Scan(
		&pool.ID,
		&pool.UserID,
		&localDate,
		&pool.Timezone,
		&pool.Topic,
		&pool.DueReviewCount,
		&pool.ShortTermCount,
		&pool.WeakCount,
		&pool.NewCount,
		&pool.GeneratedAt,
		&pool.CreatedAt,
		&pool.UpdatedAt,
	)
	pool.LocalDate = localDate.Format("2006-01-02")
	return pool, mapError(err)
}

func scanPoolItem(row pgx.Row) (domain.DailyLearningPoolItem, error) {
	var item domain.DailyLearningPoolItem
	var metadata []byte
	var word domain.Word
	var wordMetadata []byte
	err := row.Scan(
		&item.ID,
		&item.PoolID,
		&item.UserID,
		&item.WordID,
		&item.Ordinal,
		&item.ItemType,
		&item.ReviewMode,
		&item.DueAt,
		&item.Status,
		&item.IsReview,
		&item.FirstExposureRequired,
		&item.RevealedMeaning,
		&item.RevealedExample,
		&metadata,
		&item.CompletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&word.ID,
		&word.Word,
		&word.NormalizedForm,
		&word.CanonicalForm,
		&word.Lemma,
		&word.WordFamily,
		&word.ConfusableGroupKey,
		&word.PartOfSpeech,
		&word.Level,
		&word.Topic,
		&word.IPA,
		&word.PronunciationHint,
		&word.VietnameseMeaning,
		&word.EnglishMeaning,
		&word.ExampleSentence1,
		&word.ExampleSentence2,
		&word.SourceProvider,
		&wordMetadata,
		&word.CreatedAt,
		&word.UpdatedAt,
	)
	item.Metadata = toJSONMap(metadata)
	if rawBonus, ok := item.Metadata["bonus_practice"].(bool); ok {
		item.BonusPractice = rawBonus
	}
	word.SourceMetadata = toJSONMap(wordMetadata)
	if word.ID != uuid.Nil {
		item.Word = &word
	}
	return item, mapError(err)
}

func scanEvent(row pgx.Row) (domain.LearningEvent, error) {
	var event domain.LearningEvent
	var payload []byte
	var modeUsed string
	err := row.Scan(
		&event.ID,
		&event.UserID,
		&event.WordID,
		&event.PoolItemID,
		&event.EventType,
		&event.EventTime,
		&payload,
		&event.ResponseTimeMs,
		&modeUsed,
		&event.ClientEventID,
		&event.CreatedAt,
	)
	event.ModeUsed = domain.ReviewMode(modeUsed)
	event.Payload = toJSONMap(payload)
	return event, mapError(err)
}

func scanRun(row pgx.Row) (domain.LLMGenerationRun, error) {
	var run domain.LLMGenerationRun
	var localDate time.Time
	var rawResponse []byte
	var rejectionSummary []byte
	err := row.Scan(
		&run.ID,
		&run.UserID,
		&run.PoolID,
		&localDate,
		&run.Topic,
		&run.RequestedCount,
		&run.AcceptedCount,
		&run.Attempt,
		&run.Status,
		&run.Provider,
		&run.Model,
		&run.Prompt,
		&rawResponse,
		&rejectionSummary,
		&run.ErrorMessage,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	run.LocalDate = localDate.Format("2006-01-02")
	run.RawResponse = toJSONMap(rawResponse)
	run.RejectionSummary = toJSONMap(rejectionSummary)
	return run, mapError(err)
}

func scanDictionaryEntry(row pgx.Row) (domain.DictionaryEntry, error) {
	var entry domain.DictionaryEntry
	var metadata []byte
	var listStatus string
	err := row.Scan(
		&entry.Word.ID,
		&entry.Word.Word,
		&entry.Word.NormalizedForm,
		&entry.Word.CanonicalForm,
		&entry.Word.Lemma,
		&entry.Word.WordFamily,
		&entry.Word.ConfusableGroupKey,
		&entry.Word.PartOfSpeech,
		&entry.Word.Level,
		&entry.Word.Topic,
		&entry.Word.IPA,
		&entry.Word.PronunciationHint,
		&entry.Word.VietnameseMeaning,
		&entry.Word.EnglishMeaning,
		&entry.Word.ExampleSentence1,
		&entry.Word.ExampleSentence2,
		&entry.Word.SourceProvider,
		&metadata,
		&entry.Word.CreatedAt,
		&entry.Word.UpdatedAt,
		&listStatus,
		&entry.LearningStatus,
		&entry.FirstSeenAt,
		&entry.LastSeenAt,
		&entry.KnownAt,
		&entry.NextReviewAt,
		&entry.ReviewCount,
		&entry.WeaknessScore,
		&entry.UpdatedAt,
	)
	entry.Word.SourceMetadata = toJSONMap(metadata)
	entry.ListStatus = domain.DictionaryListStatus(listStatus)
	return entry, mapError(err)
}

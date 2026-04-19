package postgres

import (
	"context"
	"database/sql"
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
	Users                *UserRepository
	Settings             *SettingsRepository
	Words                *WordRepository
	States               *WordStateRepository
	Pools                *PoolRepository
	Events               *LearningEventRepository
	LLMRuns              *LLMRunRepository
	ExercisePacks        *ExercisePackRepository
	Mode4Reviews         *Mode4ReviewRepository
	DynamicReviewPrompts *DynamicReviewPromptRepository
}

func NewRepositories(pool *pgxpool.Pool) *Repositories {
	return &Repositories{
		Users:                &UserRepository{pool: pool},
		Settings:             &SettingsRepository{pool: pool},
		Words:                &WordRepository{pool: pool},
		States:               &WordStateRepository{pool: pool},
		Pools:                &PoolRepository{pool: pool},
		Events:               &LearningEventRepository{pool: pool},
		LLMRuns:              &LLMRunRepository{pool: pool},
		ExercisePacks:        &ExercisePackRepository{pool: pool},
		Mode4Reviews:         &Mode4ReviewRepository{pool: pool},
		DynamicReviewPrompts: &DynamicReviewPromptRepository{pool: pool},
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

func marshalJSONValue(value any, fallback string) []byte {
	bytes, err := json.Marshal(value)
	if err != nil {
		return []byte(fallback)
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

func nullableCommonRateValue(value *domain.WordCommonRate) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullableCommonRatePointer(value sql.NullString) *domain.WordCommonRate {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	rate := domain.WordCommonRate(value.String)
	return &rate
}

func nullableMemoryCauseValue(value domain.MemoryCause) any {
	if strings.TrimSpace(string(value)) == "" {
		return nil
	}
	return string(value)
}

func nullableMemoryCausePointer(value sql.NullString) domain.MemoryCause {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return ""
	}
	return domain.MemoryCause(value.String)
}

func nullableBoolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolPointer(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	copied := value.Bool
	return &copied
}

func extractAudioURL(metadata domain.JSONMap) string {
	raw, ok := metadata["audio_url"]
	if !ok {
		raw = metadata["audioUrl"]
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
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
	var commonRate sql.NullString
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
		&commonRate,
		&word.SourceProvider,
		&metadata,
		&word.CreatedAt,
		&word.UpdatedAt,
	)
	word.CommonRate = nullableCommonRatePointer(commonRate)
	word.SourceMetadata = toJSONMap(metadata)
	word.AudioURL = extractAudioURL(word.SourceMetadata)
	return word, mapError(err)
}

func scanState(row pgx.Row) (domain.UserWordState, error) {
	var state domain.UserWordState
	var lastRating string
	var lastMode string
	var lastMemoryCause sql.NullString
	var lastAnswerCorrect sql.NullBool
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
		&lastMemoryCause,
		&state.LastResponseTimeMs,
		&lastAnswerCorrect,
		&state.MeaningForgetCount,
		&state.SpellingIssueCount,
		&state.ConfusableMixupCount,
		&state.SlowRecallCount,
		&state.GuessedCorrectCount,
		&state.KnownAt,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	state.LastRating = domain.ReviewRating(lastRating)
	state.LastMode = domain.ReviewMode(lastMode)
	state.LastMemoryCause = nullableMemoryCausePointer(lastMemoryCause)
	state.LastAnswerCorrect = nullableBoolPointer(lastAnswerCorrect)
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
	var commonRate sql.NullString
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
		&commonRate,
		&word.SourceProvider,
		&wordMetadata,
		&word.CreatedAt,
		&word.UpdatedAt,
	)
	item.Metadata = toJSONMap(metadata)
	if rawBonus, ok := item.Metadata["bonus_practice"].(bool); ok {
		item.BonusPractice = rawBonus
	}
	word.CommonRate = nullableCommonRatePointer(commonRate)
	word.SourceMetadata = toJSONMap(wordMetadata)
	if word.ID != uuid.Nil {
		word.AudioURL = extractAudioURL(word.SourceMetadata)
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
		&event.ClientSessionID,
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

func scanContextExercisePack(row pgx.Row) (domain.ContextExercisePack, error) {
	var pack domain.ContextExercisePack
	var localDate time.Time
	var userID uuid.NullUUID
	var llmRunID uuid.NullUUID
	var sourceWordsJSON []byte
	var payloadJSON []byte
	err := row.Scan(
		&pack.ID,
		&userID,
		&localDate,
		&pack.Topic,
		&pack.CEFRLevel,
		&pack.PackType,
		&pack.ClusterHash,
		&sourceWordsJSON,
		&payloadJSON,
		&pack.Status,
		&llmRunID,
		&pack.CreatedAt,
		&pack.UpdatedAt,
	)
	if userID.Valid {
		pack.UserID = &userID.UUID
	}
	if llmRunID.Valid {
		pack.LLMRunID = &llmRunID.UUID
	}
	pack.LocalDate = localDate.Format("2006-01-02")
	if len(sourceWordsJSON) > 0 {
		if err := json.Unmarshal(sourceWordsJSON, &pack.SourceWords); err != nil {
			return domain.ContextExercisePack{}, mapError(err)
		}
	}
	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &pack.Payload); err != nil {
			return domain.ContextExercisePack{}, mapError(err)
		}
	}
	return pack, mapError(err)
}

func scanMode4ReviewPassage(row pgx.Row) (domain.Mode4ReviewPassage, error) {
	var passage domain.Mode4ReviewPassage
	var wordIDsJSON []byte
	var sourceWordsJSON []byte
	var spansJSON []byte
	var llmRunID uuid.NullUUID
	err := row.Scan(
		&passage.ID,
		&passage.UserID,
		&passage.GenerationNumber,
		&wordIDsJSON,
		&sourceWordsJSON,
		&passage.PlainPassageText,
		&passage.MarkedPassageMarkdown,
		&spansJSON,
		&passage.Status,
		&passage.SkipCount,
		&passage.LastSkippedAt,
		&passage.CompletedAt,
		&llmRunID,
		&passage.CreatedAt,
		&passage.UpdatedAt,
	)
	if llmRunID.Valid {
		passage.LLMRunID = &llmRunID.UUID
	}
	if len(wordIDsJSON) > 0 {
		if err := json.Unmarshal(wordIDsJSON, &passage.WordIDs); err != nil {
			return domain.Mode4ReviewPassage{}, mapError(err)
		}
	}
	if len(sourceWordsJSON) > 0 {
		if err := json.Unmarshal(sourceWordsJSON, &passage.SourceWords); err != nil {
			return domain.Mode4ReviewPassage{}, mapError(err)
		}
	}
	if len(spansJSON) > 0 {
		if err := json.Unmarshal(spansJSON, &passage.PassageSpans); err != nil {
			return domain.Mode4ReviewPassage{}, mapError(err)
		}
	}
	return passage, mapError(err)
}

func scanMode4ReviewState(row pgx.Row) (domain.Mode4ReviewState, error) {
	var state domain.Mode4ReviewState
	var activePassageID uuid.NullUUID
	err := row.Scan(
		&state.UserID,
		&state.GenerationCount,
		&activePassageID,
		&state.LastCompletedAt,
		&state.NextEligibleAt,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	if activePassageID.Valid {
		state.ActivePassageID = &activePassageID.UUID
	}
	return state, mapError(err)
}

func scanDynamicReviewPrompt(row pgx.Row) (domain.DailyDynamicReviewPrompt, error) {
	var prompt domain.DailyDynamicReviewPrompt
	var localDate time.Time
	var payloadJSON []byte
	var llmRunID uuid.NullUUID
	err := row.Scan(
		&prompt.ID,
		&prompt.UserID,
		&localDate,
		&prompt.WordID,
		&prompt.ReviewMode,
		&payloadJSON,
		&llmRunID,
		&prompt.CreatedAt,
		&prompt.UpdatedAt,
	)
	if llmRunID.Valid {
		prompt.LLMRunID = &llmRunID.UUID
	}
	prompt.LocalDate = localDate.Format("2006-01-02")
	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &prompt.Payload); err != nil {
			return domain.DailyDynamicReviewPrompt{}, mapError(err)
		}
	}
	return prompt, mapError(err)
}

func scanDictionaryEntry(row pgx.Row) (domain.DictionaryEntry, error) {
	var entry domain.DictionaryEntry
	var commonRate sql.NullString
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
		&commonRate,
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
	entry.Word.CommonRate = nullableCommonRatePointer(commonRate)
	entry.Word.SourceMetadata = toJSONMap(metadata)
	entry.Word.AudioURL = extractAudioURL(entry.Word.SourceMetadata)
	entry.ListStatus = domain.DictionaryListStatus(listStatus)
	return entry, mapError(err)
}

package domain

import (
	"time"

	"github.com/google/uuid"
)

type CEFRLevel string

const (
	CEFRB1 CEFRLevel = "B1"
	CEFRB2 CEFRLevel = "B2"
	CEFRC1 CEFRLevel = "C1"
	CEFRC2 CEFRLevel = "C2"
)

type MeaningLanguage string

const (
	MeaningLanguageVietnamese MeaningLanguage = "vi"
	MeaningLanguageEnglish    MeaningLanguage = "en"
)

type WordStatus string

const (
	WordStatusKnown    WordStatus = "known"
	WordStatusLearning WordStatus = "learning"
	WordStatusReview   WordStatus = "review"
)

type DictionaryFilter string

const (
	DictionaryFilterUnknown DictionaryFilter = "unknown"
	DictionaryFilterKnown   DictionaryFilter = "known"
	DictionaryFilterAll     DictionaryFilter = "all"
)

type DictionaryListStatus string

const (
	DictionaryListStatusUnknown DictionaryListStatus = "unknown"
	DictionaryListStatusKnown   DictionaryListStatus = "known"
)

type ReviewRating string

const (
	RatingEasy   ReviewRating = "easy"
	RatingMedium ReviewRating = "medium"
	RatingHard   ReviewRating = "hard"
)

type ReviewMode string

const (
	ReviewModeReveal         ReviewMode = "hidden_meaning"
	ReviewModeMultipleChoice ReviewMode = "multiple_choice"
	ReviewModeFillBlank      ReviewMode = "fill_in_blank"
)

type ReviewInputMethod string

const (
	ReviewInputMethodTap        ReviewInputMethod = "tap"
	ReviewInputMethodTyping     ReviewInputMethod = "typing"
	ReviewInputMethodRevealOnly ReviewInputMethod = "reveal_only"
)

type MemoryCause string

const (
	MemoryCauseForgotMeaning  MemoryCause = "forgot_meaning"
	MemoryCauseSpellingIssue  MemoryCause = "spelling_issue"
	MemoryCauseMixedUpWord    MemoryCause = "mixed_up_word"
	MemoryCauseSlowRecall     MemoryCause = "slow_recall"
	MemoryCauseGuessedCorrect MemoryCause = "guessed_correct"
)

type PoolItemType string

const (
	PoolItemTypeReview    PoolItemType = "review"
	PoolItemTypeShortTerm PoolItemType = "short_term"
	PoolItemTypeWeak      PoolItemType = "weak"
	PoolItemTypeNew       PoolItemType = "new"
)

type PoolItemStatus string

const (
	PoolItemStatusPending   PoolItemStatus = "pending"
	PoolItemStatusCompleted PoolItemStatus = "completed"
)

type ExposureAction string

const (
	ExposureActionDontLearn ExposureAction = "dont_learn"
	ExposureActionKnown     ExposureAction = "known"
	ExposureActionUnknown   ExposureAction = "unknown"
)

type EventType string

const (
	EventTypeFirstExposure   EventType = "first_exposure"
	EventTypeReviewAnswer    EventType = "review_answer"
	EventTypeRevealMeaning   EventType = "reveal_meaning"
	EventTypeRevealExample   EventType = "reveal_example"
	EventTypeHintUsage       EventType = "hint_usage"
	EventTypePronunciation   EventType = "play_pronunciation"
	EventTypePoolGenerated   EventType = "pool_generated"
	EventTypeWeaknessRefresh EventType = "weakness_refresh"
	EventTypeBonusPractice   EventType = "bonus_practice_review"
	EventTypeAnswerUndo      EventType = "answer_undo"
)

type RevealKind string

const (
	RevealKindMeaning RevealKind = "meaning"
	RevealKindExample RevealKind = "example"
	RevealKindHint    RevealKind = "hint"
)

type LLMRunStatus string

const (
	LLMRunStatusSuccess LLMRunStatus = "success"
	LLMRunStatusFailed  LLMRunStatus = "failed"
	LLMRunStatusPartial LLMRunStatus = "partial"
)

type ExerciseSessionState string

const (
	ExerciseSessionStateReady             ExerciseSessionState = "ready"
	ExerciseSessionStateInsufficientWords ExerciseSessionState = "insufficient_words"
	ExerciseSessionStateUnavailable       ExerciseSessionState = "unavailable"
)

type ExercisePackType string

const (
	ExercisePackTypeContextClusterChallenge ExercisePackType = "context_cluster_challenge"
)

type ExercisePackStatus string

const (
	ExercisePackStatusReady ExercisePackStatus = "ready"
)

type ExerciseQuestionType string

const (
	ExerciseQuestionTypeBestFit              ExerciseQuestionType = "best_fit"
	ExerciseQuestionTypeMeaningMatch         ExerciseQuestionType = "meaning_match"
	ExerciseQuestionTypeDefinitionMatch      ExerciseQuestionType = "definition_match"
	ExerciseQuestionTypeSentenceUsage        ExerciseQuestionType = "sentence_usage"
	ExerciseQuestionTypePassageUnderstanding ExerciseQuestionType = "passage_understanding"
	ExerciseQuestionTypeConfusableChoice     ExerciseQuestionType = "confusable_choice"
)

type User struct {
	ID              uuid.UUID `json:"id"`
	ExternalSubject string    `json:"external_subject"`
	Email           string    `json:"email,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastActiveAt    time.Time `json:"last_active_at"`
}

type UserSettings struct {
	UserID                   uuid.UUID       `json:"user_id"`
	CEFRLevel                CEFRLevel       `json:"cefr_level"`
	DailyNewWordLimit        int             `json:"daily_new_word_limit"`
	PreferredMeaningLanguage MeaningLanguage `json:"preferred_meaning_language"`
	Timezone                 string          `json:"timezone"`
	PronunciationEnabled     bool            `json:"pronunciation_enabled"`
	LockScreenEnabled        bool            `json:"lock_screen_enabled"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

type Word struct {
	ID                 uuid.UUID       `json:"id"`
	Word               string          `json:"word"`
	NormalizedForm     string          `json:"normalized_form"`
	CanonicalForm      string          `json:"canonical_form"`
	Lemma              string          `json:"lemma"`
	WordFamily         string          `json:"word_family,omitempty"`
	ConfusableGroupKey string          `json:"confusable_group_key,omitempty"`
	PartOfSpeech       string          `json:"part_of_speech,omitempty"`
	Level              CEFRLevel       `json:"level"`
	Topic              string          `json:"topic"`
	IPA                string          `json:"ipa,omitempty"`
	PronunciationHint  string          `json:"pronunciation_hint,omitempty"`
	VietnameseMeaning  string          `json:"vietnamese_meaning"`
	EnglishMeaning     string          `json:"english_meaning"`
	ExampleSentence1   string          `json:"example_sentence_1,omitempty"`
	ExampleSentence2   string          `json:"example_sentence_2,omitempty"`
	CommonRate         *WordCommonRate `json:"common_rate,omitempty"`
	SourceProvider     string          `json:"source_provider,omitempty"`
	SourceMetadata     JSONMap         `json:"source_metadata,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type UserWordState struct {
	UserID               uuid.UUID    `json:"user_id"`
	WordID               uuid.UUID    `json:"word_id"`
	Status               WordStatus   `json:"status"`
	FirstSeenAt          *time.Time   `json:"first_seen_at,omitempty"`
	LastSeenAt           *time.Time   `json:"last_seen_at,omitempty"`
	LastRating           ReviewRating `json:"last_rating,omitempty"`
	NextReviewAt         *time.Time   `json:"next_review_at,omitempty"`
	IntervalSeconds      int          `json:"interval_seconds"`
	Stability            float64      `json:"stability"`
	Difficulty           float64      `json:"difficulty"`
	ReviewCount          int          `json:"review_count"`
	WrongCount           int          `json:"wrong_count"`
	EasyCount            int          `json:"easy_count"`
	MediumCount          int          `json:"medium_count"`
	HardCount            int          `json:"hard_count"`
	HintUsedCount        int          `json:"hint_used_count"`
	RevealMeaningCount   int          `json:"reveal_meaning_count"`
	RevealExampleCount   int          `json:"reveal_example_count"`
	AvgResponseTimeMs    int64        `json:"avg_response_time_ms"`
	WeaknessScore        float64      `json:"weakness_score"`
	LearningStage        int          `json:"learning_stage"`
	LastMode             ReviewMode   `json:"last_mode,omitempty"`
	LastMemoryCause      MemoryCause  `json:"last_memory_cause,omitempty"`
	LastResponseTimeMs   int          `json:"last_response_time_ms"`
	LastAnswerCorrect    *bool        `json:"last_answer_correct,omitempty"`
	MeaningForgetCount   int          `json:"meaning_forget_count"`
	SpellingIssueCount   int          `json:"spelling_issue_count"`
	ConfusableMixupCount int          `json:"confusable_mixup_count"`
	SlowRecallCount      int          `json:"slow_recall_count"`
	GuessedCorrectCount  int          `json:"guessed_correct_count"`
	KnownAt              *time.Time   `json:"known_at,omitempty"`
	CreatedAt            time.Time    `json:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at"`
}

type DailyLearningPool struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	LocalDate      string    `json:"local_date"`
	Timezone       string    `json:"timezone"`
	Topic          string    `json:"topic"`
	DueReviewCount int       `json:"due_review_count"`
	ShortTermCount int       `json:"short_term_count"`
	WeakCount      int       `json:"weak_count"`
	NewCount       int       `json:"new_count"`
	GeneratedAt    time.Time `json:"generated_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DailyLearningPoolItem struct {
	ID                    uuid.UUID      `json:"id"`
	PoolID                uuid.UUID      `json:"pool_id"`
	UserID                uuid.UUID      `json:"user_id"`
	WordID                uuid.UUID      `json:"word_id"`
	Ordinal               int            `json:"ordinal"`
	ItemType              PoolItemType   `json:"item_type"`
	ReviewMode            ReviewMode     `json:"review_mode"`
	DueAt                 *time.Time     `json:"due_at,omitempty"`
	Status                PoolItemStatus `json:"status"`
	IsReview              bool           `json:"is_review"`
	FirstExposureRequired bool           `json:"first_exposure_required"`
	BonusPractice         bool           `json:"bonus_practice"`
	RevealedMeaning       bool           `json:"revealed_meaning"`
	RevealedExample       bool           `json:"revealed_example"`
	Metadata              JSONMap        `json:"metadata,omitempty"`
	CompletedAt           *time.Time     `json:"completed_at,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	Word                  *Word          `json:"word,omitempty"`
}

type LearningEvent struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	WordID         uuid.UUID  `json:"word_id"`
	PoolItemID     *uuid.UUID `json:"pool_item_id,omitempty"`
	EventType      EventType  `json:"event_type"`
	EventTime      time.Time  `json:"event_time"`
	Payload        JSONMap    `json:"payload,omitempty"`
	ResponseTimeMs int        `json:"response_time_ms"`
	ModeUsed       ReviewMode `json:"mode_used,omitempty"`
	ClientEventID  string     `json:"client_event_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type LLMGenerationRun struct {
	ID               uuid.UUID    `json:"id"`
	UserID           uuid.UUID    `json:"user_id"`
	PoolID           *uuid.UUID   `json:"pool_id,omitempty"`
	LocalDate        string       `json:"local_date"`
	Topic            string       `json:"topic"`
	RequestedCount   int          `json:"requested_count"`
	AcceptedCount    int          `json:"accepted_count"`
	Attempt          int          `json:"attempt"`
	Status           LLMRunStatus `json:"status"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	Prompt           string       `json:"prompt"`
	RawResponse      JSONMap      `json:"raw_response,omitempty"`
	RejectionSummary JSONMap      `json:"rejection_summary,omitempty"`
	ErrorMessage     string       `json:"error_message,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

type CandidateWord struct {
	Word               string          `json:"word"`
	CanonicalForm      string          `json:"canonical_form"`
	Lemma              string          `json:"lemma"`
	WordFamily         string          `json:"word_family,omitempty"`
	ConfusableGroupKey string          `json:"confusable_group_key,omitempty"`
	PartOfSpeech       string          `json:"part_of_speech,omitempty"`
	Level              CEFRLevel       `json:"level"`
	Topic              string          `json:"topic"`
	IPA                string          `json:"ipa,omitempty"`
	PronunciationHint  string          `json:"pronunciation_hint,omitempty"`
	VietnameseMeaning  string          `json:"vietnamese_meaning"`
	EnglishMeaning     string          `json:"english_meaning"`
	ExampleSentence1   string          `json:"example_sentence_1,omitempty"`
	ExampleSentence2   string          `json:"example_sentence_2,omitempty"`
	CommonRate         *WordCommonRate `json:"common_rate,omitempty"`
	SourceProvider     string          `json:"source_provider,omitempty"`
	SourceMetadata     JSONMap         `json:"source_metadata,omitempty"`
	NormalizedForm     string          `json:"normalized_form"`
	RankingScore       float64         `json:"ranking_score"`
	ValidationIssues   []string        `json:"validation_issues,omitempty"`
}

type ContextExerciseQuestion struct {
	ID          string               `json:"id"`
	Type        ExerciseQuestionType `json:"type"`
	TargetWord  string               `json:"target_word"`
	Prompt      string               `json:"prompt"`
	Options     []string             `json:"options"`
	Answer      string               `json:"answer"`
	Explanation string               `json:"explanation"`
}

type ContextExercisePayload struct {
	PackID       string                    `json:"pack_id"`
	Topic        string                    `json:"topic"`
	CEFRLevel    CEFRLevel                 `json:"cefr_level"`
	PackType     ExercisePackType          `json:"pack_type"`
	ClusterWords []string                  `json:"cluster_words"`
	Title        string                    `json:"title"`
	Passage      string                    `json:"passage"`
	Questions    []ContextExerciseQuestion `json:"questions"`
	SummaryTip   string                    `json:"summary_tip"`
}

type ContextExerciseSourceWord struct {
	WordID         uuid.UUID `json:"word_id"`
	Word           string    `json:"word"`
	NormalizedForm string    `json:"normalized_form"`
	Topic          string    `json:"topic"`
	Level          CEFRLevel `json:"level"`
	WeaknessScore  float64   `json:"weakness_score"`
}

type ContextExercisePack struct {
	ID          uuid.UUID                   `json:"id"`
	UserID      *uuid.UUID                  `json:"user_id,omitempty"`
	LocalDate   string                      `json:"local_date"`
	Topic       string                      `json:"topic"`
	CEFRLevel   CEFRLevel                   `json:"cefr_level"`
	PackType    ExercisePackType            `json:"pack_type"`
	ClusterHash string                      `json:"cluster_hash"`
	SourceWords []ContextExerciseSourceWord `json:"source_words"`
	Payload     ContextExercisePayload      `json:"payload"`
	Status      ExercisePackStatus          `json:"status"`
	LLMRunID    *uuid.UUID                  `json:"llm_run_id,omitempty"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type ExerciseSession struct {
	SessionID    string           `json:"session_id"`
	LocalDate    string           `json:"local_date"`
	GeneratedNow bool             `json:"generated_now"`
	Reused       bool             `json:"reused"`
	ClusterHash  string           `json:"cluster_hash"`
	ClusterWords []string         `json:"cluster_words"`
	PackType     ExercisePackType `json:"pack_type"`
	Topic        string           `json:"topic"`
	CEFRLevel    CEFRLevel        `json:"cefr_level"`
}

type ExerciseSessionResponse struct {
	State   ExerciseSessionState    `json:"state"`
	Message string                  `json:"message"`
	Session *ExerciseSession        `json:"session,omitempty"`
	Pack    *ContextExercisePayload `json:"pack,omitempty"`
}

type PoolGenerationCounts struct {
	DueReview int `json:"due_review"`
	ShortTerm int `json:"short_term"`
	Weak      int `json:"weak"`
	New       int `json:"new"`
}

type DictionaryEntry struct {
	Word           Word                 `json:"word"`
	ListStatus     DictionaryListStatus `json:"list_status"`
	LearningStatus WordStatus           `json:"learning_status"`
	FirstSeenAt    *time.Time           `json:"first_seen_at,omitempty"`
	LastSeenAt     *time.Time           `json:"last_seen_at,omitempty"`
	KnownAt        *time.Time           `json:"known_at,omitempty"`
	NextReviewAt   *time.Time           `json:"next_review_at,omitempty"`
	ReviewCount    int                  `json:"review_count"`
	WeaknessScore  float64              `json:"weakness_score"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

type DictionaryListResponse struct {
	Filter DictionaryFilter  `json:"filter"`
	Items  []DictionaryEntry `json:"items"`
}

type JSONMap map[string]any

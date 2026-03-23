package service

import (
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type GenerationInput struct {
	UserID            uuid.UUID
	CEFRLevel         domain.CEFRLevel
	Topic             string
	RequestedCount    int
	PreferredLanguage domain.MeaningLanguage
	ExcludeWords      []string
	ExcludeLemmas     []string
	ExcludeGroupKeys  []string
}

type ExercisePackGenerationInput struct {
	UserID       uuid.UUID
	LocalDate    string
	Topic        string
	CEFRLevel    domain.CEFRLevel
	ClusterWords []domain.Word
}

type DynamicReviewPromptRequestItem struct {
	WordID     uuid.UUID
	ReviewMode domain.ReviewMode
	Word       domain.Word
}

type DynamicReviewPromptGenerationInput struct {
	UserID    uuid.UUID
	LocalDate string
	Items     []DynamicReviewPromptRequestItem
}

type DynamicReviewGenerationResult struct {
	LocalDate      string `json:"local_date"`
	EligibleCount  int    `json:"eligible_count"`
	GeneratedCount int    `json:"generated_count"`
	Message        string `json:"message,omitempty"`
}

type DailyPoolView struct {
	Pool        domain.DailyLearningPool       `json:"pool"`
	Items       []domain.DailyLearningPoolItem `json:"items"`
	Counts      domain.PoolGenerationCounts    `json:"counts"`
	AppendedNew int                            `json:"appended_new,omitempty"`
}

type CardResponse struct {
	LocalDate string                        `json:"local_date"`
	NextDueAt *time.Time                    `json:"next_due_at,omitempty"`
	PoolItem  *domain.DailyLearningPoolItem `json:"pool_item,omitempty"`
}

type FirstExposureRequest struct {
	PoolItemID     uuid.UUID
	Action         domain.ExposureAction
	ResponseTimeMs int
	ClientEventID  string
}

type ReviewRequest struct {
	PoolItemID                       uuid.UUID
	Rating                           domain.ReviewRating
	ModeUsed                         domain.ReviewMode
	ResponseTimeMs                   int
	ClientEventID                    string
	AnswerCorrect                    *bool
	RevealedMeaningBeforeAnswer      bool
	RevealedExampleBeforeAnswer      bool
	UsedHint                         bool
	InputMethod                      domain.ReviewInputMethod
	NormalizedTypedAnswer            string
	SelectedChoiceWordID             *uuid.UUID
	SelectedChoiceConfusableGroupKey string
}

type RevealRequest struct {
	PoolItemID     uuid.UUID
	Kind           domain.RevealKind
	ModeUsed       domain.ReviewMode
	ResponseTimeMs int
	ClientEventID  string
}

type PronunciationRequest struct {
	PoolItemID    uuid.UUID
	ClientEventID string
}

type UndoLastAnswerRequest struct {
	PoolItemID uuid.UUID
}

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type LearningService struct {
	settingsRepo                SettingsRepository
	stateRepo                   WordStateRepository
	poolRepo                    PoolRepository
	eventRepo                   LearningEventRepository
	quotaManager                UnknownDailyQuotaManager
	clock                       Clock
	logger                      *slog.Logger
	memoryCauseInferenceEnabled bool
}

func NewLearningService(
	settingsRepo SettingsRepository,
	stateRepo WordStateRepository,
	poolRepo PoolRepository,
	eventRepo LearningEventRepository,
	quotaManager UnknownDailyQuotaManager,
	clock Clock,
	logger *slog.Logger,
	memoryCauseInferenceEnabled bool,
) *LearningService {
	return &LearningService{
		settingsRepo:                settingsRepo,
		stateRepo:                   stateRepo,
		poolRepo:                    poolRepo,
		eventRepo:                   eventRepo,
		quotaManager:                quotaManager,
		clock:                       clock,
		logger:                      logger,
		memoryCauseInferenceEnabled: memoryCauseInferenceEnabled,
	}
}

func (s *LearningService) SubmitFirstExposure(ctx context.Context, user domain.User, req FirstExposureRequest) error {
	item, err := s.poolRepo.GetPoolItem(ctx, user.ID, req.PoolItemID)
	if err != nil {
		return err
	}
	if item.Status != domain.PoolItemStatusPending || !item.FirstExposureRequired {
		return fmt.Errorf("%w: pool item is not awaiting first exposure", domain.ErrValidation)
	}

	now := s.clock.Now()
	previousState, hadPreviousState, err := s.loadExistingStateSnapshot(ctx, user.ID, item.WordID)
	if err != nil {
		return err
	}
	createdItemIDs := []uuid.UUID{}
	payload := domain.JSONMap{
		"action": req.Action,
	}

	switch req.Action {
	case domain.ExposureActionDontLearn:
		if err := s.markFirstExposureDontLearn(ctx, user, item, hadPreviousState, now, &createdItemIDs); err != nil {
			return err
		}
	case domain.ExposureActionKnown:
		state := s.initStateFromSnapshot(user.ID, item.WordID, previousState, hadPreviousState, now)
		state = ApplyFirstExposureKnown(state, now, req.ResponseTimeMs)
		if _, err := s.stateRepo.Upsert(ctx, state); err != nil {
			return err
		}
		if err := s.poolRepo.MarkPoolItemCompleted(ctx, item.ID, now); err != nil {
			return err
		}
		if s.quotaManager != nil {
			if createdItemIDs, err = s.quotaManager.EnsureUnknownDailyQuota(ctx, user, &item.ID); err != nil {
				s.logger.Warn("ensure unknown daily quota", "error", err)
				createdItemIDs = nil
			}
		}
	case domain.ExposureActionUnknown:
		state := s.initStateFromSnapshot(user.ID, item.WordID, previousState, hadPreviousState, now)
		state = ApplyFirstExposureUnknown(state, now, req.ResponseTimeMs)
		if _, err := s.stateRepo.Upsert(ctx, state); err != nil {
			return err
		}
		if err := s.poolRepo.MarkPoolItemCompleted(ctx, item.ID, now); err != nil {
			return err
		}
		if followUpID, err := s.maybeAppendSameDayFollowUp(ctx, user.ID, item, state); err != nil {
			s.logger.Warn("append same-day follow-up", "error", err)
		} else if followUpID != uuid.Nil {
			createdItemIDs = append(createdItemIDs, followUpID)
		}
	default:
		return fmt.Errorf("%w: unsupported action", domain.ErrValidation)
	}

	appendUndoSnapshotPayload(payload, previousState, hadPreviousState, createdItemIDs)
	if err := s.recordEvent(ctx, domain.LearningEvent{
		UserID:         user.ID,
		WordID:         item.WordID,
		PoolItemID:     &item.ID,
		EventType:      domain.EventTypeFirstExposure,
		EventTime:      now,
		ResponseTimeMs: req.ResponseTimeMs,
		ClientEventID:  req.ClientEventID,
		Payload:        payload,
	}); err != nil {
		return err
	}
	return nil
}

func (s *LearningService) markFirstExposureDontLearn(
	ctx context.Context,
	user domain.User,
	item domain.DailyLearningPoolItem,
	hadPreviousState bool,
	now time.Time,
	createdItemIDs *[]uuid.UUID,
) error {
	if err := s.poolRepo.MarkPoolItemCompleted(ctx, item.ID, now); err != nil {
		return err
	}
	if hadPreviousState {
		if err := s.stateRepo.Delete(ctx, user.ID, item.WordID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if s.quotaManager != nil {
		appendedIDs, err := s.quotaManager.EnsureUnknownDailyQuota(ctx, user, &item.ID)
		if err != nil {
			s.logger.Warn("ensure unknown daily quota after discard", "error", err)
			return nil
		}
		*createdItemIDs = append(*createdItemIDs, appendedIDs...)
	}
	return nil
}

func (s *LearningService) SubmitReview(ctx context.Context, user domain.User, req ReviewRequest) error {
	item, err := s.poolRepo.GetPoolItem(ctx, user.ID, req.PoolItemID)
	if err != nil {
		return err
	}
	if item.Status != domain.PoolItemStatusPending {
		return fmt.Errorf("%w: pool item already completed", domain.ErrValidation)
	}

	state, err := s.stateRepo.Get(ctx, user.ID, item.WordID)
	if err != nil {
		return err
	}
	previousState := state
	now := s.clock.Now()
	eventType := domain.EventTypeReviewAnswer
	payload := domain.JSONMap{
		"rating": req.Rating,
	}
	answerCorrect := req.Rating != domain.RatingHard
	if req.AnswerCorrect != nil {
		answerCorrect = *req.AnswerCorrect
	}
	inferredCause := domain.MemoryCause("")
	if s.memoryCauseInferenceEnabled && item.Word != nil {
		inferredCause = InferMemoryCause(MemoryCauseInput{
			State:                            previousState,
			TargetWord:                       *item.Word,
			ModeUsed:                         req.ModeUsed,
			AnswerCorrect:                    answerCorrect,
			RevealedMeaningBeforeAnswer:      req.RevealedMeaningBeforeAnswer,
			RevealedExampleBeforeAnswer:      req.RevealedExampleBeforeAnswer,
			UsedHint:                         req.UsedHint,
			ResponseTimeMs:                   req.ResponseTimeMs,
			InputMethod:                      req.InputMethod,
			NormalizedTypedAnswer:            NormalizeTypedAnswerForStorage(req.NormalizedTypedAnswer),
			SelectedChoiceConfusableGroupKey: req.SelectedChoiceConfusableGroupKey,
		})
	}
	createdItemIDs := []uuid.UUID{}
	if item.BonusPractice {
		state = ApplyBonusPracticeOutcome(state, req.Rating, req.ModeUsed, now, req.ResponseTimeMs)
		payload["bonus_practice"] = true
		eventType = domain.EventTypeBonusPractice
	} else {
		state = ApplyReviewOutcome(state, req.Rating, req.ModeUsed, now, req.ResponseTimeMs)
		if s.memoryCauseInferenceEnabled {
			state = ApplyMemoryCauseIntervalBias(state, inferredCause, now)
		}
	}
	if s.memoryCauseInferenceEnabled {
		state = ApplyMemoryCause(state, inferredCause, req.ResponseTimeMs, answerCorrect)
	}
	payload["answer_correct"] = answerCorrect
	payload["revealed_meaning_before_answer"] = req.RevealedMeaningBeforeAnswer
	payload["revealed_example_before_answer"] = req.RevealedExampleBeforeAnswer
	payload["used_hint"] = req.UsedHint
	if req.InputMethod != "" {
		payload["input_method"] = req.InputMethod
	}
	if normalizedTypedAnswer := NormalizeTypedAnswerForStorage(req.NormalizedTypedAnswer); normalizedTypedAnswer != "" {
		payload["normalized_typed_answer"] = normalizedTypedAnswer
	}
	if req.SelectedChoiceWordID != nil && *req.SelectedChoiceWordID != uuid.Nil {
		payload["selected_choice_word_id"] = req.SelectedChoiceWordID.String()
	}
	if req.SelectedChoiceConfusableGroupKey != "" {
		payload["selected_choice_confusable_group_key"] = req.SelectedChoiceConfusableGroupKey
	}
	if inferredCause != "" {
		payload["memory_cause"] = inferredCause
	}
	if _, err := s.stateRepo.Upsert(ctx, state); err != nil {
		return err
	}
	if err := s.poolRepo.MarkPoolItemCompleted(ctx, item.ID, now); err != nil {
		return err
	}
	if !item.BonusPractice {
		if followUpID, err := s.maybeAppendSameDayFollowUp(ctx, user.ID, item, state); err != nil {
			s.logger.Warn("append same-day review follow-up", "error", err)
		} else if followUpID != uuid.Nil {
			createdItemIDs = append(createdItemIDs, followUpID)
		}
	}
	appendUndoSnapshotPayload(payload, &previousState, true, createdItemIDs)
	if err := s.recordEvent(ctx, domain.LearningEvent{
		UserID:         user.ID,
		WordID:         item.WordID,
		PoolItemID:     &item.ID,
		EventType:      eventType,
		EventTime:      now,
		ResponseTimeMs: req.ResponseTimeMs,
		ModeUsed:       req.ModeUsed,
		ClientEventID:  req.ClientEventID,
		Payload:        payload,
	}); err != nil {
		return err
	}
	return nil
}

func (s *LearningService) SubmitReveal(ctx context.Context, user domain.User, req RevealRequest) error {
	item, err := s.poolRepo.GetPoolItem(ctx, user.ID, req.PoolItemID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	switch req.Kind {
	case domain.RevealKindMeaning, domain.RevealKindExample, domain.RevealKindHint:
	default:
		return fmt.Errorf("%w: unsupported reveal kind", domain.ErrValidation)
	}
	if !item.BonusPractice {
		state, err := s.stateRepo.Get(ctx, user.ID, item.WordID)
		if err == nil {
			switch req.Kind {
			case domain.RevealKindMeaning:
				state.RevealMeaningCount++
			case domain.RevealKindExample:
				state.RevealExampleCount++
			case domain.RevealKindHint:
				state.HintUsedCount++
			}
			state.WeaknessScore = ComputeWeaknessScore(state)
			if _, err := s.stateRepo.Upsert(ctx, state); err != nil {
				return err
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if err := s.poolRepo.UpdatePoolItemReveal(ctx, item.ID, req.Kind); err != nil {
		return err
	}
	eventType := domain.EventTypeRevealMeaning
	switch req.Kind {
	case domain.RevealKindMeaning:
		eventType = domain.EventTypeRevealMeaning
	case domain.RevealKindExample:
		eventType = domain.EventTypeRevealExample
	case domain.RevealKindHint:
		eventType = domain.EventTypeHintUsage
	}
	return s.recordEvent(ctx, domain.LearningEvent{
		UserID:         user.ID,
		WordID:         item.WordID,
		PoolItemID:     &item.ID,
		EventType:      eventType,
		EventTime:      now,
		ResponseTimeMs: req.ResponseTimeMs,
		ModeUsed:       req.ModeUsed,
		ClientEventID:  req.ClientEventID,
	})
}

func (s *LearningService) SubmitPronunciation(ctx context.Context, user domain.User, req PronunciationRequest) error {
	item, err := s.poolRepo.GetPoolItem(ctx, user.ID, req.PoolItemID)
	if err != nil {
		return err
	}
	return s.recordEvent(ctx, domain.LearningEvent{
		UserID:        user.ID,
		WordID:        item.WordID,
		PoolItemID:    &item.ID,
		EventType:     domain.EventTypePronunciation,
		EventTime:     s.clock.Now(),
		ClientEventID: req.ClientEventID,
	})
}

func (s *LearningService) RefreshWeaknessForActiveUser(ctx context.Context, userID uuid.UUID) error {
	return s.stateRepo.RefreshWeaknessScores(ctx, userID)
}

func (s *LearningService) recordEvent(ctx context.Context, event domain.LearningEvent) error {
	if err := s.eventRepo.Insert(ctx, event); err != nil {
		return err
	}
	return nil
}

func (s *LearningService) loadOrInitState(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	state, err := s.stateRepo.Get(ctx, userID, wordID)
	if err == nil {
		return state, nil
	}
	if !isNotFound(err) {
		return domain.UserWordState{}, err
	}
	return domain.UserWordState{
		UserID:     userID,
		WordID:     wordID,
		Status:     domain.WordStatusLearning,
		Difficulty: 0.5,
		Stability:  0.5,
		CreatedAt:  s.clock.Now(),
		UpdatedAt:  s.clock.Now(),
	}, nil
}

func (s *LearningService) maybeAppendSameDayFollowUp(ctx context.Context, userID uuid.UUID, item domain.DailyLearningPoolItem, state domain.UserWordState) (uuid.UUID, error) {
	if state.NextReviewAt == nil {
		return uuid.Nil, nil
	}
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return uuid.Nil, err
	}
	localDate, _, _, loc, err := domain.BoundsForLocalDate(*state.NextReviewAt, settings.Timezone)
	if err != nil {
		return uuid.Nil, err
	}
	nowDate, _, _, _, err := domain.BoundsForLocalDate(s.clock.Now(), settings.Timezone)
	if err != nil {
		return uuid.Nil, err
	}
	if localDate != nowDate {
		return uuid.Nil, nil
	}

	pool, _, err := s.poolRepo.GetByLocalDate(ctx, userID, nowDate)
	if err != nil {
		return uuid.Nil, err
	}
	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return uuid.Nil, err
	}
	followUp := domain.DailyLearningPoolItem{
		PoolID:                pool.ID,
		UserID:                userID,
		WordID:                item.WordID,
		Ordinal:               lastOrdinal + 1,
		ItemType:              domain.PoolItemTypeShortTerm,
		ReviewMode:            SelectReviewMode(state, s.memoryCauseInferenceEnabled),
		DueAt:                 state.NextReviewAt,
		Status:                domain.PoolItemStatusPending,
		IsReview:              true,
		FirstExposureRequired: false,
		Metadata: domain.JSONMap{
			"scheduled_local_date": localDate,
			"scheduled_timezone":   loc.String(),
			"source_pool_item_id":  item.ID.String(),
			"source_reason":        "same_day_follow_up",
			"weakness_score":       state.WeaknessScore,
		},
	}
	appendedItem, err := s.poolRepo.AppendPoolItem(ctx, followUp)
	if err != nil {
		return uuid.Nil, err
	}
	return appendedItem.ID, nil
}

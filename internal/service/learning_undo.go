package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type answerUndoSnapshot struct {
	HadPreviousState bool                  `json:"had_previous_state"`
	PreviousState    *domain.UserWordState `json:"previous_state,omitempty"`
	CreatedItemIDs   []string              `json:"created_item_ids,omitempty"`
}

func (s *LearningService) UndoLastAnswer(ctx context.Context, user domain.User, req UndoLastAnswerRequest) error {
	item, err := s.poolRepo.GetPoolItem(ctx, user.ID, req.PoolItemID)
	if err != nil {
		return err
	}
	if item.Status != domain.PoolItemStatusCompleted {
		return fmt.Errorf("%w: pool item is not completed", domain.ErrValidation)
	}

	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return err
	}
	localDate, _, _, _, err := domain.BoundsForLocalDate(s.clock.Now(), settings.Timezone)
	if err != nil {
		return err
	}
	currentPool, _, err := s.poolRepo.GetByLocalDate(ctx, user.ID, localDate)
	if err != nil {
		return err
	}
	if item.PoolID != currentPool.ID {
		return fmt.Errorf("%w: undo is only available in the current daily pool", domain.ErrValidation)
	}

	latestCompleted, err := s.poolRepo.GetLatestCompletedPoolItem(ctx, user.ID, currentPool.ID)
	if err != nil {
		return err
	}
	if latestCompleted.ID != item.ID {
		return fmt.Errorf("%w: only the most recently answered card can be undone", domain.ErrValidation)
	}

	events, err := s.eventRepo.ListRecentByPoolItem(ctx, item.ID)
	if err != nil {
		return err
	}
	answerEvent, snapshot, err := latestUndoableAnswerEvent(events)
	if err != nil {
		return err
	}
	createdItemIDs, err := parseUndoCreatedItemIDs(snapshot.CreatedItemIDs)
	if err != nil {
		return fmt.Errorf("%w: invalid undo snapshot", domain.ErrValidation)
	}
	for _, createdItemID := range createdItemIDs {
		dependentItem, err := s.poolRepo.GetPoolItem(ctx, user.ID, createdItemID)
		if err != nil {
			return fmt.Errorf("%w: undo is no longer available", domain.ErrValidation)
		}
		if dependentItem.Status != domain.PoolItemStatusPending {
			return fmt.Errorf("%w: dependent cards have already been answered", domain.ErrValidation)
		}
	}

	if snapshot.HadPreviousState {
		if snapshot.PreviousState == nil {
			return fmt.Errorf("%w: missing previous state in undo snapshot", domain.ErrValidation)
		}
		if _, err := s.stateRepo.Upsert(ctx, *snapshot.PreviousState); err != nil {
			return err
		}
	} else if err := s.stateRepo.Delete(ctx, user.ID, item.WordID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	if err := s.poolRepo.DeletePoolItems(ctx, user.ID, createdItemIDs); err != nil {
		return err
	}
	if err := s.poolRepo.ReopenPoolItem(ctx, item.ID); err != nil {
		return err
	}

	return s.recordEvent(ctx, domain.LearningEvent{
		UserID:     user.ID,
		WordID:     item.WordID,
		PoolItemID: &item.ID,
		EventType:  domain.EventTypeAnswerUndo,
		EventTime:  s.clock.Now(),
		Payload: domain.JSONMap{
			"undone_event_type":      answerEvent.EventType,
			"undone_client_event_id": answerEvent.ClientEventID,
			"deleted_item_ids":       snapshot.CreatedItemIDs,
		},
	})
}

func (s *LearningService) loadExistingStateSnapshot(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (*domain.UserWordState, bool, error) {
	state, err := s.stateRepo.Get(ctx, userID, wordID)
	if err == nil {
		stateCopy := state
		return &stateCopy, true, nil
	}
	if isNotFound(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *LearningService) initStateFromSnapshot(userID uuid.UUID, wordID uuid.UUID, previousState *domain.UserWordState, hadPreviousState bool, now time.Time) domain.UserWordState {
	if hadPreviousState && previousState != nil {
		return *previousState
	}
	return domain.UserWordState{
		UserID:     userID,
		WordID:     wordID,
		Status:     domain.WordStatusLearning,
		Difficulty: 0.5,
		Stability:  0.5,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func appendUndoSnapshotPayload(payload domain.JSONMap, previousState *domain.UserWordState, hadPreviousState bool, createdItemIDs []uuid.UUID) {
	snapshot := answerUndoSnapshot{
		HadPreviousState: hadPreviousState,
		CreatedItemIDs:   uuidStrings(createdItemIDs),
	}
	if hadPreviousState && previousState != nil {
		stateCopy := *previousState
		snapshot.PreviousState = &stateCopy
	}
	payload["undo"] = structToJSONMap(snapshot)
}

func latestUndoableAnswerEvent(events []domain.LearningEvent) (domain.LearningEvent, answerUndoSnapshot, error) {
	for _, event := range events {
		switch event.EventType {
		case domain.EventTypeFirstExposure, domain.EventTypeReviewAnswer, domain.EventTypeBonusPractice:
			snapshot, err := decodeUndoSnapshot(event.Payload)
			if err != nil {
				return domain.LearningEvent{}, answerUndoSnapshot{}, err
			}
			return event, snapshot, nil
		}
	}
	return domain.LearningEvent{}, answerUndoSnapshot{}, fmt.Errorf("%w: no undoable answer found", domain.ErrValidation)
}

func decodeUndoSnapshot(payload domain.JSONMap) (answerUndoSnapshot, error) {
	rawUndo, ok := payload["undo"]
	if !ok {
		return answerUndoSnapshot{}, fmt.Errorf("%w: missing undo snapshot", domain.ErrValidation)
	}
	var snapshot answerUndoSnapshot
	if err := decodeMapValue(rawUndo, &snapshot); err != nil {
		return answerUndoSnapshot{}, fmt.Errorf("%w: invalid undo snapshot", domain.ErrValidation)
	}
	return snapshot, nil
}

func decodeMapValue(value any, out any) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, out)
}

func structToJSONMap(value any) domain.JSONMap {
	bytes, err := json.Marshal(value)
	if err != nil {
		return domain.JSONMap{}
	}
	var out domain.JSONMap
	if err := json.Unmarshal(bytes, &out); err != nil {
		return domain.JSONMap{}
	}
	return out
}

func parseUndoCreatedItemIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func uuidStrings(ids []uuid.UUID) []string {
	if len(ids) == 0 {
		return nil
	}
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		values = append(values, id.String())
	}
	return values
}

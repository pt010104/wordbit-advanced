package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type LearningEventRepository struct {
	pool *pgxpool.Pool
}

func (r *LearningEventRepository) Insert(ctx context.Context, event domain.LearningEvent) error {
	query := `
		INSERT INTO learning_events (
			user_id, word_id, pool_item_id, event_type, event_time, payload, response_time_ms, mode_used, client_event_id, client_session_id
		) VALUES (
			$1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10
		)
	`
	_, err := r.pool.Exec(ctx, query,
		event.UserID,
		event.WordID,
		event.PoolItemID,
		event.EventType,
		event.EventTime,
		fromJSONMap(event.Payload),
		event.ResponseTimeMs,
		string(event.ModeUsed),
		event.ClientEventID,
		event.ClientSessionID,
	)
	return mapError(err)
}

func (r *LearningEventRepository) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, word_id, pool_item_id, event_type, event_time, payload, response_time_ms, mode_used, client_event_id, client_session_id, created_at
		FROM learning_events
		WHERE pool_item_id = $1
		ORDER BY event_time DESC
		LIMIT 100
	`, itemID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var events []domain.LearningEvent
	for rows.Next() {
		event, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *LearningEventRepository) ListByUserTimeRange(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time) ([]domain.LearningEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, word_id, pool_item_id, event_type, event_time, payload, response_time_ms, mode_used, client_event_id, client_session_id, created_at
		FROM learning_events
		WHERE user_id = $1
		  AND event_time >= $2
		  AND event_time < $3
		ORDER BY event_time ASC, created_at ASC
	`, userID, start, end)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var events []domain.LearningEvent
	for rows.Next() {
		event, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
